#!/usr/bin/env bash

go run generate_fleet/generate_fleet.go 2.2.0 ../2.2.0/fleet
go run generate_fleet/generate_fleet.go 3.0.1 ../3.0.1/fleet

cp ../templates/fleet/couchbase_announce@.service \
   ../2.2.0/fleet 

cp ../templates/fleet/couchbase_announce@.service \
   ../3.0.1/fleet 
