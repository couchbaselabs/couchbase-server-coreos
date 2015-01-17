package main

import (
	"fmt"
	"log"
	"path"
	"strings"

	"github.com/coreos/go-etcd/etcd"
)

const (
	LOCAL_ETCD_URL           = "http://127.0.0.1:4001"
	KEY_CLUSTER_INITIAL_NODE = "cluster-initial-node"
	KEY_NODE_STATE           = "node-state"
	TTL_NONE                 = 0
)

type CouchbaseCluster struct {
	etcdClient *etcd.Client
}

func (c *CouchbaseCluster) StartCouchbaseNode(nodeIp string) error {

	c.etcdClient = etcd.NewClient([]string{LOCAL_ETCD_URL})
	success, err := c.BecomeFirstClusterNode(nodeIp)
	if err != nil {
		return err
	}

	c.StartCouchbaseService()

	switch success {
	case true:
		if err := c.ClusterInit(); err != nil {
			return err
		}
		if err := c.CreateBucket(); err != nil {
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

// Loop over list of machines in etc cluster and join
// the first node that is up
func (c CouchbaseCluster) JoinExistingCluster() error {

	liveNodeIp, err := c.FindLiveNode()
	if err != nil {
		return err
	}
	return c.JoinLiveNode(liveNodeIp)

}

// Loop over list of machines in etc cluster and find
// first live node.
func (c CouchbaseCluster) FindLiveNode() (string, error) {

	key := path.Join(KEY_NODE_STATE)
	log.Printf("key: %v", key)
	response, err := c.etcdClient.Get(key, false, false)
	log.Printf("response: %+v, err: %v", response, err)

	node := response.Node
	if node == nil {
		return "", fmt.Errorf("No live node found.  Node == nil")
	}

	log.Printf("node: %+v", node)

	if len(node.Nodes) == 0 {
		return "", fmt.Errorf("No live node found.  Nodes is empty")
	}

	for _, subNode := range node.Nodes {
		log.Printf("\tsubnode: %+v", subNode)

		// the key will be: /node-state/172.17.8.101:8091, but we
		// only want the last element in the path
		// TODO: path.Split()
		_, subNodeIp := path.Split(subNode.Key)
		return subNodeIp, nil
	}

	return "", fmt.Errorf("No live node found")

}

func (c CouchbaseCluster) StartCouchbaseService() error {
	log.Printf("StartCouchbaseService()")
	return nil
}

func (c CouchbaseCluster) ClusterInit() error {
	log.Printf("ClusterInit()")
	return nil
}

func (c CouchbaseCluster) CreateBucket() error {
	log.Printf("CreateBucket()")
	return nil
}

func (c CouchbaseCluster) JoinLiveNode(liveNodeIp string) error {
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
