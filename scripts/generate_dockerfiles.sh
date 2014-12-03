#!/usr/bin/env bash

go run generate_dockerfiles.go 2.2.0 > ../2.2/Dockerfile
go run generate_dockerfiles.go 3.0.1 > ../3.0.1/Dockerfile


