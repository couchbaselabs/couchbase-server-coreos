
Run Couchbase Server under Docker + CoreOS.

The following Docker images are built with the Dockerfiles contained in this repo:

* https://registry.hub.docker.com/u/tleyden5iwx/couchbase-server-3.0.1
* https://registry.hub.docker.com/u/tleyden5iwx/couchbase-server-2.2.0

## Regnerating Dockerfiles / Fleet files

The Dockerfile and Fleet files in this repo are generated from templates.

In order to re-generate them, do the following:

```
$ cd scripts
$ ./generate_scripts.sh && ./generate_dockerfiles.sh && ./generate_fleet.sh
``` 

## Proposal for fixing node restart problem

- When couchbase-start is called 