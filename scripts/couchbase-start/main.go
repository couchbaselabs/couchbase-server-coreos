package main

import (
	"fmt"
	"log"

	"github.com/tleyden/couchbase-cluster-go"

	"os"

	"strings"
)

func main() {

	usage := fmt.Sprintf("%v <ip> <user:pass>", os.Args[0])

	if len(os.Args) != 3 {
		log.Fatal(usage)
	}

	couchbaseCluster := &cbcluster.CouchbaseCluster{}
	couchbaseCluster.LocalCouchbaseIp = os.Args[1]

	userPass := os.Args[2]
	if !strings.Contains(userPass, ":") {
		log.Fatal(fmt.Errorf("user:pass must have a colon"))
	}
	userPassComponents := strings.Split(userPass, ":")
	couchbaseCluster.AdminUsername = userPassComponents[0]
	couchbaseCluster.AdminPassword = userPassComponents[1]

	if err := couchbaseCluster.StartCouchbaseNode(); err != nil {
		log.Fatal(err)
	}

}
