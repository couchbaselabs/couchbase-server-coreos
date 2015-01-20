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

	if err := PrepareVarDirectory(); err != nil {
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

	return c.POST(true, endpointUrl, data)

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

	inCluster, err := c.CheckIfInCluster(liveNodeIp)
	if err != nil {
		return err
	}

	if !inCluster {
		if err := c.AddNode(liveNodeIp); err != nil {
			return err
		}
	}

	if err := c.WaitUntilNoRebalanceRunning(liveNodeIp); err != nil {
		return err
	}

	if err := c.TriggerRebalance(liveNodeIp); err != nil {
		return err
	}

	return nil
}

/*
{
    "storageTotals":{
        "ram":{
            "total":11855024128,
            "quotaTotal":7112491008,
            "quotaUsed":268435456,
            "used":8470298624,
            "usedByData":83898760
        },
        "hdd":{
            "total":56962781184,
            "quotaTotal":56962781184,
            "used":7784913426,
            "usedByData":68614614,
            "free":48987991821
        }
    },
    "name":"default",
    "alerts":[

    ],
    "alertsSilenceURL":"/controller/resetAlerts?token=0&uuid=6cd05d617fbfd831f914d972f725c54b",
    "nodes":[
        {
            "systemStats":{
                "cpu_utilization_rate":8.910891089108912,
                "swap_total":0,
                "swap_used":0,
                "mem_total":3951648768,
                "mem_free":2904670208
            },
            "interestingStats":{
                "cmd_get":0.0,
                "couch_docs_actual_disk_size":17814782,
                "couch_docs_data_size":17801064,
                "couch_views_actual_disk_size":0,
                "couch_views_data_size":0,
                "curr_items":0,
                "curr_items_tot":0,
                "ep_bg_fetched":0.0,
                "get_hits":0.0,
                "mem_used":19499352,
                "ops":0.0,
                "vb_replica_curr_items":0
            },
            "uptime":"173",
            "memoryTotal":3951648768,
            "memoryFree":2904670208,
            "mcdMemoryReserved":3014,
            "mcdMemoryAllocated":3014,
            "couchApiBase":"http://10.169.231.39:8092/",
            "clusterMembership":"active",
            "status":"warmup",
            "otpNode":"ns_1@10.169.231.39",
            "thisNode":true,
            "hostname":"10.169.231.39:8091",
            "clusterCompatibility":131072,
            "version":"2.2.0-837-rel-community",
            "os":"x86_64-unknown-linux-gnu",
            "ports":{
                "proxy":11211,
                "direct":11210
            }
        },
        {
            "systemStats":{
                "cpu_utilization_rate":33.64485981308411,
                "swap_total":0,
                "swap_used":0,
                "mem_total":3951726592,
                "mem_free":3186880512
            },
            "interestingStats":{
                "cmd_get":0.0,
                "couch_docs_actual_disk_size":26823580,
                "couch_docs_data_size":26744832,
                "couch_views_actual_disk_size":0,
                "couch_views_data_size":0,
                "curr_items":0,
                "curr_items_tot":0,
                "ep_bg_fetched":0.0,
                "get_hits":0.0,
                "mem_used":32125016,
                "ops":0.0,
                "vb_replica_curr_items":0
            },
            "uptime":"27",
            "memoryTotal":3951726592,
            "memoryFree":3186880512,
            "mcdMemoryReserved":3014,
            "mcdMemoryAllocated":3014,
            "couchApiBase":"http://10.33.184.190:8092/",
            "clusterMembership":"active",
            "status":"unhealthy",
            "otpNode":"ns_1@10.33.184.190",
            "hostname":"10.33.184.190:8091",
            "clusterCompatibility":131072,
            "version":"2.2.0-837-rel-community",
            "os":"x86_64-unknown-linux-gnu",
            "ports":{
                "proxy":11211,
                "direct":11210
            }
        },
        {
            "systemStats":{
                "cpu_utilization_rate":8.910891089108912,
                "swap_total":0,
                "swap_used":0,
                "mem_total":3951648768,
                "mem_free":2915377152
            },
            "interestingStats":{
                "cmd_get":0.0,
                "couch_docs_actual_disk_size":23976252,
                "couch_docs_data_size":23848960,
                "couch_views_actual_disk_size":0,
                "couch_views_data_size":0,
                "curr_items":0,
                "curr_items_tot":0,
                "ep_bg_fetched":0.0,
                "get_hits":0.0,
                "mem_used":32274392,
                "ops":0.0,
                "vb_replica_curr_items":0
            },
            "uptime":"18",
            "memoryTotal":3951648768,
            "memoryFree":2915377152,
            "mcdMemoryReserved":3014,
            "mcdMemoryAllocated":3014,
            "couchApiBase":"http://10.231.192.180:8092/",
            "clusterMembership":"active",
            "status":"unhealthy",
            "otpNode":"ns_1@10.231.192.180",
            "hostname":"10.231.192.180:8091",
            "clusterCompatibility":131072,
            "version":"2.2.0-837-rel-community",
            "os":"x86_64-unknown-linux-gnu",
            "ports":{
                "proxy":11211,
                "direct":11210
            }
        }
    ],
    "buckets":{
        "uri":"/pools/default/buckets?v=42121168&uuid=6cd05d617fbfd831f914d972f725c54b"
    },
    "remoteClusters":{
        "uri":"/pools/default/remoteClusters?uuid=6cd05d617fbfd831f914d972f725c54b",
        "validateURI":"/pools/default/remoteClusters?just_validate=1"
    },
    "controllers":{
        "addNode":{
            "uri":"/controller/addNode?uuid=6cd05d617fbfd831f914d972f725c54b"
        },
        "rebalance":{
            "uri":"/controller/rebalance?uuid=6cd05d617fbfd831f914d972f725c54b"
        },
        "failOver":{
            "uri":"/controller/failOver?uuid=6cd05d617fbfd831f914d972f725c54b"
        },
        "reAddNode":{
            "uri":"/controller/reAddNode?uuid=6cd05d617fbfd831f914d972f725c54b"
        },
        "ejectNode":{
            "uri":"/controller/ejectNode?uuid=6cd05d617fbfd831f914d972f725c54b"
        },
        "setAutoCompaction":{
            "uri":"/controller/setAutoCompaction?uuid=6cd05d617fbfd831f914d972f725c54b",
            "validateURI":"/controller/setAutoCompaction?just_validate=1"
        },
        "replication":{
            "createURI":"/controller/createReplication?uuid=6cd05d617fbfd831f914d972f725c54b",
            "validateURI":"/controller/createReplication?just_validate=1"
        },
        "setFastWarmup":{
            "uri":"/controller/setFastWarmup?uuid=6cd05d617fbfd831f914d972f725c54b",
            "validateURI":"/controller/setFastWarmup?just_validate=1"
        }
    },
    "rebalanceStatus":"none",
    "rebalanceProgressUri":"/pools/default/rebalanceProgress",
    "stopRebalanceUri":"/controller/stopRebalance?uuid=6cd05d617fbfd831f914d972f725c54b",
    "nodeStatusesUri":"/nodeStatuses",
    "maxBucketCount":10,
    "autoCompactionSettings":{
        "parallelDBAndViewCompaction":false,
        "databaseFragmentationThreshold":{
            "percentage":30,
            "size":"undefined"
        },
        "viewFragmentationThreshold":{
            "percentage":30,
            "size":"undefined"
        }
    },
    "fastWarmupSettings":{
        "fastWarmupEnabled":true,
        "minMemoryThreshold":10,
        "minItemsThreshold":10
    },
    "tasks":{
        "uri":"/pools/default/tasks?v=133172395"
    },
    "counters":{
        "rebalance_start":2,
        "rebalance_success":1
    }
}
*/
func (c CouchbaseCluster) CheckIfInCluster(liveNodeIp string) (bool, error) {

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
			log.Printf("CheckIfInCluster returning true")
			return true, nil
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

	data := url.Values{
		"ejectedNodes": {},
		"knownNodes":   otpNodeList,
	}

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
