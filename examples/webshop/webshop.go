package main

import (
	"log"
	"net/http"
	"path/filepath"

	"github.com/dosco/graphjin/serv/v3"
	"github.com/go-chi/chi/v5"
)

func main() {
	// create the router
	r := chi.NewRouter()

	// readin graphjin config
	conf, err := serv.ReadInConfig(filepath.Join("./config", "dev.yml"))
	if err != nil {
		panic(err)
	}

	// create the graphjin service
	gjs, err := serv.NewGraphJinService(conf)
	if err != nil {
		log.Fatal(err)
	}

	// attach the graphql http handler
	r.Handle("/graphql", gjs.GraphQL(nil))

	// attach the rest http handler
	r.Handle("/rest/*", gjs.REST(nil))

	// attach the webui http handler
	r.Handle("/webui/*", gjs.WebUI("/webui/", "/graphql"))

	// add your own http handlers
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Welcome to the webshop!"))
	})

	http.ListenAndServe(":8080", r)
}
