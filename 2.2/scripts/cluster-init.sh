#!/bin/sh

# Usage:
#
# ./cluster-init.sh -n 3 -u "user:passw0rd"
#
# Where:
#   -n number of couchbase nodes to start
#   -u the username and password as a single string, delimited by a colon (:)
# 
# This will start a 3-node couchbase cluster (so you will need to have kicked off
# a cluster with at least 3 ec2 instances)

usage="./cluster-init.sh -n 3 -u \"user:passw0rd\""

while getopts ":n:u:" opt; do
      case $opt in
        n  ) numnodes=$OPTARG ;;
        u  ) userpass=$OPTARG ;;
        \? ) echo $usage
             exit 1 ;; 
      esac
done

shift $(($OPTIND - 1))

# make sure required args were given
if [[ -z "$numnodes" || -z "$userpass" ]] ; then
    echo $usage
    exit 1 
fi

# clone repo with fleet unit files
git clone https://github.com/tleyden/couchbase-server-coreos.git

# generate unit files for non-bootstrap couchbase server nodes
cd couchbase-server-coreos/2.2/fleet && ./create_node_services.sh $numnodes 

# add the username and password to etcd
etcdctl set /services/couchbase/userpass "$userpass"

# launch fleet!
fleetctl start couchbase_bootstrap_node.service couchbase_bootstrap_node_announce.service couchbase_node.*.service


