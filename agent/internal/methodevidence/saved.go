package methodevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

type definition struct{ query, hash string }

// Resolver binds a successful saved-query execution to a prior same-run detail
// receipt. Definitions must match the catalog's content hash. Prepared catalog
// snapshots let older recordings resolve definitions without fetching current
// server state or trusting model-generated text.
type Resolver struct {
	namespace string
	prepared  map[string]definition
	observed  map[string]string
}

func NewResolver(trace any) *Resolver {
	r := &Resolver{prepared: map[string]definition{}, observed: map[string]string{}}
	// Ax values as well as decoded HTTP records use this seam.
	raw, _ := json.Marshal(trace)
	var t map[string]any
	_ = json.Unmarshal(raw, &t)
	input := object(t["input"])
	r.namespace = text(input["namespace"])
	prepared := object(object(input["context"])["_graphjin_prepared"])
	for _, batch := range list(prepared["catalog"]) {
		for _, value := range list(object(batch)["cards"]) {
			card := object(value)
			if card["kind"] != "saved_query" {
				continue
			}
			meta := metadata(card["evidence_json"])
			name := qualifiedName(meta)
			query, hash := text(card["graphql_query"]), text(meta["source_hash"])
			if verifiedDefinition(query, meta) {
				r.prepared[name] = definition{query, hash}
			}
		}
	}
	return r
}

func (r *Resolver) ObserveCatalog(action map[string]any) {
	if action["tool"] != "query_catalog" || !Successful(action) {
		return
	}
	args := object(action["args"])
	for id, value := range object(action["evidence"]) {
		if !strings.HasPrefix(id, "saved_query:") {
			continue
		}
		requested := text(args["id"]) == id
		for _, target := range list(args["ids"]) {
			requested = requested || text(target) == id
		}
		if !requested {
			continue
		}
		fields := object(value)
		meta := metadata(fields["evidence_json"])
		name, hash := qualifiedName(meta), text(meta["source_hash"])
		if name == "" || strings.TrimPrefix(id, "saved_query:") != name {
			continue
		}
		// A later detail read supersedes the previous definition, even when
		// incomplete or changed. Never fall back to a stale earlier definition.
		delete(r.observed, name)
		query := text(fields["graphql_query"])
		if query == "" {
			if d, ok := r.prepared[name]; ok && d.hash == hash {
				query = d.query
			}
		}
		if verifiedDefinition(query, meta) {
			r.observed[name] = query
		}
	}
}

func qualifiedName(meta map[string]any) string {
	if name := text(meta["qualified_name"]); name != "" {
		return name
	}
	name, ns := text(meta["name"]), text(meta["namespace"])
	if name == "" {
		return ""
	}
	if ns != "" {
		return ns + "." + name
	}
	return name
}

func verified(query, hash string) bool {
	if query == "" || hash == "" {
		return false
	}
	// Core's default hashes the normalized JSON string. The service supplies
	// raw source hashes; older catalogs removed surrounding whitespace. Each
	// accepted representation still has to reproduce the recorded hash exactly.
	data, err := json.Marshal(query)
	if err != nil {
		return false
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) == hash {
		return true
	}
	for _, source := range []string{query, query + "\n", query + "\r\n"} {
		sum = sha256.Sum256([]byte(source))
		if hex.EncodeToString(sum[:]) == hash {
			return true
		}
	}
	return false
}

func verifiedDefinition(query string, meta map[string]any) bool {
	if hash := text(meta["query_hash"]); hash != "" {
		data, err := json.Marshal(query)
		if err != nil || query == "" || text(meta["source_hash"]) == "" {
			return false
		}
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:]) == hash
	}
	return verified(query, text(meta["source_hash"]))
}

func object(value any) map[string]any { m, _ := value.(map[string]any); return m }
func list(value any) []any            { s, _ := value.([]any); return s }
func text(value any) string           { s, _ := value.(string); return s }
func metadata(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(text(value)), &m)
	return m
}
