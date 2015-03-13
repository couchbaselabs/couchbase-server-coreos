
Note: the latest version of these instructions can be found [here](http://tleyden.github.io/blog/2014/11/01/running-couchbase-cluster-under-coreos-on-aws/).

Here are instructions on how to fire up a Couchbase Server cluster running under CoreOS on AWS CloudFormation.  You will end up with the following system:

![architecture diagram](http://tleyden-misc.s3.amazonaws.com/blog_images/couchbase-coreos-onion.png)

## Launch CoreOS instances via AWS Cloud Formation

Click the "Launch Stack" button to launch your CoreOS instances via AWS Cloud Formation:

[<img src="https://s3.amazonaws.com/cloudformation-examples/cloudformation-launch-stack.png">](https://console.aws.amazon.com/cloudformation/home?region=us-east-1#cstack=sn%7ECouchbase-CoreOS%7Cturl%7Ehttp://tleyden-misc.s3.amazonaws.com/couchbase-coreos/coreos-stable-pv.template)

*NOTE: this is hardcoded to use the us-east-1 region, so if you need a different region, you should edit the URL accordingly*

Use the following parameters in the form:

* **ClusterSize**: 3 nodes (default)
* **Discovery URL**:  as it says, you need to grab a new token from https://discovery.etcd.io/new and paste it in the box.
* **KeyPair**:  use whatever you normally use to start EC2 instances.  For this discussion, let's assumed you used `aws`, which corresponds to a file you have on your laptop called `aws.cer`

## ssh into a CoreOS instance

Go to the AWS console under EC2 instances and find the public ip of one of your newly launched CoreOS instances.  

Choose any one of them (it doesn't matter which), and ssh into it as the **core** user with the cert provided in the previous step:

```
$ ssh -i aws.cer -A core@ec2-54-83-80-161.compute-1.amazonaws.com
```

## Sanity check

Let's make sure the CoreOS cluster is healthy first:

```
$ fleetctl list-machines
```

This should return a list of machines in the cluster, like this:

```
MACHINE	        IP              METADATA
03b08680...     10.33.185.16    -
209a8a2e...     10.164.175.9    -
25dd84b7...     10.13.180.194   -
```

## Download cluster-init script

```
$ wget https://raw.githubusercontent.com/couchbaselabs/couchbase-server-docker/master/scripts/cluster-init.sh
$ chmod +x cluster-init.sh
```

This script is not much.  I wrapped things up in a script because the instructions were getting long, but all it does is:

* Downloads a few fleet init files from github.
* Generates a few more fleet init files based on a template and the number of nodes you want.
* Stashes the username/password argument you give it into `etcd`.
* Tells `fleetctl` to kick everything off.  Whee!

## Launch cluster

Run the script you downloaded in the previous step:

```
$ ./cluster-init.sh -v 3.0.1 -n 3 -u "user:passw0rd"
```

Where:

* **-v** the version of Couchbase Server to use.  Valid values are 3.0.1 or 2.2.0.
* **-n** the total number of couchbase nodes to start -- should correspond to number of ec2 instances (eg, 3)
* **-u** the username and password as a single string, delimited by a colon (:) 

Replace `user:passw0rd` with a sensible username and password.  It **must** be colon separated, with no spaces.  The password itself must be at least 6 characters.

Once this command completes, your cluster will be in the process of launching.

## Verify 

To check the status of your cluster, run:

```
$ fleetctl list-units
```

You should see four units, all as active.

```
UNIT						MACHINE				ACTIVE	SUB
couchbase_bootstrap_node.service                375d98b9.../10.63.168.35	active	running
couchbase_bootstrap_node_announce.service       375d98b9.../10.63.168.35	active	running
couchbase_node.1.service                        8cf54d4d.../10.187.61.136	active	running
couchbase_node.2.service                        b8cf0ed6.../10.179.161.76	active	running
```

## Rebalance Couchbase Cluster

**Login to Couchbase Server Web Admin**

* Find the public ip of any of your CoreOS instances via the AWS console
* In a browser, go to `http://<instance_public_ip>:8091`
* Login with the username/password you provided above

After logging in, your Server Nodes tab should look like this:

![screenshot](http://tleyden-misc.s3.amazonaws.com/blog_images/couchbase_admin_ui_prerebalance.png)

**Kick off initial rebalance**

* Click server nodes
* Click "Rebalance"

After the rebalance is complete, you should see:

![screenshot](http://tleyden-misc.s3.amazonaws.com/blog_images/couchbase_admin_ui_post_rebalance.png)

Congratulations!  You now have a 3 node Couchbase Server cluster running under CoreOS / Docker.  

# References

* [How I built couchbase 2.2 for docker](https://gist.github.com/dustin/6605182) by [@dlsspy](https://twitter.com/dlsspy)
* https://github.com/tleyden/couchbase-server-coreos
* https://registry.hub.docker.com/u/ncolomer/couchbase/
* https://github.com/lifegadget/docker-couchbase

