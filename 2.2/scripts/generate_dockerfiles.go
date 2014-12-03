package main

import (
	"io/ioutil"
	"log"
	"os"
	"text/template"
)

type Params struct {
	CB_VERSION string
	CB_PACKAGE string
}

func main() {

	if len(os.Args) < 2 {
		log.Fatal("Usage: ./generate_dockerfiles <couchbase version> <couchbase package>")
		return
	}

	params := Params{
		CB_VERSION: os.Args[1],
		CB_PACKAGE: os.Args[2],
	}

	templateFile := "../templates/Dockerfile.template"

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
