package main

import (
	"encoding/json"
	"fmt"

	"github.com/dosco/graphjin/core"
	"github.com/invopop/jsonschema"
)

func main() {
	// cm := make(map[string]string)
	// err := jsonschema.ExtractGoComments("github.com/dosco/graphjin", "./core", cm)
	// if err != nil {
	// 	panic(err)
	// }
	// for k, v := range cm {
	// 	fmt.Println(">", k, v)
	// }
	// return

	r := new(jsonschema.Reflector)
	if err := r.AddGoComments("github.com/dosco/graphjin", "./core"); err != nil {
		panic(err)
	}

	s := r.Reflect(&core.Config{})
	b, err := json.MarshalIndent(s, "", "\t")
	// b, err := s.MarshalJSON()
	if err != nil {
		panic(err)
	}
	fmt.Println(string(b))
}
