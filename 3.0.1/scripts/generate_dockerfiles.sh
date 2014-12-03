#!/usr/bin/env bash

go run generate_dockerfiles.go 2.2.0 couchbase-server-community_2.2.0_x86_64.rpm > ../2.2/Dockerfile
go run generate_dockerfiles.go 3.0.1 couchbase-server-community-3.0.1-centos6.x86_64.rpm > ../3.0.1/Dockerfile


