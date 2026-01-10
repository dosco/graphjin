package mongodriver

import (
	"context"
	"database/sql/driver"
	"fmt"
	"reflect"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// IntrospectOptions configures schema discovery.
type IntrospectOptions struct {
	SampleSize        int  `json:"sample_size"`
	IncludeValidators bool `json:"include_validators"`
}

// introspectColumns discovers collection schemas and returns column metadata.
// This implements the "introspect_columns" operation for GraphJin schema discovery.
func (c *Conn) introspectColumns(ctx context.Context, q *QueryDSL) (driver.Rows, error) {
	opts := IntrospectOptions{
		SampleSize:        100,
		IncludeValidators: true,
	}

	if q.Options != nil {
		if v, ok := q.Options["sample_size"].(float64); ok {
			opts.SampleSize = int(v)
		}
		if v, ok := q.Options["include_validators"].(bool); ok {
			opts.IncludeValidators = v
		}
	}

	// List all collections
	collections, err := c.db.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("mongodriver: list collections: %w", err)
	}

	// Column names matching GraphJin's expected format (must match sdata/tables.go Scan order)
	columns := []string{
		"table_schema",
		"table_name",
		"column_name",
		"data_type",
		"is_nullable",
		"is_primary_key",
		"is_unique_key",
		"is_array",
		"is_fulltext",
		"fkey_schema",
		"fkey_table",
		"fkey_column",
	}

	var rows [][]any

	for _, collName := range collections {
		// Skip system collections
		if strings.HasPrefix(collName, "system.") {
			continue
		}

		coll := c.db.Collection(collName)

		// Try to get JSON Schema validator
		var schemaFields map[string]FieldInfo
		if opts.IncludeValidators {
			schemaFields = c.getCollectionValidator(ctx, collName)
		}

		// Sample documents to discover fields
		discoveredFields := c.sampleCollectionFields(ctx, coll, opts.SampleSize)

		// Merge schema and discovered fields
		allFields := mergeFields(schemaFields, discoveredFields)

		// Convert to rows
		for fieldName, field := range allFields {
			// Infer foreign key from naming convention
			fkTable, fkCol := inferForeignKey(fieldName)
			fkSchema := ""
			if fkTable != "" {
				fkSchema = c.db.Name() // same database for FK
			}

			row := []any{
				c.db.Name(),           // table_schema
				collName,              // table_name
				fieldName,             // column_name
				field.SQLType,         // data_type
				!field.Required,       // is_nullable (NotNull in DBColumn)
				fieldName == "_id",    // is_primary_key
				field.IsUnique,        // is_unique_key
				field.IsArray,         // is_array
				false,                 // is_fulltext (MongoDB doesn't have SQL-style FTS by default)
				fkSchema,              // fkey_schema
				fkTable,               // fkey_table
				fkCol,                 // fkey_column
			}
			rows = append(rows, row)

			// Add "id" alias for "_id" (GraphQL convention)
			// This allows GraphQL queries to use "id" instead of "_id"
			if fieldName == "_id" {
				idRow := []any{
					c.db.Name(), // table_schema
					collName,    // table_name
					"id",        // column_name (alias for _id)
					field.SQLType,
					false, // is_nullable (id is required)
					true,  // is_primary_key
					true,  // is_unique_key
					false, // is_array
					false, // is_fulltext
					"",    // fkey_schema
					"",    // fkey_table
					"",    // fkey_column
				}
				rows = append(rows, idRow)
			}
		}
	}

	return NewColumnRows(columns, rows), nil
}

// introspectInfo returns database metadata (version, schema, name).
// This matches what GraphJin's sdata/tables.go expects to scan.
func (c *Conn) introspectInfo(ctx context.Context, q *QueryDSL) (driver.Rows, error) {
	// Get MongoDB server version
	var result bson.M
	err := c.db.RunCommand(ctx, bson.M{"buildInfo": 1}).Decode(&result)
	if err != nil {
		return nil, fmt.Errorf("mongodriver: buildInfo: %w", err)
	}

	version := "0"
	if v, ok := result["version"].(string); ok {
		// Extract major version number (e.g., "7.0.4" -> 7)
		parts := strings.Split(v, ".")
		if len(parts) > 0 {
			version = parts[0]
		}
	}

	// Convert version to int for GraphJin
	versionInt := 0
	fmt.Sscanf(version, "%d", &versionInt)

	// Return: version, schema (database name), name (database name)
	columns := []string{"version", "schema", "name"}
	rows := [][]any{
		{versionInt, c.db.Name(), c.db.Name()},
	}

	return NewColumnRows(columns, rows), nil
}

// introspectFunctions returns available MongoDB functions/aggregations.
func (c *Conn) introspectFunctions(ctx context.Context, q *QueryDSL) (driver.Rows, error) {
	// MongoDB doesn't have user-defined functions like SQL databases
	// Return empty result
	columns := []string{"function_name", "return_type", "num_params"}
	return NewColumnRows(columns, nil), nil
}

// FieldInfo describes a discovered field.
type FieldInfo struct {
	Name     string
	BSONType string
	SQLType  string
	Required bool
	IsArray  bool
	IsUnique bool
}

// getCollectionValidator retrieves the JSON Schema validator for a collection.
func (c *Conn) getCollectionValidator(ctx context.Context, collName string) map[string]FieldInfo {
	fields := make(map[string]FieldInfo)

	// Get collection info
	cursor, err := c.db.ListCollections(ctx, bson.M{"name": collName})
	if err != nil {
		return fields
	}
	defer cursor.Close(ctx)

	if !cursor.Next(ctx) {
		return fields
	}

	var collInfo struct {
		Options struct {
			Validator struct {
				JSONSchema struct {
					Properties map[string]struct {
						BSONType    any    `bson:"bsonType"`
						Description string `bson:"description"`
					} `bson:"properties"`
					Required []string `bson:"required"`
				} `bson:"$jsonSchema"`
			} `bson:"validator"`
		} `bson:"options"`
	}

	if err := cursor.Decode(&collInfo); err != nil {
		return fields
	}

	requiredSet := make(map[string]bool)
	for _, r := range collInfo.Options.Validator.JSONSchema.Required {
		requiredSet[r] = true
	}

	for name, prop := range collInfo.Options.Validator.JSONSchema.Properties {
		bsonType := normalizeBSONType(prop.BSONType)
		fields[name] = FieldInfo{
			Name:     name,
			BSONType: bsonType,
			SQLType:  bsonTypeToSQL(bsonType),
			Required: requiredSet[name],
			IsArray:  bsonType == "array",
		}
	}

	return fields
}

// sampleCollectionFields samples documents to discover field types.
func (c *Conn) sampleCollectionFields(ctx context.Context, coll *mongo.Collection, sampleSize int) map[string]FieldInfo {
	fields := make(map[string]FieldInfo)

	pipeline := bson.A{
		bson.M{"$sample": bson.M{"size": sampleSize}},
	}

	cursor, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return fields
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}

		for key, val := range doc {
			if _, exists := fields[key]; exists {
				continue // Already discovered
			}

			bsonType := inferBSONType(val)
			fields[key] = FieldInfo{
				Name:     key,
				BSONType: bsonType,
				SQLType:  bsonTypeToSQL(bsonType),
				Required: key == "_id", // Only _id is guaranteed
				IsArray:  bsonType == "array",
			}
		}
	}

	return fields
}

// inferBSONType determines the BSON type from a Go value.
func inferBSONType(v any) string {
	if v == nil {
		return "null"
	}

	switch val := v.(type) {
	case bson.ObjectID:
		return "objectId"
	case string:
		return "string"
	case int, int32, int64:
		return "long"
	case float32, float64:
		return "double"
	case bool:
		return "bool"
	case bson.DateTime:
		return "date"
	case bson.A, []any:
		return "array"
	case bson.M, map[string]any:
		return "object"
	case bson.Binary:
		return "binData"
	default:
		rt := reflect.TypeOf(val)
		if rt != nil && rt.Kind() == reflect.Slice {
			return "array"
		}
		if rt != nil && (rt.Kind() == reflect.Map || rt.Kind() == reflect.Struct) {
			return "object"
		}
		return "string" // Default fallback
	}
}

// normalizeBSONType handles bsonType being string or array.
func normalizeBSONType(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		if len(t) > 0 {
			if s, ok := t[0].(string); ok {
				return s
			}
		}
	case bson.A:
		if len(t) > 0 {
			if s, ok := t[0].(string); ok {
				return s
			}
		}
	}
	return "string"
}

// bsonTypeToSQL maps BSON types to SQL-equivalent types for GraphJin.
func bsonTypeToSQL(bsonType string) string {
	switch bsonType {
	case "objectId":
		return "text" // Could be "uuid" but text is safer
	case "string":
		return "text"
	case "int", "long":
		return "bigint"
	case "double", "decimal":
		return "double precision"
	case "bool":
		return "boolean"
	case "date":
		return "timestamptz"
	case "array":
		return "jsonb"
	case "object":
		return "jsonb"
	case "binData":
		return "bytea"
	case "null":
		return "text"
	default:
		return "text"
	}
}

// inferForeignKey returns empty values since MongoDB doesn't have real foreign keys.
// Users should configure relationships via GraphJin config.yaml tables section.
// Automatic inference is too error-prone (e.g., owner_id -> owners vs users).
func inferForeignKey(fieldName string) (table, column string) {
	// Disabled: MongoDB has no foreign keys, and simple naming conventions
	// like "owner_id" -> "owners" are often wrong (should be "users").
	// Configure relationships in config.yaml instead.
	return "", ""
}

// mergeFields combines schema and discovered fields.
func mergeFields(schema, discovered map[string]FieldInfo) map[string]FieldInfo {
	result := make(map[string]FieldInfo)

	// Start with schema fields (more authoritative)
	for k, v := range schema {
		result[k] = v
	}

	// Add discovered fields not in schema
	for k, v := range discovered {
		if _, exists := result[k]; !exists {
			result[k] = v
		}
	}

	return result
}
