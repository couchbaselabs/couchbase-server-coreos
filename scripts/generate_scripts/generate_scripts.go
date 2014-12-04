package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"text/template"
)

type Params struct {
	CB_VERSION_3 string
}

func main() {

	if len(os.Args) < 2 {
		log.Fatal("Usage: ./generate_scripts <couchbase version>")
		return
	}

	params := Params{}

	rawVersionString := os.Args[1]
	switch rawVersionString {
	case "3.0.1":
		params.CB_VERSION_3 = "true"
	case "2.2.0":
		params.CB_VERSION_3 = "false"
	default:
		log.Panic(fmt.Sprintf("Unknown version: %v", rawVersionString))
	}

	templateFile := "../templates/couchbase-start.template"

	templateBytes, err := ioutil.ReadFile(templateFile)
	if err != nil {
		panic(err)
	}

	tmpl, err := template.New("docker").Parse(string(templateBytes))
	if err != nil {
		panic(err)
	}
	err = tmpl.Execute(os.Stdout, params)
	if err != nil {
		panic(err)
	}

}
