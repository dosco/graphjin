package serv

import (
	_ "embed"
	"net/http"
	"reflect"
	"strings"
)

// configSchemaJSON is the generated JSON Schema for serv.Config
// (see serv/internal/tools and `make config-schema`).
//
//go:embed config.schema.json
var configSchemaJSON []byte

// ConfigJSONSchema returns the JSON Schema describing the full GraphJin
// configuration file (serv.Config, core sections included). It powers editor
// autocomplete via yaml-language-server modelines, the dev-mode
// /api/v1/config/schema.json endpoint, and `graphjin config schema`.
func ConfigJSONSchema() []byte {
	out := make([]byte, len(configSchemaJSON))
	copy(out, configSchemaJSON)
	return out
}

// Config scope constants classify a top-level config key by which half of the
// config it belongs to.
const (
	ConfigScopeServ = "serv" // server settings (host, auth, rate limiting, agent, ...)
	ConfigScopeCore = "core" // compiler-core settings (sources, tables, roles, ...)
)

// servTopLevelKeys is the set of top-level YAML keys owned by the Serv struct,
// derived once by reflection so it stays in sync with the struct definition.
var servTopLevelKeys = buildTopLevelKeys(reflect.TypeOf(Serv{}))

func buildTopLevelKeys(t reflect.Type) map[string]bool {
	keys := map[string]bool{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := f.Name
		if tag := f.Tag.Get("mapstructure"); tag != "" {
			name = strings.Split(tag, ",")[0]
		}
		keys[strings.ToLower(name)] = true
	}
	return keys
}

// ConfigKeyScope reports whether a top-level config key is a server ("serv") or
// compiler-core ("core") setting, and — for server settings the runtime config
// machinery can patch — whether a change applies live ("hot") or needs a
// restart ("restart"). reload is "" for keys that are not runtime-writable via
// gj_config/update_current_config. The key is the first dotted segment, e.g.
// "rate_limiter" for "rate_limiter.rate".
func ConfigKeyScope(key string) (scope string, reload string) {
	head := strings.ToLower(strings.SplitN(strings.TrimSpace(key), ".", 2)[0])
	if r, ok := servWritableReload[head]; ok {
		return ConfigScopeServ, r
	}
	if servTopLevelKeys[head] {
		return ConfigScopeServ, ""
	}
	return ConfigScopeCore, ""
}

// configSchemaHandler serves the config JSON Schema (dev mode only; see
// routesHandler). The schema describes the file format and carries no
// configuration values.
func configSchemaHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/schema+json")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(configSchemaJSON)
	})
}
