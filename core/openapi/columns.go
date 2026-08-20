package openapi

import (
	"sort"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"github.com/getkin/kin-openapi/openapi3"
)

// SynthesiseColumns returns DBColumn entries for the response payload's
// top-level fields, after stripping any result-path wrapper. Returns nil
// when the schema isn't an object we can introspect (e.g. raw scalar
// response, schema absent).
func SynthesiseColumns(schema, table string, ref *openapi3.SchemaRef, resultPath []string) []sdata.DBColumn {
	if ref == nil || ref.Value == nil {
		return nil
	}
	s := walkResultPath(ref.Value, resultPath)
	if s == nil {
		return nil
	}
	if isType(s, "array") && s.Items != nil && s.Items.Value != nil {
		s = s.Items.Value
	}
	if !isType(s, "object") || len(s.Properties) == 0 {
		return nil
	}
	required := requiredSet(s)
	cols := make([]sdata.DBColumn, 0, len(s.Properties))
	for name, prop := range s.Properties {
		col := sdata.DBColumn{
			Schema: schema, Table: table, Name: name,
			Type:    openapiSchemaToSQLType(prop),
			NotNull: required[name],
		}
		cols = append(cols, col)
	}
	sort.Slice(cols, func(i, j int) bool { return cols[i].Name < cols[j].Name })
	for i := range cols {
		cols[i].ID = int32(i)
	}
	return cols
}

// SynthesiseMutationColumns returns the stable mutation envelope plus any
// non-colliding fields declared by an object success response.
func SynthesiseMutationColumns(schema, table string, ref *openapi3.SchemaRef) []sdata.DBColumn {
	cols := []sdata.DBColumn{
		{Schema: schema, Table: table, Name: "ok", Type: "boolean", NotNull: true},
		{Schema: schema, Table: table, Name: "status_code", Type: "integer", NotNull: true},
		{Schema: schema, Table: table, Name: "operation_id", Type: "text", NotNull: true},
		{Schema: schema, Table: table, Name: "request_id", Type: "text", NotNull: true},
		{Schema: schema, Table: table, Name: "response_json", Type: "json"},
	}
	reserved := make(map[string]struct{}, len(cols))
	for _, col := range cols {
		reserved[strings.ToLower(col.Name)] = struct{}{}
	}
	if ref != nil && ref.Value != nil && isType(ref.Value, "object") {
		extra := make([]sdata.DBColumn, 0, len(ref.Value.Properties))
		required := requiredSet(ref.Value)
		for name, prop := range ref.Value.Properties {
			if _, exists := reserved[strings.ToLower(name)]; exists {
				continue
			}
			extra = append(extra, sdata.DBColumn{Schema: schema, Table: table, Name: name, Type: openapiSchemaToSQLType(prop), NotNull: required[name]})
		}
		sort.Slice(extra, func(i, j int) bool { return extra[i].Name < extra[j].Name })
		cols = append(cols, extra...)
	}
	for i := range cols {
		cols[i].ID = int32(i)
	}
	return cols
}

// SynthesiseArgs returns DBColumn entries for an operation's path and
// query parameters. The bridge sets these on DBTable.Args; intro.go
// emits them as field arguments.
func SynthesiseArgs(schema, table string, op OpDescriptor) []sdata.DBColumn {
	out := make([]sdata.DBColumn, 0, len(op.PathParams)+len(op.QueryParams))
	for _, p := range op.PathParams {
		out = append(out, sdata.DBColumn{
			Schema: schema, Table: table, Name: p.Name,
			Type:    paramTypeToSQL(p.Type),
			NotNull: p.Required && !hasDefault(op, p.Name),
		})
	}
	for _, p := range op.QueryParams {
		out = append(out, sdata.DBColumn{
			Schema: schema, Table: table, Name: p.Name,
			Type:    paramTypeToSQL(p.Type),
			NotNull: p.Required && !hasDefault(op, p.Name),
		})
	}
	for i := range out {
		out[i].ID = int32(i)
	}
	return out
}

func hasDefault(op OpDescriptor, name string) bool {
	if len(op.Defaults) == 0 {
		return false
	}
	_, ok := op.Defaults[name]
	return ok
}

func walkResultPath(s *openapi3.Schema, path []string) *openapi3.Schema {
	cur := s
	for _, k := range path {
		if cur == nil || cur.Properties == nil {
			return nil
		}
		ref, ok := cur.Properties[k]
		if !ok || ref == nil || ref.Value == nil {
			return nil
		}
		cur = ref.Value
	}
	return cur
}

func requiredSet(s *openapi3.Schema) map[string]bool {
	out := map[string]bool{}
	for _, name := range s.Required {
		out[name] = true
	}
	return out
}

func openapiSchemaToSQLType(ref *openapi3.SchemaRef) string {
	if ref == nil || ref.Value == nil {
		return "text"
	}
	for _, t := range ref.Value.Type.Slice() {
		return paramTypeToSQL(t)
	}
	return "text"
}

func paramTypeToSQL(t string) string {
	switch t {
	case "integer":
		return "bigint"
	case "number":
		return "numeric"
	case "boolean":
		return "boolean"
	case "string":
		return "text"
	case "":
		return "text"
	default:
		return "text"
	}
}
