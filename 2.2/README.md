# Running Couchbase Server 2.2 under CoreOS Fleet

## Launch CoreOS instances via AWS Cloud Formation

[<img src="https://s3.amazonaws.com/cloudformation-examples/cloudformation-launch-stack.png">](https://console.aws.amazon.com/cloudformation/home?region=us-east-1#cstack=sn%7ECouchbase-CoreOS%7Cturl%7Ehttp://tleyden-misc.s3.amazonaws.com/couchbase-coreos/coreos-stable-pv.template) 

*NOTE: this is hardcoded to use the us-east-1 region, so if you need a different region, you should edit the URL accordingly*

## Update the "memlock" setting

It will work without updating the memlock limit setting, but at best it will be less performant, and at worst it will lead to crashes.  

* SSH into each CoreOS node in the cluster and:
    * `cp /usr/lib/systemd/system/docker.service /etc/systemd/system/`
    * edit /etc/systemd/system/docker.service and add line: `LimitMEMLOCK=infinity`
    * `sudo systemctl daemon-reload`
    * `sudo systemctl restart docker`

## Create data dir

Rather than store databases and indexes on the container's filesystem, it is much more efficient to mount a volume in the container to a directory on the CoreOS instance, and have all data stored there instead.

SSH into **each CoreOS node** in the cluster and:

```
$ sudo mkdir -p /var/lib/couchbase/data
$ sudo mkdir -p /var/lib/couchbase/index
$ sudo chown -R core:core /var/lib/couchbase
```

## Clone repository with scripts / unit files

Ssh into any one of the CoreOS nodes, it doesn't matter which one.

```
$ git clone https://github.com/tleyden/couchbase-server-coreos.git
```

## Generate unit files

We already have a special unit file for the "bootstrap node", but we'll need unit files for the N other Couchbase Server nodes.

In the default CloudFormation, it will launch three nodes total.  Which means we want two other Couchbase Server nodes.  Generate the unit files for those two nodes with:

```
$ cd couchbase-server-coreos/2.2/fleet
$ create_node_services.sh 2
```

After this step, you should have two new files:

* couchbase_node.1.service
* couchbase_node.2.service

## Add Couchbase credentials to etcd

```
$ etcdctl set /services/couchbase/userpass "user:passw0rd"
```

Replace `user:passw0rd` with a sensible username and password.  It **must** be colon separated, with no spaces.  The password itself must be at least 6 characters.

## Launch CoreOS Fleet

```
$ cd couchbase-server-coreos/2.2/fleet
$ fleetctl start couchbase_bootstrap_node.service couchbase_bootstrap_node_announce.service couchbase_node.*.service
```

## Verify correct startup

```
$ fleetctl list-units
```

You should see four units, all as active.

## Login to Couchbase Server Web Admin

* Find the public ip of one of your CoreOS instances via the AWS console
* In a browser, go to `http://<instance_public_ip>:8091`
* Login with the username/password you provided above

## Kick off initial rebalance

* Click server nodes
* Click "Rebalance"

# References

* https://registry.hub.docker.com/u/ncolomer/couchbase/
* https://github.com/lifegadget/docker-couchbase