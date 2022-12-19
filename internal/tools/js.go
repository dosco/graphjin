package main

import (
	"encoding/json"
	"fmt"

	serv "github.com/dosco/graphjin/v2/serv"
	"github.com/invopop/jsonschema"
)

func main() {
	r := new(jsonschema.Reflector)
	if err := r.AddGoComments("github.com/dosco/graphjin", "./core"); err != nil {
		panic(err)
	}
	if err := r.AddGoComments("github.com/dosco/graphjin", "./serv"); err != nil {
		panic(err)
	}
	if err := r.AddGoComments("github.com/dosco/graphjin", "./serv/auth"); err != nil {
		panic(err)
	}

	s := r.Reflect(&serv.Config{})
	b, err := json.MarshalIndent(s, "", "\t")
	// b, err := s.MarshalJSON()
	if err != nil {
		panic(err)
	}
	fmt.Println(string(b))
}
