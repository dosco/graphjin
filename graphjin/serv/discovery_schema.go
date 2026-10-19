package serv

import (
	"encoding/json"

	"github.com/invopop/jsonschema"
)

func discoverySchema() map[string]any {
	return map[string]any{
		"contract_version":    "discovery.v1",
		"list_namespaces":     jsonSchemaFor[ListNamespacesResult](),
		"list_tables":         jsonSchemaFor[ListTablesResult](),
		"describe_table":      jsonSchemaFor[TableSchemaWithAggregations](),
		"get_table_sample":    jsonSchemaFor[TableSampleResult](),
		"get_schema_insights": jsonSchemaFor[SchemaInsights](),
	}
}

func jsonSchemaFor[T any]() any {
	var zero T
	reflector := jsonschema.Reflector{
		DoNotReference:            true,
		Anonymous:                 true,
		AllowAdditionalProperties: true,
	}
	schema := reflector.Reflect(zero)
	schema.Version = ""

	data, err := json.Marshal(schema)
	if err != nil {
		return map[string]any{"type": "object"}
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{"type": "object"}
	}
	return out
}
