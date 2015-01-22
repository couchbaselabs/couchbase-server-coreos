package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/coreos/go-etcd/etcd"
)

/**

TODO:

- Take user/password as parameter to this script
- Make it work with couchbase 3
  - Figure out cluster ram size and tell couchbase somehow (need to check rest api)

*/

const (
	LOCAL_ETCD_URL              = "http://127.0.0.1:4001"
	KEY_NODE_STATE              = "/couchbase.com/couchbase-node-state"
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
	DEFAULT_BUCKET_RAM_MB         = "128"
	DEFAULT_BUCKET_REPLICA_NUMBER = "2"
)

type CouchbaseCluster struct {
	etcdClient                 *etcd.Client
	localCouchbaseIp           string
	localCouchbasePort         string
	localCouchbaseVersion      string
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

	if err := PrepareVarDirectory(); err != nil {
		return err
	}

	if err := StartCouchbaseService(); err != nil {
		return err
	}

	if err := c.FetchClusterDetails(); err != nil {
		return err
	}

	switch success {
	case true:
		log.Printf("We became first cluster node, init cluster and bucket")

		// TODO: for cbs 3.0, we need to calc and set the memory size of the cluster
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

	log.Printf("BecomeFirstClusterNode()")

	// since we don't knoow how long it will be until we go
	// into the event loop, set TTL to 0 (infinite) for now.
	_, err := c.etcdClient.CreateDir(KEY_NODE_STATE, TTL_NONE)

	if err != nil {
		// expected error where someone beat us out
		if strings.Contains(err.Error(), "Key already exists") {
			log.Printf("Key %v already exists", KEY_NODE_STATE)
			return false, nil
		}

		// otherwise, unexpected error
		log.Printf("Unexpected error: %v", err)
		return false, err
	}

	// no error must mean that were were able to create the key
	log.Printf("Created key: %v", KEY_NODE_STATE)
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
			log.Printf("FindLiveNode returned err: %v.  Trying again", err)
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

func (c *CouchbaseCluster) FetchClusterDetails() error {

	for i := 0; i < MAX_RETRIES_START_COUCHBASE; i++ {

		endpointUrl := fmt.Sprintf(
			"http://%v:%v/pools",
			c.localCouchbaseIp,
			c.localCouchbasePort,
		)

		jsonMap := map[string]interface{}{}
		if err := c.getJsonData(endpointUrl, &jsonMap); err != nil {
			log.Printf("Got error %v trying to fetch details.  Assume that the cluster is not up yet, sleeping and will retry", err)
			<-time.After(time.Second * 10)
			continue
		}

		implementationVersion := jsonMap["implementationVersion"]
		versionStr, ok := implementationVersion.(string)
		if !ok {
			return fmt.Errorf("Expected implementationVersion to contain a string")
		}

		log.Printf("Version: %v", versionStr)
		c.localCouchbaseVersion = versionStr

		return nil

	}

	return fmt.Errorf("Unable to fetch cluster details after several attempts")

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

// Couchbase expects a few subdirectories under /opt/couchbase/var, or else
// it will refuse to start and fail with an error.  This is only needed
// when /opt/couchbase/var is mounted as a volume, which presumably starts out empty.
func PrepareVarDirectory() error {

	log.Printf("PrepareVarDirectory()")

	cmd := exec.Command(
		"mkdir",
		"-p",
		"lib/couchbase",
		"lib/couchbase/config",
		"lib/couchbase/data",
		"lib/couchbase/stats",
		"lib/couchbase/logs",
		"lib/moxi",
	)
	cmd.Dir = "/opt/couchbase/var"

	output, err := cmd.CombinedOutput()
	log.Printf("mkdir output: %v", output)
	if err != nil {
		return err
	}

	cmd = exec.Command(
		"chown",
		"-R",
		"couchbase:couchbase",
		"/opt/couchbase/var",
	)
	output, err = cmd.CombinedOutput()
	log.Printf("chown output: %v", output)
	if err != nil {
		return err
	}

	return nil

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

	if err := c.POST(true, endpointUrl, data); err != nil {
		return err
	}

	majorVersion, err := c.CouchbaseMajorVersion()
	if err != nil {
		return err
	}
	if majorVersion > 2 {
		return c.SetClusterRam()
	}

	return nil

}

func (c CouchbaseCluster) CouchbaseMajorVersion() (int, error) {

	if len(c.localCouchbaseVersion) == 0 {
		return -1, fmt.Errorf("c.localcouchbaseversion is empty ")
	}

	firstCharVerion, _ := utf8.DecodeRuneInString(c.localCouchbaseVersion)
	majorVersion, err := strconv.Atoi(fmt.Sprintf("%v", firstCharVerion))
	if err != nil {
		return -1, err
	}

	return majorVersion, nil

}

// in Couchbase 3, we need to also set the cluster ram setting
// See http://docs.couchbase.com/admin/admin/REST/rest-node-provisioning.html
func (c CouchbaseCluster) SetClusterRam() error {

	ramMb, err := CalculateClusterRam()
	if err != nil {
		log.Printf("Warning, failed to calculate cluster ram: %v.  Default to 1024 MB", err)
		ramMb = "1024"
	}

	endpointUrl := fmt.Sprintf("http://%v:%v/pools/default", c.localCouchbaseIp, c.localCouchbasePort)

	data := url.Values{
		"memoryQuota": {ramMb},
	}

	log.Printf("Attempting to set cluster ram to: %v MB", ramMb)

	return c.POST(true, endpointUrl, data)

}

func CalculateClusterRam() (string, error) {

	totalRamMb, err := CalculateTotalRam()
	if err != nil {
		return "", err
	}
	log.Printf("Total RAM (MB) on machine: %v", totalRamMb)
	clusterRam := (totalRamMb * 75) / 100
	return fmt.Sprintf("%v", clusterRam), nil

}

func CalculateTotalRam() (int, error) {

	cmd := exec.Command(
		"free",
		"-m",
	)

	output, err := cmd.Output()
	if err != nil {
		return -1, err
	}

	// The returned output will look something like this:
	//              total       used       free     shared    buffers     cached
	// Mem:          3768       2601       1166          0          4       1877
	// -/+ buffers/cache:        720       3048
	// Swap:            0          0          0

	re := regexp.MustCompile(`Mem:[ ]*[0-9]*`)
	memPair := re.FindString(string(output)) // ie, "Mem: 3768"
	if memPair == "" {
		return -1, fmt.Errorf("Could not extract Mem total from %v", output)
	}
	if !strings.Contains(memPair, ":") {
		return -1, fmt.Errorf("Could not extract Mem total from %v, no :", output)
	}
	memPairs := strings.Split(memPair, ":")

	outputTrimmed := strings.TrimSpace(memPairs[1])

	i, err := strconv.Atoi(outputTrimmed)
	if err != nil {
		return -1, err
	}

	return i, nil

}

func (c CouchbaseCluster) CreateDefaultBucket() error {

	log.Printf("CreateDefaultBucket()")

	hasDefaultBucket, err := c.HasDefaultBucket()
	if err != nil {
		return err
	}
	if hasDefaultBucket {
		log.Printf("Default bucket already exists, nothing to do")
		return nil
	}

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

func (c CouchbaseCluster) HasDefaultBucket() (bool, error) {

	log.Printf("HasDefaultBucket()")

	endpointUrl := fmt.Sprintf(
		"http://%v:%v/pools/default/buckets",
		c.localCouchbaseIp,
		c.localCouchbasePort,
	)

	jsonList := []interface{}{}
	if err := c.getJsonData(endpointUrl, &jsonList); err != nil {
		return false, err
	}

	for _, bucketEntry := range jsonList {
		bucketEntryMap, ok := bucketEntry.(map[string]interface{})
		if !ok {
			continue
		}
		name := bucketEntryMap["name"]
		name, ok = name.(string)
		if !ok {
			continue
		}
		if name == "default" {
			return true, nil
		}

	}

	return false, nil

}

func (c CouchbaseCluster) JoinLiveNode(liveNodeIp string) error {

	log.Printf("JoinLiveNode() called with %v", liveNodeIp)

	inCluster, err := c.CheckIfInClusterAndHealthy(liveNodeIp)
	if err != nil {
		return err
	}

	if !inCluster {
		if err := c.AddNodeRetry(liveNodeIp); err != nil {
			return err
		}
	}

	if err := c.WaitUntilNoRebalanceRunning(liveNodeIp); err != nil {
		return err
	}

	// TODO: better coordinate the rebalance, so if N nodes come up at
	// roughly the same time, rebalance only happens _once_

	if err := c.TriggerRebalance(liveNodeIp); err != nil {
		return err
	}

	return nil
}

func (c CouchbaseCluster) CheckIfInClusterAndHealthy(liveNodeIp string) (bool, error) {

	log.Printf("CheckIfInCluster()")
	nodes, err := c.GetClusterNodes(liveNodeIp)
	if err != nil {
		return false, err
	}
	log.Printf("CheckIfInCluster nodes: %+v", nodes)

	for _, node := range nodes {

		nodeMap, ok := node.(map[string]interface{})
		if !ok {
			return false, fmt.Errorf("Node had unexpected data type")
		}

		hostname := nodeMap["hostname"] // ex: "10.231.192.180:8091"
		hostnameStr, ok := hostname.(string)
		log.Printf("CheckIfInCluster, hostname: %v", hostnameStr)

		if !ok {
			return false, fmt.Errorf("No hostname string found")
		}
		if strings.Contains(hostnameStr, c.localCouchbaseIp) {

			status := nodeMap["status"]
			statusStr, ok := status.(string)
			if !ok {
				return false, fmt.Errorf("No status string found")
			}
			if statusStr == "healthy" {
				log.Printf("CheckIfInCluster returning true")
				return true, nil
			} else {
				log.Printf("%v in cluster, but status not healthy.  Status: %v", c.localCouchbaseIp, statusStr)
			}

		}
	}

	log.Printf("CheckIfInCluster returning false")
	return false, nil
}

// Based on docs: http://docs.couchbase.com/couchbase-manual-2.5/cb-rest-api/#rebalancing-nodes
func (c CouchbaseCluster) TriggerRebalance(liveNodeIp string) error {

	log.Printf("TriggerRebalance()")

	otpNodeList, err := c.OtpNodeList(liveNodeIp)
	if err != nil {
		return nil
	}

	log.Printf("TriggerRebalance otpNodeList: %v", otpNodeList)

	liveNodePort := c.localCouchbasePort // TODO: we should be getting this from etcd

	endpointUrl := fmt.Sprintf("http://%v:%v/controller/rebalance", liveNodeIp, liveNodePort)

	otpNodes := strings.Join(otpNodeList, ",")

	data := url.Values{
		"ejectedNodes": {},
		"knownNodes":   {otpNodes},
	}

	log.Printf("TriggerRebalance encoded form value: %v", data.Encode())

	return c.POST(false, endpointUrl, data)
}

// The rebalance command needs the current list of nodes, and it wants
// the "otpNode" values, ie: ["ns_1@10.231.192.180", ..]
func (c CouchbaseCluster) OtpNodeList(liveNodeIp string) ([]string, error) {

	otpNodeList := []string{}

	nodes, err := c.GetClusterNodes(liveNodeIp)
	if err != nil {
		return otpNodeList, err
	}

	for _, node := range nodes {

		nodeMap, ok := node.(map[string]interface{})
		if !ok {
			return otpNodeList, fmt.Errorf("Node had unexpected data type")
		}

		otpNode := nodeMap["otpNode"] // ex: "ns_1@10.231.192.180"
		otpNodeStr, ok := otpNode.(string)
		log.Printf("OtpNodeList, otpNode: %v", otpNodeStr)

		if !ok {
			return otpNodeList, fmt.Errorf("No otpNode string found")
		}

		otpNodeList = append(otpNodeList, otpNodeStr)

	}

	return otpNodeList, nil

}

func (c CouchbaseCluster) GetClusterNodes(liveNodeIp string) ([]interface{}, error) {

	log.Printf("GetClusterNodes()")
	liveNodePort := c.localCouchbasePort // TODO: we should be getting this from etcd

	endpointUrl := fmt.Sprintf("http://%v:%v/pools/default", liveNodeIp, liveNodePort)

	jsonMap := map[string]interface{}{}
	if err := c.getJsonData(endpointUrl, &jsonMap); err != nil {
		return nil, err
	}
	log.Printf("GetClusterNodes jsonMap: %+v", jsonMap)

	nodes := jsonMap["nodes"]

	log.Printf("GetClusterNodes nodes: %+v", nodes)

	nodeMaps, ok := nodes.([]interface{})
	if !ok {
		return nil, fmt.Errorf("Unexpected data type in nodes field")
	}

	return nodeMaps, nil

}

// Since AddNode seems to fail sometimes (I saw a case where it returned a 400 error)
// retry several times before finally giving up.
func (c CouchbaseCluster) AddNodeRetry(liveNodeIp string) error {

	numSecondsToSleep := 0

	for i := 0; i < MAX_RETRIES_JOIN_CLUSTER; i++ {

		numSecondsToSleep += 10

		if err := c.AddNode(liveNodeIp); err != nil {
			log.Printf("AddNode failed with err: %v.  Will retry in %v secs", err, numSecondsToSleep)

		} else {
			// it worked, we are done
			return nil

		}

		time2wait := time.Second * time.Duration(numSecondsToSleep)

		<-time.After(time2wait)

	}

	return fmt.Errorf("Unable to AddNode after several attempts")

}

func (c CouchbaseCluster) AddNode(liveNodeIp string) error {

	log.Printf("AddNode()")

	liveNodePort := c.localCouchbasePort // TODO: we should be getting this from etcd

	endpointUrl := fmt.Sprintf("http://%v:%v/controller/addNode", liveNodeIp, liveNodePort)

	data := url.Values{
		"hostname": {c.localCouchbaseIp},
		"user":     {c.adminUsername},
		"password": {c.adminPassword},
	}

	log.Printf("AddNode posting to %v with data: %v", endpointUrl, data.Encode())

	err := c.POST(false, endpointUrl, data)
	if err != nil {
		if strings.Contains(err.Error(), "Node is already part of cluster") {
			// absorb the error in this case, since its harmless
			log.Printf("Node was already part of cluster, so no need to add")
		} else {
			return err
		}
	}

	return nil

}

func (c CouchbaseCluster) WaitUntilNoRebalanceRunning(liveNodeIp string) error {

	log.Printf("WaitUntilNoRebalanceRunning()")

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

			return nil
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

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	body := ""
	if err != nil {
		body = fmt.Sprintf("Unable to read body: %v", err.Error())
	} else {
		body = string(bodyBytes)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf(
			"Failed to POST to %v.  Status code: %v.  Body: %v",
			endpointUrl,
			resp.StatusCode,
			body,
		)
	}

	return nil

}

// An an vent loop that:
//   - publishes the fact that we are alive into etcd.
func (c CouchbaseCluster) EventLoop() {

	log.Printf("EventLoop()")

	var lastErr error

	for {

		// update the node-state directory ttl.  we want this directory
		// to disappear in case all nodes in the cluster are down, since
		// otherwise it would just be unwanted residue.
		ttlSeconds := uint64(10)
		_, err := c.etcdClient.UpdateDir(KEY_NODE_STATE, ttlSeconds)
		if err != nil {
			msg := fmt.Sprintf("Error updating %v dir in etc with new TTL. "+
				"Ignoring error, but this could cause problems",
				KEY_NODE_STATE)
			log.Printf(msg)
		}

		// publish our ip into etcd with short ttl
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
