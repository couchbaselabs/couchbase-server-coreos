package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/coreos/go-etcd/etcd"
)

const (
	LOCAL_ETCD_URL              = "http://127.0.0.1:4001"
	KEY_NODE_STATE              = "couchbase-node-state"
	TTL_NONE                    = 0
	MAX_RETRIES_JOIN_CLUSTER    = 10
	MAX_RETRIES_START_COUCHBASE = 3

	// in order to set the username and password of a cluster
	// you must pass these "factory default values"
	COUCHBASE_DEFAULT_ADMIN_USERNAME = "admin"
	COUCHBASE_DEFAULT_ADMIN_PASSWORD = "password"

	// TODO: these all need to be passed in as CLI params
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

func (c *CouchbaseCluster) StartCouchbaseNode() error {

	c.localCouchbasePort = LOCAL_COUCHBASE_PORT
	c.adminUsername = ADMIN_USERNAME
	c.adminPassword = ADMIN_PASSWORD
	c.defaultBucketRamQuotaMB = DEFAULT_BUCKET_RAM_MB
	c.defaultBucketReplicaNumber = DEFAULT_BUCKET_REPLICA_NUMBER

	c.etcdClient = etcd.NewClient([]string{LOCAL_ETCD_URL})
	success, err := c.BecomeFirstClusterNode()
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
		log.Printf("We became first cluster node, init cluster and bucket")

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

	c.EventLoop()

	return fmt.Errorf("Event loop died") // should never get here

}

func (c CouchbaseCluster) BecomeFirstClusterNode() (bool, error) {

	_, err := c.etcdClient.CreateDir(KEY_NODE_STATE, TTL_NONE)

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

	log.Printf("node: %+v", node)

	if node == nil {
		log.Printf("node is nil, returning")
		return "", nil
	}

	if len(node.Nodes) == 0 {
		log.Printf("len(node.Nodes) == 0, returning")
		return "", nil
	}

	log.Printf("node.Nodes: %+v", node.Nodes)

	for _, subNode := range node.Nodes {

		// the key will be: /node-state/172.17.8.101:8091, but we
		// only want the last element in the path
		_, subNodeIp := path.Split(subNode.Key)

		log.Printf("subNodeIp: %v", subNodeIp)

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

	log.Printf("AddNodeAndRebalanceWhenReady()")

	numSecondsToSleep := 0

	for i := 0; i < MAX_RETRIES_JOIN_CLUSTER; i++ {

		numSecondsToSleep += 100

		isRebalancing, err := c.IsRebalancing(liveNodeIp)
		if err != nil {
			return err
		}

		switch isRebalancing {
		case true:

			time2wait := time.Second * time.Duration(numSecondsToSleep)

			log.Printf("Rebalance in progress, waiting %v seconds", time2wait)

			<-time.After(time2wait)

			continue
		case false:

			log.Printf("No rebalance in progress")

			return c.AddNodeAndRebalance(liveNodeIp)
		}

	}

	return fmt.Errorf("Unable to rebalance after several attempts")

}

func (c CouchbaseCluster) IsRebalancing(liveNodeIp string) (bool, error) {

	liveNodePort := c.localCouchbasePort // TODO: we should be getting this from etcd

	endpointUrl := fmt.Sprintf("http://%v:%v/pools/default/rebalanceProgress", liveNodeIp, liveNodePort)

	jsonMap := map[string]interface{}{}
	if err := c.getJsonData(endpointUrl, &jsonMap); err != nil {
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

func (c CouchbaseCluster) getJsonData(endpointUrl string, into interface{}) error {

	client := &http.Client{}

	req, err := http.NewRequest("GET", endpointUrl, nil)
	if err != nil {
		return err
	}

	req.SetBasicAuth(c.adminUsername, c.adminPassword)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("Failed to GET %v.  Status code: %v", endpointUrl, resp.StatusCode)
	}

	d := json.NewDecoder(resp.Body)
	return d.Decode(into)

}

func (c CouchbaseCluster) AddNodeAndRebalance(liveNodeIp string) error {

	// TODO: this should first check if the node _needs_ to be added,
	// since otherwise it will fail with:
	//
	//   2015/01/17 15:21:01 Running command returned error: exit status 2.
	//   Combined output: ERROR: unable to server-add 172.17.8.101:8091 (400) Bad Request
	//   [u'Prepare join failed. Node is already part of cluster.']
	//
	// We will run into this error if we mount /opt/couchbase/data instead of current
	// appraoch.

	log.Printf("AddNodeAndRebalance()")

	// TODO: switch to REST API
	// The REST api for rebalancing looks more complicated, so I'll loop back to it
	// curl -v -X -u admin:password POST 'http://192.168.0.77:8091/controller/rebalance' \
	// -d 'ejectedNodes=&knownNodes=ns_1%40192.168.0.77%2Cns_1%40192.168.0.56'

	liveNodePort := c.localCouchbasePort // TODO: we should be getting this from etcd
	ipPortExistingClusterNode := fmt.Sprintf("%v:%v", liveNodeIp, liveNodePort)
	ipPortNodeBeingAdded := fmt.Sprintf("%v:%v", c.localCouchbaseIp, c.localCouchbasePort)

	log.Printf("ipPortExistingClusterNode: %v", ipPortExistingClusterNode)
	log.Printf("ipPortNodeBeingAdded: %v", ipPortNodeBeingAdded)

	cmd := exec.Command(
		"couchbase-cli",
		"rebalance",
		"-c",
		ipPortExistingClusterNode,
		"-u",
		c.adminUsername,
		"-p",
		c.adminPassword,
		fmt.Sprintf("--server-add=%v", ipPortNodeBeingAdded),
		fmt.Sprintf("--server-add-username=%v", c.adminUsername),
		fmt.Sprintf("--server-add-password=%v", c.adminPassword),
	)

	output, err := cmd.CombinedOutput()

	if err != nil {
		log.Printf("Running command returned error: %v.  Combined output: %v", err, string(output))
		return err
	}

	log.Printf("output: %v", string(output))

	// getting weird error:
	// close failed in file object destructor:
	// Error in sys.excepthook: [empty]
	// Original exception was: [empty]

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

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("Failed to POST to %v.  Status code: %v", endpointUrl, resp.StatusCode)
	}

	return nil

}

// An an vent loop that:
//   - publishes the fact that we are alive into etcd.
func (c CouchbaseCluster) EventLoop() {

	log.Printf("EventLoop()")

	var lastErr error

	for {
		// publish our ip into etcd with short ttl
		ttlSeconds := uint64(10)
		if err := c.PublishNodeStateEtcd(ttlSeconds); err != nil {
			msg := fmt.Sprintf("Error publishing node state to etcd: %v. "+
				"Ignoring error, but other nodes won't be able to join"+
				"this node until this issue is resolved.",
				err)
			log.Printf(msg)
			lastErr = err
		} else {
			log.Printf("Published node state to etcd")
			// if we had an error earlier, but it's now resolved,
			// lets log that fact
			if lastErr != nil {
				msg := fmt.Sprintf("Successfully node state to etcd: %v. "+
					"The previous error seems to have fixed itself!",
					err)
				log.Printf(msg)
				lastErr = nil
			}
		}

		// sleep for a while
		<-time.After(time.Second * time.Duration(ttlSeconds/2))

	}

}

// Publish the fact that we are up into etcd.
func (c CouchbaseCluster) PublishNodeStateEtcd(ttlSeconds uint64) error {

	// the etcd key to use, ie: /couchbase-node-state/<our ip>
	// TODO: maybe this should be ip:port
	key := path.Join(KEY_NODE_STATE, c.localCouchbaseIp)

	log.Printf("Publish node-state to key: %v", key)

	_, err := c.etcdClient.Set(key, "up", ttlSeconds)

	return err

}

func main() {

	if len(os.Args) < 2 {
		log.Fatal(fmt.Errorf("You must pass the ip of this node as an arg."))
	}

	couchbaseCluster := &CouchbaseCluster{}
	couchbaseCluster.localCouchbaseIp = os.Args[1]

	if err := couchbaseCluster.StartCouchbaseNode(); err != nil {
		log.Fatal(err)
	}

}
