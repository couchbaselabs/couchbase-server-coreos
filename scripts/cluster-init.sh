#!/bin/sh

# Usage:
#
# ./cluster-init.sh -n 3 -u "user:passw0rd"
#
# Where:
#   -v Couchbase Server version (3.0.1 or 2.2)
#   -n number of couchbase nodes to start
#   -u the username and password as a single string, delimited by a colon (:)
# 
# This will start a 3-node couchbase cluster (so you will need to have kicked off
# a cluster with at least 3 ec2 instances)

usage="./cluster-init.sh -v 3.0.1 -n 3 -u \"user:passw0rd\""

while getopts ":v:n:u:" opt; do
      case $opt in
        v  ) version=$OPTARG ;;
        n  ) numnodes=$OPTARG ;;
        u  ) userpass=$OPTARG ;;
        \? ) echo $usage
             exit 1 ;; 
      esac
done

shift $(($OPTIND - 1))

# make sure required args were given
if [[ -z "$version" || -z "$numnodes" || -z "$userpass" ]] ; then
    echo "Required argument was empty"
    echo $usage
    exit 1 
fi

# calculate how many "other" couchbase nodes aside from the
# bootstrap node by subtracting 1 from the total # of nodes
non_bootstrap_nodes=$(( ${numnodes} - 1 ))

# validate that we have a valid value
if [ "$non_bootrap_nodes" == "-1" ]; then
    echo "You passed an invalid value for n: $numnodes"
    exit 1 
fi

# clone repo with fleet unit files
git clone https://github.com/couchbaselabs/couchbase-server-docker


# add the username and password to etcd
etcdctl set /services/couchbase/userpass "$userpass"

# launch fleet!
echo "Kicking off couchbase server bootstrap node"
fleetctl start couchbase_bootstrap_node.service couchbase_bootstrap_node_announce.service 

if (( $non_bootstrap_nodes > 0 )); then 

    # generate unit files for non-bootstrap couchbase server nodes
    cd couchbase-server-docker/$version/fleet && ./create_node_services.sh $non_bootstrap_nodes

    echo "Kicking off $non_bootrap_nodes additional couchbase server nodes"
    fleetctl start couchbase_node.*.service
else 
    echo "No additional couchbase server nodes needed, running single node system"
fi





