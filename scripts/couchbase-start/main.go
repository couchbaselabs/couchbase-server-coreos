package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/coreos/go-etcd/etcd"
)

const (
	LOCAL_ETCD_URL           = "http://127.0.0.1:4001"
	KEY_CLUSTER_INITIAL_NODE = "cluster-initial-node"
	TTL_NONE                 = 0
)

type CouchbaseCluster struct {
	etcdClient *etcd.Client
}

func (c CouchbaseCluster) ClusterInit() error {
	log.Printf("ClusterInit()")
	return nil
}

func (c CouchbaseCluster) CreateBucket() error {
	log.Printf("CreateBucket()")
	return nil
}

func (c CouchbaseCluster) JoinExistingCluster() error {
	log.Printf("JoinExistingCluster()")
	return nil
}

func (c CouchbaseCluster) StartCouchbaseService() error {
	log.Printf("StartCouchbaseService()")
	return nil
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

	// client.SyncCluster()
	// nodes := client.GetCluster()
	// fmt.Printf("nodes: %+v", nodes)
	return nil

}

func (c CouchbaseCluster) BecomeFirstClusterNode(nodeIp string) (bool, error) {

	response, err := c.etcdClient.Create(KEY_CLUSTER_INITIAL_NODE, nodeIp, TTL_NONE)

	fmt.Printf("response: %+v, err: %+v", response, err)

	if err != nil {
		// expected error where someone beat us out
		fmt.Printf("err.Error(): %v", err.Error())
		if strings.Contains(err.Error(), "Key already exists") {
			fmt.Printf("strings.Contains == true")
			return false, nil
		} else {
			fmt.Printf("strings.Contains == false")
		}

		// otherwise, unexpected error
		return false, err
	}

	// no error must mean that were were able to create the key
	return true, nil

}

func main() {

	// TODO: this needs to be passed in as cmd line arg!!
	ip := "127.0.0.1"

	couchbaseCluster := &CouchbaseCluster{}
	if err := couchbaseCluster.StartCouchbaseNode(ip); err != nil {
		log.Fatal(err)
	}

	/*


		           # Discussion with James:

		           # Use client.write('/cluster-first-node', '<our ip address>', prevExist=False) to
		           # to do an ADD operation.  Only first one to do this will win.

		           # Each node will have a sidekick to maintain its state in
		           # /node-state/<node ip> -> ON or the key will be missing, meaning its not running

		           # When a node comes up:
		           #    - It will try to be the cluster-first-node
		           #    - If succeeds:
		           #       - couchbase-cli cluster-init
		           #       - couchbase-cli bucket-create
		           #    - Else: (there is already a cluster)
		           #       - Find list of machines in cluster from etcd
		           #       - Loop over machines
		           #           - Get ip of machine
		           #           - Does /node-state/<ip> key exist?
		           #              - If it does, try to join the cluster via couchbase-cli server-add





			   # Scenarios

			   # 1. Setting up cluster from scratch (recent machine boot)
			   #
			   #   1A. We become the leader
			   #   1B. Someone beats us to becoming the leader, so we join the leader
			   #
			   # 2. This node rebooted and there is an existing cluster
			   #
			   #   2A. We *were* the leader previously
			   #   2B. We were not the leader previously

			   #  ---- v2

			   # Call etcd to get cluster-leader-counter key

			       # If 0, then we are setting up cluster from scratch (recent machine boot)

			         # success = try_to_become_leader()
			         # if success:
			         #     cluster_one_time_init()
			         #     create_bucket()
			         # else:
			         #     leader = find_leader() && join_leader(leader)

			       # If >= 1, then this node rebooted and there is an existing cluster

			         # leader = find_leader()
			         # if leader == null: abort
			         # join_leader(leader)

			   # try_to_become_leader():

			      # Call etcd to get cluster-leader-ip key

			      # if non-empty, abort and return error

			      # Use previous key index to do a CAS request to write our ip to cluster-leader-ip

			      # if CAS request failed, abort and return error

			      # otherwise, we are the leader

			   # find_leader()

			     # Call etcd to get cluster-leader-ip key or return error

			   # join_leader()

			     #



	*/

	fmt.Sprintf("hello..")

}
