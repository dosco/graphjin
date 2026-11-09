package serv

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// agentOpenAPISpec describes the agent's REST endpoints: their paths and the
// schemas of what they take and return, generated from the Go types by
// TestAgentOpenAPIIsCurrent (make agent-openapi).
//
//go:embed agent.openapi.json
var agentOpenAPISpec []byte

// agentOpenAPIFeatures reports which agent features are switched on, keyed by
// the x-graphjin-feature names in agent.openapi.json.
func (s *graphjinService) agentOpenAPIFeatures() map[string]bool {
	agent := s.conf.agentEnabled()
	threads := agent && s.threadsEnabled()
	return map[string]bool{
		"status":    true,
		"agent":     agent,
		"threads":   threads,
		"profiles":  threads && s.profilesEnabled(),
		"approvals": agent && s.approvalsEnabled(),
	}
}

// withAgentOpenAPI adds the agent endpoints that are switched on to the REST
// spec core generates from saved queries. The agent paths are absolute, so
// they carry their own server; core's paths stay under /api/v1/rest. The
// document becomes OpenAPI 3.1, whose schemas are plain JSON Schema, which is
// what the agent schemas are reflected as.
func withAgentOpenAPI(spec []byte, features map[string]bool) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(spec, &doc); err != nil {
		return nil, fmt.Errorf("openapi spec: %w", err)
	}
	var agent struct {
		Paths      map[string]map[string]any `json:"paths"`
		Components struct {
			Schemas map[string]any `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(agentOpenAPISpec, &agent); err != nil {
		return nil, fmt.Errorf("agent openapi spec: %w", err)
	}
	paths := mapFromAny(doc["paths"])
	if paths == nil {
		paths = map[string]any{}
	}
	added := 0
	for path, item := range agent.Paths {
		feature, _ := item["x-graphjin-feature"].(string)
		if !features[feature] {
			continue
		}
		delete(item, "x-graphjin-feature")
		item["servers"] = []any{map[string]any{"url": "/", "description": "GraphJin agent API"}}
		paths[path] = item
		added++
	}
	if added == 0 {
		return spec, nil
	}
	doc["paths"] = paths
	components := mapFromAny(doc["components"])
	if components == nil {
		components = map[string]any{}
	}
	schemas := mapFromAny(components["schemas"])
	if schemas == nil {
		schemas = map[string]any{}
	}
	for name, schema := range agent.Components.Schemas {
		schemas[name] = schema
	}
	components["schemas"] = schemas
	doc["components"] = components
	doc["openapi"] = "3.1.0"
	if info := mapFromAny(doc["info"]); info != nil {
		info["title"] = "GraphJin API"
		info["description"] = "The agent API, and REST endpoints generated from saved GraphQL queries (under /api/v1/rest)."
	}
	return json.MarshalIndent(doc, "", "  ")
}
