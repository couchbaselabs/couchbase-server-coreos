package main

import (
	"fmt"

	"github.com/coreos/go-etcd/etcd"
)

func main() {

	client := etcd.NewClient([]string{"foo"})
	fmt.Println("client: %v", client)

	/*

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
