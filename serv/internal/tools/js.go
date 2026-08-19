// Generates the JSON Schema for the GraphJin service configuration
// (serv.Config). The schema powers editor autocomplete/validation via
// yaml-language-server modelines, the dev-mode /api/v1/config/schema.json
// endpoint, and `graphjin config schema`.
//
// Run via `make config-schema` or `go generate` from the serv module root;
// the emitted serv/config.schema.json is checked into the repo.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/dosco/graphjin/serv/v3"
	"github.com/invopop/jsonschema"
)

const schemaID = "https://graphjin.com/schema/config.json"

func main() {
	out := flag.String("o", "", "write the schema to this file instead of stdout")
	flag.Parse()

	r := &jsonschema.Reflector{
		// Config structs name their YAML keys with mapstructure tags (viper),
		// not json tags.
		FieldNameTag: "mapstructure",
		// Untagged fields (e.g. Production) are matched case-insensitively by
		// viper, which lowercases keys; mirror that so the schema accepts the
		// keys viper actually reads.
		KeyNamer: strings.ToLower,
		// Config keys are optional unless explicitly tagged
		// jsonschema:"required"; the default omitempty heuristic would mark
		// nearly every key required and drown editors in warnings.
		RequiredFromJSONSchemaTags: true,
		// Root the schema at the Config object itself so editors surface
		// field docs at the top level.
		ExpandedStruct: true,
		Anonymous:      true,
	}

	// $defs are keyed by bare type name, and three reachable types are all
	// called Config: serv.Config (the root), core.Config, and agent.Config.
	// ExpandedStruct inlines the root and then deletes its definition, so a
	// same-named type from another package was left pointing at a $ref that no
	// longer existed — agent.* has never appeared in this schema. Qualify any
	// name that would collide with the root's.
	rootName := reflect.TypeOf(serv.Config{}).Name()
	r.Namer = func(t reflect.Type) string {
		name := t.Name()
		if name == "" || name != rootName {
			return name
		}
		if t.PkgPath() == reflect.TypeOf(serv.Config{}).PkgPath() {
			return name
		}
		return packageQualifiedName(t, name)
	}

	// Field descriptions come from Go source comments. The repo is
	// multi-module, so comments must be registered per module with its real
	// import path; directories are relative to the serv module root.
	for _, m := range []struct{ pkg, dir string }{
		{"github.com/dosco/graphjin/core/v3", "../core"},
		{"github.com/dosco/graphjin/serv/v3", "."},
		{"github.com/dosco/graphjin/auth/v3", "../auth"},
		{"github.com/dosco/graphjin/agent/v3", "../agent"},
	} {
		if err := r.AddGoComments(m.pkg, m.dir); err != nil {
			panic(fmt.Sprintf("comments %s: %v", m.dir, err))
		}
	}

	s := r.Reflect(&serv.Config{})
	s.ID = schemaID
	s.Title = "GraphJin Configuration"
	s.Description = "Configuration for the GraphJin service and compiler core (dev.yml / prod.yml / agentic.yml)."

	b, err := json.MarshalIndent(s, "", "\t")
	if err != nil {
		panic(err)
	}
	b = append(b, '\n')

	if *out == "" {
		fmt.Print(string(b))
		return
	}
	if err := os.WriteFile(*out, b, 0o644); err != nil {
		panic(err)
	}
}

// packageQualifiedName prefixes a type name with its owning package so it
// cannot collide in $defs: github.com/dosco/graphjin/agent/v3.Config becomes
// AgentConfig. Trailing module version elements (v2, v3, …) are skipped.
func packageQualifiedName(t reflect.Type, name string) string {
	parts := strings.Split(t.PkgPath(), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		part := parts[i]
		if part == "" || (len(part) > 1 && part[0] == 'v' && strings.Trim(part[1:], "0123456789") == "") {
			continue
		}
		return strings.ToUpper(part[:1]) + part[1:] + name
	}
	return name
}
