#!/usr/bin/env bash

# For the following files, they are static so just copy them

cp ../templates/README.md \
   ../2.2.0/

cp ../templates/README.md \
   ../3.0.1/

go run generate_scripts/generate_scripts.go 2.2.0 > ../2.2.0/scripts/couchbase-start
go run generate_scripts/generate_scripts.go 3.0.1 > ../3.0.1/scripts/couchbase-start
