package main

import (
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"
)

type FleetParams struct {
	CB_VERSION string
}

func main() {

	if len(os.Args) < 2 {
		log.Fatal("Usage: ./generate_fleet <couchbase version> <dest dir>")
		return
	}

	params := FleetParams{
		CB_VERSION: os.Args[1],
	}

	destDir := os.Args[2]

	templateFiles := []string{
		"../templates/fleet/couchbase_node@.service.template",
	}

	for _, templateFile := range templateFiles {

		templateBytes, err := ioutil.ReadFile(templateFile)
		if err != nil {
			panic(err)
		}

		tmpl, err := template.New("docker").Parse(string(templateBytes))
		if err != nil {
			panic(err)
		}

		// create a writer that is going to write to <dest dir>/templateFile
		_, filename := filepath.Split(templateFile)

		// strip off .template from filename
		filename = strings.Replace(filename, ".template", "", -1)

		destFile := path.Join(destDir, filename)

		f, err := os.Create(destFile)
		if err != nil {
			panic(err)
		}
		defer f.Close()

		// execute template and write to dest
		err = tmpl.Execute(f, params)
		if err != nil {
			panic(err)
		}

	}

}
