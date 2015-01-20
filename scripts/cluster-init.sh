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

# clone repo with fleet unit files
git clone https://github.com/couchbaselabs/couchbase-server-docker

# TEMP: use experimental branch
cd couchbase-server-docker && git checkout -t origin/feature/experimental && cd

# add the username and password to etcd
etcdctl set /services/couchbase/userpass "$userpass"

# launch fleet!

cd couchbase-server-docker/$version/fleet/
echo "Submit couchbase_node@.service"
fleetctl submit couchbase_node@.service
echo "Kicking off $numnodes couchbase server nodes: couchbase_node@{1..$numnodes}.service"
fleetctl start "couchbase_node@{1..$numnodes}.service"





