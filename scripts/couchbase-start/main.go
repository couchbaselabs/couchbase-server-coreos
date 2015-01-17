package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/coreos/go-etcd/etcd"
)

const (
	LOCAL_ETCD_URL              = "http://127.0.0.1:4001"
	KEY_CLUSTER_INITIAL_NODE    = "cluster-initial-node"
	KEY_NODE_STATE              = "node-state"
	TTL_NONE                    = 0
	MAX_RETRIES_JOIN_CLUSTER    = 10
	MAX_RETRIES_START_COUCHBASE = 3

	// in order to set the username and password of a cluster
	// you must pass these "factory default values"
	COUCHBASE_DEFAULT_ADMIN_USERNAME = "admin"
	COUCHBASE_DEFAULT_ADMIN_PASSWORD = "password"

	// TODO: these all need to be passed in as CLI params
	COUCHBASE_IP   = "172.17.8.101"
	COUCHBASE_PORT = "8091"
	ADMIN_USERNAME = "user"
	ADMIN_PASSWORD = "passw0rd"
)

type CouchbaseCluster struct {
	etcdClient    *etcd.Client
	couchbaseIp   string
	couchbasePort string
	adminUsername string
	adminPassword string
}

func (c *CouchbaseCluster) StartCouchbaseNode(nodeIp string) error {

	c.couchbaseIp = COUCHBASE_IP
	c.couchbasePort = COUCHBASE_PORT
	c.adminUsername = ADMIN_USERNAME
	c.adminPassword = ADMIN_PASSWORD

	c.etcdClient = etcd.NewClient([]string{LOCAL_ETCD_URL})
	success, err := c.BecomeFirstClusterNode(nodeIp)
	if err != nil {
		return err
	}

	if err := StartCouchbaseService(); err != nil {
		return err
	}

	if err := c.WaitForRestService(); err != nil {
		return err
	}

	switch success {
	case true:
		if err := c.ClusterInit(COUCHBASE_IP, ADMIN_USERNAME, ADMIN_PASSWORD); err != nil {
			return err
		}
		if err := c.CreateDefaultBucket(); err != nil {
			return err
		}
	case false:
		if err := c.JoinExistingCluster(); err != nil {
			return err
		}
	}

	return nil

}

func (c CouchbaseCluster) BecomeFirstClusterNode(nodeIp string) (bool, error) {

	_, err := c.etcdClient.Create(KEY_CLUSTER_INITIAL_NODE, nodeIp, TTL_NONE)

	if err != nil {
		// expected error where someone beat us out
		if strings.Contains(err.Error(), "Key already exists") {
			return false, nil
		}

		// otherwise, unexpected error
		return false, err
	}

	// no error must mean that were were able to create the key
	return true, nil

}

// Loop over list of machines in etcd cluster and join
// the first node that is up
func (c CouchbaseCluster) JoinExistingCluster() error {

	log.Printf("JoinExistingCluster() called")

	sleepSeconds := 0

	for i := 0; i < MAX_RETRIES_JOIN_CLUSTER; i++ {

		log.Printf("Calling FindLiveNode()")

		liveNodeIp, err := c.FindLiveNode()
		if err != nil {
			return err
		}

		log.Printf("liveNodeIp: %v", liveNodeIp)

		if liveNodeIp != "" {
			return c.JoinLiveNode(liveNodeIp)
		}

		sleepSeconds += 10

		log.Printf("Sleeping for %v", sleepSeconds)

		<-time.After(time.Second * time.Duration(sleepSeconds))

	}

	return fmt.Errorf("Failed to join cluster after several retries")

}

// Loop over list of machines in etc cluster and find
// first live node.
func (c CouchbaseCluster) FindLiveNode() (string, error) {

	key := path.Join(KEY_NODE_STATE)

	response, err := c.etcdClient.Get(key, false, false)
	if err != nil {
		return "", fmt.Errorf("Error getting key.  Err: %v", err)
	}

	node := response.Node
	if node == nil {
		return "", nil
	}

	if len(node.Nodes) == 0 {
		return "", nil
	}

	for _, subNode := range node.Nodes {

		// the key will be: /node-state/172.17.8.101:8091, but we
		// only want the last element in the path
		_, subNodeIp := path.Split(subNode.Key)
		return subNodeIp, nil
	}

	return "", nil

}

func (c CouchbaseCluster) WaitForRestService() error {

	for i := 0; i < MAX_RETRIES_START_COUCHBASE; i++ {

		endpointUrl := fmt.Sprintf("http://%v:%v/", c.couchbaseIp, c.couchbasePort)
		log.Printf("Waiting for REST service at %v to be up", endpointUrl)
		resp, err := http.Get(endpointUrl)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == 200 {
				log.Printf("REST service appears to be up")
				return nil
			}
		}

		log.Printf("Not up yet, sleeping and will retry")
		<-time.After(time.Second * 10)

	}

	return fmt.Errorf("Unable to connect to REST api after several attempts")

}

func StartCouchbaseService() error {

	log.Printf("StartCouchbaseService()")

	for i := 0; i < MAX_RETRIES_START_COUCHBASE; i++ {

		// call "service couchbase-server start"
		cmd := exec.Command("service", "couchbase-server", "start")

		if err := cmd.Run(); err != nil {
			log.Printf("Running command returned error: %v", err)
			return err
		}

		running, err := CouchbaseServiceRunning()
		if err != nil {
			return err
		}
		if running {
			log.Printf("Couchbase service running")
			return nil
		}

		log.Printf("Couchbase service not running, sleep and try again")

		<-time.After(time.Second * 10)

	}

	return fmt.Errorf("Unable to start couchbase service after several retries")

}

func CouchbaseServiceRunning() (bool, error) {

	cmd := exec.Command("service", "couchbase-server", "status")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// service x status returns a non-zero exit code if
		// the service is not running, which causes cmd.CombinedOutput
		// to return an error.   however, absorb the error and turn it
		// into a "not running" signal rather than propagating an error.
		return false, nil
	}
	log.Printf("Checking status returned output: %v", string(output))

	return strings.Contains(string(output), "is running"), nil
}

// Set the username and password for the cluster.  The same as calling:
// $ couchbase-cli cluster-init ..
//
// Docs: http://docs.couchbase.com/admin/admin/REST/rest-node-set-username.html
func (c CouchbaseCluster) ClusterInit(ip, adminUsername, adminPass string) error {

	client := &http.Client{}

	endpointUrl := fmt.Sprintf("http://%v:%v/settings/web", ip, COUCHBASE_PORT)

	data := url.Values{
		"username": {adminUsername},
		"password": {adminPass},
		"port":     {COUCHBASE_PORT},
	}
	req, err := http.NewRequest("POST", endpointUrl, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	req.SetBasicAuth(COUCHBASE_DEFAULT_ADMIN_USERNAME, COUCHBASE_DEFAULT_ADMIN_PASSWORD)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("Failed to init cluster: %v", resp.StatusCode)
	}

	return nil

}

func (c CouchbaseCluster) CreateDefaultBucket() error {

	/*
		    echo "Call bucket-create"
		    untilsuccessful /opt/couchbase/bin/couchbase-cli bucket-create -c $IP \
			-u $CB_USERNAME -p $CB_PASSWORD \
			--bucket=default --bucket-ramsize=$DEFAULT_BUCKET_RAM_SIZE_MB

	*/

	log.Printf("CreateBucket()")
	return nil
}

func (c CouchbaseCluster) JoinLiveNode(liveNodeIp string) error {

	/*
		    untilsuccessful /opt/couchbase/bin/couchbase-cli server-add -c $BOOTSTRAP_IP \
			--user=$CB_USERNAME --password=$CB_PASSWORD \
			--server-add=$IP

	*/
	log.Printf("JoinLiveNode() called with %v", liveNodeIp)
	return nil
}

func main() {

	// TODO: this needs to be passed in as cmd line arg!!
	ip := "127.0.0.1"

	couchbaseCluster := &CouchbaseCluster{}
	if err := couchbaseCluster.StartCouchbaseNode(ip); err != nil {
		log.Fatal(err)
	}

}
