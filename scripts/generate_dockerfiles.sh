#!/usr/bin/env bash

go run generate_dockerfiles/generate_dockerfiles.go 2.2.0 couchbase-server-community_2.2.0_x86_64.rpm > ../community-edition/2.2.0/Dockerfile

go run generate_dockerfiles/generate_dockerfiles.go 2.5.2 couchbase-server-enterprise_2.5.2_x86_64.rpm > ../enterprise-edition/2.5.2/Dockerfile

go run generate_dockerfiles/generate_dockerfiles.go 3.0.1 couchbase-server-community-3.0.1-centos6.x86_64.rpm > ../community-edition/3.0.1/Dockerfile

go run generate_dockerfiles/generate_dockerfiles.go 3.0.2 couchbase-server-enterprise-3.0.2-centos6.x86_64.rpm > ../enterprise-edition/3.0.2/Dockerfile


