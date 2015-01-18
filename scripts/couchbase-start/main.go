package main

import (
	"encoding/json"
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
	KEY_NODE_STATE              = "node-state"
	TTL_NONE                    = 0
	MAX_RETRIES_JOIN_CLUSTER    = 10
	MAX_RETRIES_START_COUCHBASE = 3

	// in order to set the username and password of a cluster
	// you must pass these "factory default values"
	COUCHBASE_DEFAULT_ADMIN_USERNAME = "admin"
	COUCHBASE_DEFAULT_ADMIN_PASSWORD = "password"

	// TODO: these all need to be passed in as CLI params
	LOCAL_COUCHBASE_IP            = "172.17.8.101"
	LOCAL_COUCHBASE_PORT          = "8091"
	ADMIN_USERNAME                = "admin"
	ADMIN_PASSWORD                = "password"
	DEFAULT_BUCKET_RAM_MB         = "256"
	DEFAULT_BUCKET_REPLICA_NUMBER = "2"
)

type CouchbaseCluster struct {
	etcdClient                 *etcd.Client
	localCouchbaseIp           string
	localCouchbasePort         string
	adminUsername              string
	adminPassword              string
	defaultBucketRamQuotaMB    string
	defaultBucketReplicaNumber string
}

func (c *CouchbaseCluster) StartCouchbaseNode(nodeIp string) error {

	c.localCouchbaseIp = LOCAL_COUCHBASE_IP
	c.localCouchbasePort = LOCAL_COUCHBASE_PORT
	c.adminUsername = ADMIN_USERNAME
	c.adminPassword = ADMIN_PASSWORD
	c.defaultBucketRamQuotaMB = DEFAULT_BUCKET_RAM_MB
	c.defaultBucketReplicaNumber = DEFAULT_BUCKET_REPLICA_NUMBER

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
		if err := c.ClusterInit(); err != nil {
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

	_, err := c.etcdClient.Create(KEY_NODE_STATE, nodeIp, TTL_NONE)

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

		endpointUrl := fmt.Sprintf("http://%v:%v/", c.localCouchbaseIp, c.localCouchbasePort)
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
func (c CouchbaseCluster) ClusterInit() error {

	endpointUrl := fmt.Sprintf("http://%v:%v/settings/web", c.localCouchbaseIp, c.localCouchbasePort)

	data := url.Values{
		"username": {c.adminUsername},
		"password": {c.adminPassword},
		"port":     {c.localCouchbasePort},
	}

	return c.POST(true, endpointUrl, data)

}

func (c CouchbaseCluster) CreateDefaultBucket() error {

	log.Printf("CreateDefaultBucket()")

	endpointUrl := fmt.Sprintf("http://%v:%v/pools/default/buckets", c.localCouchbaseIp, c.localCouchbasePort)

	data := url.Values{
		"name":          {"default"},
		"ramQuotaMB":    {c.defaultBucketRamQuotaMB},
		"authType":      {"none"},
		"replicaNumber": {c.defaultBucketReplicaNumber},
		"proxyPort":     {"11215"},
	}

	return c.POST(false, endpointUrl, data)

}

func (c CouchbaseCluster) JoinLiveNode(liveNodeIp string) error {

	log.Printf("JoinLiveNode() called with %v", liveNodeIp)

	if err := c.AddNodeAndRebalanceWhenReady(liveNodeIp); err != nil {
		return err
	}

	return nil
}

func (c CouchbaseCluster) AddNode(liveNodeIp string) error {

	log.Printf("AddNode()")

	endpointUrl := fmt.Sprintf("http://%v:%v/controller/addNode", c.localCouchbaseIp, c.localCouchbasePort)

	data := url.Values{
		"hostname": {liveNodeIp},
		"user":     {c.adminUsername},
		"password": {c.adminPassword},
	}

	return c.POST(false, endpointUrl, data)

}

func (c CouchbaseCluster) AddNodeAndRebalanceWhenReady(liveNodeIp string) error {

	log.Printf("RebalanceWhenReady()")

	numSecondsToSleep := 0

	for i := 0; i < MAX_RETRIES_JOIN_CLUSTER; i++ {

		numSecondsToSleep += 100

		isRebalancing, err := c.IsRebalancing(liveNodeIp)
		if err != nil {
			return err
		}

		switch isRebalancing {
		case true:
			log.Printf("Rebalance in progress, waiting ..")

			<-time.After(time.Second * time.Duration(numSecondsToSleep))

			continue
		case false:
			return c.AddNodeAndRebalance(liveNodeIp)
		}

	}

	return fmt.Errorf("Unable to rebalance after several attempts")

}

func (c CouchbaseCluster) IsRebalancing(liveNodeIp string) (bool, error) {

	liveNodePort := c.localCouchbasePort // TODO: we should be getting this from etcd

	endpointUrl := fmt.Sprintf("http://%v:%v/pools/default/rebalanceProgress", liveNodeIp, liveNodePort)

	jsonMap := map[string]interface{}{}
	if err := getJsonData(endpointUrl, &jsonMap); err != nil {
		return true, err
	}

	rawStatus := jsonMap["status"]
	str, ok := rawStatus.(string)
	if !ok {
		return true, fmt.Errorf("Unexepected type in status field in json")
	}

	if str == "none" {
		return false, nil
	}

	return true, nil

}

func getJsonData(u string, into interface{}) error {
	res, err := http.Get(u)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("Non-200 response from: %v", u)
	}

	d := json.NewDecoder(res.Body)
	return d.Decode(into)
}

func (c CouchbaseCluster) AddNodeAndRebalance(liveNodeIp string) error {

	// TODO: switch to REST API
	// The REST api for rebalancing looks more complicated, so I'll loop back to it
	// curl -v -X -u admin:password POST 'http://192.168.0.77:8091/controller/rebalance' \
	// -d 'ejectedNodes=&knownNodes=ns_1%40192.168.0.77%2Cns_1%40192.168.0.56'

	liveNodePort := c.localCouchbasePort // TODO: we should be getting this from etcd
	ipPortExistingClusterNode := fmt.Sprintf("%v:%v", liveNodeIp, liveNodePort)
	ipPortNodeBeingAdded := fmt.Sprintf("%v:%v", c.localCouchbaseIp, c.localCouchbasePort)

	cmd := exec.Command(
		"couchbase-cli",
		"rebalance",
		"-c",
		ipPortExistingClusterNode,
		fmt.Sprintf("--server-add=%v", ipPortNodeBeingAdded),
		fmt.Sprintf("--server-add-username=%v", c.adminUsername),
		fmt.Sprintf("--server-add-password=%v", c.adminPassword),
	)

	if err := cmd.Run(); err != nil {
		log.Printf("Running command returned error: %v", err)
		return err
	}

	return nil

}

func (c CouchbaseCluster) POST(defaultAdminCreds bool, endpointUrl string, data url.Values) error {

	client := &http.Client{}

	req, err := http.NewRequest("POST", endpointUrl, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if defaultAdminCreds {
		req.SetBasicAuth(COUCHBASE_DEFAULT_ADMIN_USERNAME, COUCHBASE_DEFAULT_ADMIN_PASSWORD)
	} else {
		req.SetBasicAuth(c.adminUsername, c.adminPassword)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("Failed to POST to %v.  Status code: %v", endpointUrl, resp.StatusCode)
	}

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
