package core

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/allow"
	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// OpenAPI 3.0 Specification Types
type OpenAPIDocument struct {
	OpenAPI    string                `json:"openapi"`
	Info       OpenAPIInfo           `json:"info"`
	Servers    []OpenAPIServer       `json:"servers,omitempty"`
	Paths      map[string]PathItem   `json:"paths"`
	Components *OpenAPIComponents    `json:"components,omitempty"`
	Security   []map[string][]string `json:"security,omitempty"`
}

type OpenAPIInfo struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version"`
}

type OpenAPIServer struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

type PathItem struct {
	Get    *OpenAPIOperation `json:"get,omitempty"`
	Post   *OpenAPIOperation `json:"post,omitempty"`
	Put    *OpenAPIOperation `json:"put,omitempty"`
	Delete *OpenAPIOperation `json:"delete,omitempty"`
}

type OpenAPIOperation struct {
	Summary     string              `json:"summary,omitempty"`
	Description string              `json:"description,omitempty"`
	OperationID string              `json:"operationId,omitempty"`
	Parameters  []Parameter         `json:"parameters,omitempty"`
	RequestBody *RequestBody        `json:"requestBody,omitempty"`
	Responses   map[string]Response `json:"responses"`
	Tags        []string            `json:"tags,omitempty"`
}

type Parameter struct {
	Name        string `json:"name"`
	In          string `json:"in"` // "query", "header", "path", "cookie"
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Schema      Schema `json:"schema"`
}

type RequestBody struct {
	Description string               `json:"description,omitempty"`
	Content     map[string]MediaType `json:"content"`
	Required    bool                 `json:"required,omitempty"`
}

type Response struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content,omitempty"`
}

type MediaType struct {
	Schema Schema `json:"schema"`
}

type Schema struct {
	Type                 string            `json:"type,omitempty"`
	Format               string            `json:"format,omitempty"`
	Properties           map[string]Schema `json:"properties,omitempty"`
	Items                *Schema           `json:"items,omitempty"`
	Required             []string          `json:"required,omitempty"`
	AdditionalProperties interface{}       `json:"additionalProperties,omitempty"`
	OneOf                []Schema          `json:"oneOf,omitempty"`
	Description          string            `json:"description,omitempty"`
	Example              interface{}       `json:"example,omitempty"`
	Ref                  string            `json:"$ref,omitempty"`
}

type OpenAPIComponents struct {
	Schemas map[string]Schema `json:"schemas,omitempty"`
}

// QueryAnalysis contains analyzed information about a GraphQL query
type QueryAnalysis struct {
	Item           allow.Item
	Operation      graph.Operation
	QCode          *qcode.QCode
	HTTPMethods    []string
	Parameters     []Parameter
	ResponseSchema Schema
}

// GenerateOpenAPISpec generates a complete OpenAPI specification for all REST endpoints
func (g *GraphJin) GenerateOpenAPISpec() (*OpenAPIDocument, error) {
	gj := g.Load().(*graphjinEngine)

	// Get all queries from allow list
	items, err := gj.allowList.ListAll()
	if err != nil {
		return nil, fmt.Errorf("failed to list queries: %w", err)
	}

	spec := &OpenAPIDocument{
		OpenAPI: "3.0.0",
		Info: OpenAPIInfo{
			Title:       "GraphJin REST API",
			Description: "Auto-generated REST API endpoints from GraphQL queries",
			Version:     "1.0.0",
		},
		Servers: []OpenAPIServer{
			{
				URL:         "/api/v1/rest",
				Description: "GraphJin REST API Server",
			},
		},
		Paths: make(map[string]PathItem),
		Components: &OpenAPIComponents{
			Schemas: make(map[string]Schema),
		},
	}

	// Generate shared schema components from database schema
	g.generateComponents(spec.Components, gj)

	// Analyze each query and generate paths
	for _, item := range items {
		analysis, err := g.analyzeQuery(item)
		if err != nil {
			// Log error but continue with other queries
			continue
		}

		pathItem := g.generatePathItem(analysis, spec.Components)
		path := "/" + item.Name
		spec.Paths[path] = pathItem
	}

	return spec, nil
}

// generateComponents creates shared OpenAPI components from GraphJin's schema
func (g *GraphJin) generateComponents(components *OpenAPIComponents, gj *graphjinEngine) {
	// Generate base response schema
	components.Schemas["GraphJinResponse"] = Schema{
		Type: "object",
		Properties: map[string]Schema{
			"data": {
				Type:        "object",
				Description: "Query result data",
			},
			"errors": {
				Type: "array",
				Items: &Schema{
					Ref: "#/components/schemas/GraphQLError",
				},
			},
		},
	}

	// Generate error schema
	components.Schemas["GraphQLError"] = Schema{
		Type: "object",
		Properties: map[string]Schema{
			"message": {Type: "string", Description: "Error message"},
			"path":    {Type: "array", Items: &Schema{Type: "string"}},
		},
		Required: []string{"message"},
	}

	// Generate schema objects for each table using introspection logic
	for _, table := range gj.schema.GetTables() {
		if table.Blocked || len(table.Columns) == 0 {
			continue
		}

		tableName := strings.Title(table.Name)

		// Generate table object schema
		tableSchema := Schema{
			Type:       "object",
			Properties: make(map[string]Schema),
		}

		for _, col := range table.Columns {
			if col.Blocked {
				continue
			}

			fieldSchema := g.columnToOpenAPISchema(col)
			tableSchema.Properties[col.Name] = fieldSchema
		}

		components.Schemas[tableName] = tableSchema

		// Generate array wrapper for table results
		components.Schemas[tableName+"Array"] = Schema{
			Type:  "array",
			Items: &Schema{Ref: fmt.Sprintf("#/components/schemas/%s", tableName)},
		}
	}
}

// analyzeQuery analyzes a single query and extracts type information using GraphJin's compilation
func (g *GraphJin) analyzeQuery(item allow.Item) (*QueryAnalysis, error) {
	gj := g.Load().(*graphjinEngine)

	// Parse the GraphQL query using GraphJin's parser
	op, err := graph.Parse(item.Query)
	if err != nil {
		return nil, fmt.Errorf("failed to parse query %s: %w", item.Name, err)
	}

	// Compile the query using GraphJin's compiler to get exact type information
	qc, err := gj.qcodeCompiler.Compile(item.Query, nil, "admin", item.Namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to compile query %s: %w", item.Name, err)
	}

	analysis := &QueryAnalysis{
		Item:      item,
		Operation: op,
		QCode:     qc,
	}

	// Determine HTTP methods based on operation type
	analysis.HTTPMethods = g.getHTTPMethods(qc.Type, qc.SType)

	// Extract parameters from GraphQL variables using introspection-style type mapping
	analysis.Parameters = g.extractParameters(op.VarDef)

	// Generate response schema using GraphJin's compiled query structure
	analysis.ResponseSchema = g.generateResponseSchemaFromQCode(qc, gj)

	return analysis, nil
}

// extractParameters converts GraphQL variable definitions to OpenAPI parameters
// Uses the same type mapping logic as GraphJin's introspection
func (g *GraphJin) extractParameters(varDefs []graph.VarDef) []Parameter {
	var params []Parameter

	for _, varDef := range varDefs {
		// Extract type information from the Val node
		typeName := "String" // default type
		required := false    // default to optional

		if varDef.Val != nil {
			typeName = varDef.Val.Name
			// Check if it's a non-null type (required)
			if varDef.Val.Type == graph.NodeLabel && len(varDef.Val.Children) > 0 &&
			   varDef.Val.Children[0].Type == graph.NodeLabel {
				required = true
			}
		}

		param := Parameter{
			Name:        varDef.Name,
			In:          "query",
			Description: fmt.Sprintf("GraphQL variable: %s", varDef.Name),
			Required:    required,
			Schema:      g.graphQLTypeToOpenAPISchema(typeName),
		}
		params = append(params, param)
	}

	return params
}

// graphQLTypeToOpenAPISchema converts GraphQL type to OpenAPI schema
// Reuses GraphJin's type mapping from intro.go
func (g *GraphJin) graphQLTypeToOpenAPISchema(graphQLType string) Schema {
	// Parse the type using the same logic as GraphJin's getType function
	gqlType, isList := getType(graphQLType)

	var schema Schema

	// Map GraphQL types to OpenAPI types using GraphJin's type mapping
	switch gqlType {
	case "String":
		schema = Schema{Type: "string"}
	case "ID":
		schema = Schema{Type: "string", Description: "Unique identifier"}
	case "Int":
		schema = Schema{Type: "integer", Format: "int32"}
	case "Float":
		schema = Schema{Type: "number", Format: "float"}
	case "Boolean":
		schema = Schema{Type: "boolean"}
	case "JSON":
		schema = Schema{Type: "object", AdditionalProperties: true}
	default:
		schema = Schema{Type: "string", Description: fmt.Sprintf("Custom type: %s", gqlType)}
	}

	// Handle arrays using GraphJin's list detection
	if isList {
		schema = Schema{
			Type:  "array",
			Items: &schema,
		}
	}

	return schema
}

// generateResponseSchemaFromQCode generates response schema from QCode using GraphJin's compilation results
func (g *GraphJin) generateResponseSchemaFromQCode(qc *qcode.QCode, gj *graphjinEngine) Schema {
	// Create root response schema following GraphJin's structure
	rootSchema := Schema{
		Type: "object",
		Properties: map[string]Schema{
			"data":   g.generateDataSchemaFromQCode(qc, gj),
			"errors": {Ref: "#/components/schemas/GraphQLError"},
		},
	}

	return rootSchema
}

// generateDataSchemaFromQCode generates the data field schema from QCode structure
func (g *GraphJin) generateDataSchemaFromQCode(qc *qcode.QCode, gj *graphjinEngine) Schema {
	if len(qc.Roots) == 0 {
		return Schema{Type: "object"}
	}

	// Single root query
	if len(qc.Roots) == 1 {
		rootSel := &qc.Selects[qc.Roots[0]]
		return g.generateSelectSchemaFromQCode(qc, rootSel, gj)
	}

	// Multiple roots - create object with each root as property
	schema := Schema{
		Type:       "object",
		Properties: make(map[string]Schema),
	}

	for _, rootID := range qc.Roots {
		rootSel := &qc.Selects[rootID]
		schema.Properties[rootSel.FieldName] = g.generateSelectSchemaFromQCode(qc, rootSel, gj)
	}

	return schema
}

// generateSelectSchemaFromQCode generates schema for a specific select using GraphJin's compiled structure
func (g *GraphJin) generateSelectSchemaFromQCode(qc *qcode.QCode, sel *qcode.Select, gj *graphjinEngine) Schema {
	// Try to find the table info for this select
	var tableInfo *sdata.DBTable
	if sel.Ti.Name != "" {
		for _, table := range gj.schema.GetTables() {
			if table.Name == sel.Ti.Name {
				tableInfo = &table
				break
			}
		}
	}

	objectSchema := Schema{
		Type:       "object",
		Properties: make(map[string]Schema),
	}

	// Add __typename if requested
	if sel.Typename {
		objectSchema.Properties["__typename"] = Schema{
			Type:        "string",
			Description: "GraphQL type name",
		}
	}

	// Add regular fields using GraphJin's field compilation
	for _, field := range sel.Fields {
		fieldSchema := g.generateFieldSchemaFromQCode(field, tableInfo)
		objectSchema.Properties[field.FieldName] = fieldSchema
	}

	// Add child relationships using GraphJin's relationship resolution
	for _, childID := range sel.Children {
		childSel := &qc.Selects[childID]
		childSchema := g.generateSelectSchemaFromQCode(qc, childSel, gj)

		// Use GraphJin's singularity detection
		if childSel.Singular {
			objectSchema.Properties[childSel.FieldName] = childSchema
		} else {
			objectSchema.Properties[childSel.FieldName] = Schema{
				Type:  "array",
				Items: &childSchema,
			}
		}
	}

	// If this is a table with a known schema, reference it
	if tableInfo != nil && !sel.Singular && sel.ParentID == -1 {
		// This is a root table query that returns an array
		tableName := strings.Title(tableInfo.Name)
		return Schema{
			Ref: fmt.Sprintf("#/components/schemas/%sArray", tableName),
		}
	} else if tableInfo != nil && sel.Singular && sel.ParentID == -1 {
		// This is a root table query that returns a single object
		tableName := strings.Title(tableInfo.Name)
		return Schema{
			Ref: fmt.Sprintf("#/components/schemas/%s", tableName),
		}
	}

	// For nested objects or when we can't determine table info, return the object schema
	if !sel.Singular && sel.ParentID != -1 {
		return Schema{
			Type:  "array",
			Items: &objectSchema,
		}
	}

	return objectSchema
}

// generateFieldSchemaFromQCode generates schema for a field using GraphJin's type system
func (g *GraphJin) generateFieldSchemaFromQCode(field qcode.Field, tableInfo *sdata.DBTable) Schema {
	switch field.Type {
	case qcode.FieldTypeCol:
		return g.columnToOpenAPISchema(field.Col)
	case qcode.FieldTypeFunc:
		return g.functionToOpenAPISchema(field.Func)
	default:
		return Schema{Type: "string", Description: "Unknown field type"}
	}
}

// columnToOpenAPISchema converts a database column to OpenAPI schema
// Uses the same logic as GraphJin's getType and getTypeFromColumn functions
func (g *GraphJin) columnToOpenAPISchema(col sdata.DBColumn) Schema {
	// Use GraphJin's type resolution for primary keys
	if col.PrimaryKey {
		schema := Schema{Type: "string", Format: "uuid", Description: "Primary key"}
		if col.Array {
			return Schema{Type: "array", Items: &schema}
		}
		return schema
	}

	// Use GraphJin's getType function for type mapping
	gqlType, isList := getType(col.Type)

	var schema Schema

	// Convert GraphQL type to OpenAPI type
	switch gqlType {
	case "String":
		schema = Schema{Type: "string"}
		// Add specific formats for known string types
		sqlType := strings.ToLower(col.Type)
		if strings.Contains(sqlType, "timestamp") || strings.Contains(sqlType, "date") {
			schema.Format = "date-time"
		} else if strings.Contains(sqlType, "time") {
			schema.Format = "time"
		} else if strings.Contains(sqlType, "uuid") {
			schema.Format = "uuid"
		}
	case "Int":
		schema = Schema{Type: "integer"}
		// Determine format based on SQL type
		sqlType := strings.ToLower(col.Type)
		if strings.Contains(sqlType, "big") {
			schema.Format = "int64"
		} else {
			schema.Format = "int32"
		}
	case "Float":
		schema = Schema{Type: "number", Format: "float"}
	case "Boolean":
		schema = Schema{Type: "boolean"}
	case "JSON":
		schema = Schema{Type: "object", AdditionalProperties: true}
	default:
		schema = Schema{Type: "string", Description: fmt.Sprintf("Unknown SQL type: %s", col.Type)}
	}

	// Handle arrays using GraphJin's array detection
	if col.Array || isList {
		schema = Schema{
			Type:  "array",
			Items: &schema,
		}
	}

	// Add description from column comment
	if col.Comment != "" {
		schema.Description = col.Comment
	}

	return schema
}

// functionToOpenAPISchema converts a database function to OpenAPI schema
func (g *GraphJin) functionToOpenAPISchema(fn sdata.DBFunction) Schema {
	// Use GraphJin's type mapping for function return types
	gqlType, isList := getType(fn.Type)

	var schema Schema

	switch gqlType {
	case "String":
		schema = Schema{Type: "string"}
	case "Int":
		schema = Schema{Type: "integer"}
	case "Float":
		schema = Schema{Type: "number"}
	case "Boolean":
		schema = Schema{Type: "boolean"}
	case "JSON":
		schema = Schema{Type: "object", AdditionalProperties: true}
	default:
		schema = Schema{Type: "string", Description: fmt.Sprintf("Function: %s", fn.Name)}
	}

	if isList {
		schema = Schema{
			Type:  "array",
			Items: &schema,
		}
	}

	return schema
}

// getHTTPMethods determines appropriate HTTP methods for the operation
func (g *GraphJin) getHTTPMethods(opType, subType qcode.QType) []string {
	switch opType {
	case qcode.QTQuery:
		return []string{"GET", "POST"}
	case qcode.QTMutation:
		switch subType {
		case qcode.QTInsert:
			return []string{"POST"}
		case qcode.QTUpdate, qcode.QTUpsert:
			return []string{"PUT", "POST"}
		case qcode.QTDelete:
			return []string{"DELETE", "POST"}
		default:
			return []string{"POST"}
		}
	case qcode.QTSubscription:
		return []string{"GET"}
	default:
		return []string{"POST"}
	}
}

// generatePathItem creates OpenAPI path item for a query
func (g *GraphJin) generatePathItem(analysis *QueryAnalysis, components *OpenAPIComponents) PathItem {
	pathItem := PathItem{}

	for _, method := range analysis.HTTPMethods {
		operation := &OpenAPIOperation{
			Summary:     fmt.Sprintf("Execute %s query", analysis.Item.Name),
			Description: fmt.Sprintf("Executes the %s GraphQL query via REST", analysis.Item.Name),
			OperationID: fmt.Sprintf("%s_%s", strings.ToLower(method), analysis.Item.Name),
			Tags:        []string{strings.Title(analysis.Item.Operation)},
			Responses: map[string]Response{
				"200": {
					Description: "Successful response",
					Content: map[string]MediaType{
						"application/json": {
							Schema: analysis.ResponseSchema,
						},
					},
				},
				"400": {
					Description: "Bad request",
					Content: map[string]MediaType{
						"application/json": {
							Schema: Schema{
								Ref: "#/components/schemas/GraphJinResponse",
							},
						},
					},
				},
			},
		}

		// Add parameters for GET requests
		if method == "GET" && len(analysis.Parameters) > 0 {
			operation.Parameters = analysis.Parameters

			// Add variables parameter for JSON variables
			operation.Parameters = append(operation.Parameters, Parameter{
				Name:        "variables",
				In:          "query",
				Description: "JSON-encoded GraphQL variables",
				Required:    false,
				Schema:      Schema{Type: "string", Description: "JSON object as string"},
			})
		}

		// Add request body for POST/PUT requests
		if (method == "POST" || method == "PUT") && len(analysis.Parameters) > 0 {
			// Create schema for variables object
			varsSchema := Schema{
				Type:       "object",
				Properties: make(map[string]Schema),
			}

			for _, param := range analysis.Parameters {
				varsSchema.Properties[param.Name] = param.Schema
			}

			operation.RequestBody = &RequestBody{
				Description: "GraphQL variables as JSON object",
				Required:    false,
				Content: map[string]MediaType{
					"application/json": {
						Schema: varsSchema,
					},
				},
			}
		}

		// Assign operation to appropriate HTTP method
		switch method {
		case "GET":
			pathItem.Get = operation
		case "POST":
			pathItem.Post = operation
		case "PUT":
			pathItem.Put = operation
		case "DELETE":
			pathItem.Delete = operation
		}
	}

	return pathItem
}

// GetOpenAPISpec returns the OpenAPI specification as JSON
func (g *GraphJin) GetOpenAPISpec() ([]byte, error) {
	spec, err := g.GenerateOpenAPISpec()
	if err != nil {
		return nil, err
	}

	return json.MarshalIndent(spec, "", "  ")
}
