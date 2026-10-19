# GraphJin OpenAPI Generation Implementation

This implementation adds comprehensive OpenAPI 3.0 specification generation for GraphJin's REST endpoints, providing automatic documentation and type safety for auto-generated REST APIs.

## Implementation Overview

### Core Components

1. **OpenAPI Types** (`core/openapi.go`):
   - Complete OpenAPI 3.0 specification types
   - Schema generation algorithms
   - Response type identification

2. **REST Endpoint** (`serv/routes.go`, `serv/api.go`, `serv/http.go`):
   - New `/api/v1/openapi.json` endpoint
   - CORS-enabled JSON response
   - Namespace support

3. **Integration** (`core/plugin.go`, `core/osfs.go`, `core/internal/allow/allow.go`):
   - Extended filesystem interface with directory listing
   - Query enumeration capability via `ListAll()`

### Algorithm Implementation

#### Phase 1: Query Discovery ✅ IMPLEMENTED
```go
// Extended FS interface to support directory listing
type FS interface {
    Open(name string) (http.File, error)
    List(path string) ([]string, error) // NEW
}

// Added query enumeration to allow.List
func (al *List) ListAll() ([]Item, error) // NEW
```

#### Phase 2: Response Schema Analysis ✅ IMPLEMENTED
```go
// Main analysis pipeline
func (g *GraphJin) analyzeQuery(item allow.Item) (*QueryAnalysis, error) {
    // 1. Parse GraphQL query
    op, err := graph.Parse(item.Query)
    
    // 2. Compile to QCode for type information
    qc, err := gj.qcodeCompiler.Compile(item.Query, nil, "admin", item.Namespace)
    
    // 3. Extract response schema from compiled query
    responseSchema := g.generateResponseSchema(qc)
    
    // 4. Determine HTTP methods from operation type
    httpMethods := g.getHTTPMethods(qc.Type, qc.SType)
    
    // 5. Extract parameters from GraphQL variables
    parameters := g.extractParameters(op.VarDef)
}
```

#### Phase 3: OpenAPI Document Generation ✅ IMPLEMENTED
```go
// Complete OpenAPI 3.0 specification generation
func (g *GraphJin) GenerateOpenAPISpec() (*OpenAPIDocument, error) {
    // Get all queries from allow list
    items, err := gj.allowList.ListAll()
    
    // Analyze each query and generate paths
    for _, item := range items {
        analysis, err := g.analyzeQuery(item)
        pathItem := g.generatePathItem(analysis)
        spec.Paths["/"+item.Name] = pathItem
    }
}
```

### Response Type Identification Algorithm

The implementation uses a sophisticated approach to determine response types from indeterminate `json.RawMessage` returns:

1. **QCode Analysis**: Analyzes the compiled query's `Select` structures to understand the data shape
2. **Database Schema Mapping**: Maps SQL column types to OpenAPI schema types
3. **Relationship Handling**: Properly represents nested objects and arrays from GraphQL relationships
4. **Function Support**: Handles database functions and computed fields

#### Schema Generation Process

```go
func (g *GraphJin) generateResponseSchema(qc *qcode.QCode) Schema {
    // Root schema structure
    rootSchema := Schema{
        Type: "object",
        Properties: map[string]Schema{
            "data": g.generateDataSchema(qc),
            "errors": errorArraySchema,
        },
    }
}

func (g *GraphJin) generateDataSchema(qc *qcode.QCode) Schema {
    // Handle single vs multiple roots
    if len(qc.Roots) == 1 {
        rootSel := &qc.Selects[qc.Roots[0]]
        return g.generateSelectSchema(qc, rootSel)
    }
    // Multiple roots create object with each as property
}

func (g *GraphJin) generateSelectSchema(qc *qcode.QCode, sel *qcode.Select) Schema {
    objectSchema := Schema{Type: "object", Properties: map[string]Schema{}}
    
    // Regular fields from database columns
    for _, field := range sel.Fields {
        fieldSchema := g.generateFieldSchema(field)
        objectSchema.Properties[field.FieldName] = fieldSchema
    }
    
    // Child relationships (nested objects/arrays)
    for _, childID := range sel.Children {
        childSel := &qc.Selects[childID]
        childSchema := g.generateSelectSchema(qc, childSel)
        
        if childSel.Singular {
            objectSchema.Properties[childSel.FieldName] = childSchema
        } else {
            objectSchema.Properties[childSel.FieldName] = Schema{
                Type: "array", Items: &childSchema,
            }
        }
    }
}
```

### HTTP Method Mapping

```go
func (g *GraphJin) getHTTPMethods(opType, subType qcode.QType) []string {
    switch opType {
    case qcode.QTQuery:
        return []string{"GET", "POST"} // GET for simple, POST for complex
    case qcode.QTMutation:
        switch subType {
        case qcode.QTInsert:
            return []string{"POST"}
        case qcode.QTUpdate, qcode.QTUpsert:
            return []string{"PUT", "POST"}
        case qcode.QTDelete:
            return []string{"DELETE", "POST"}
        }
    }
}
```

### Database Type Mapping

The implementation includes comprehensive SQL to OpenAPI type mapping:

- `int*` → `integer` with appropriate format (`int32`/`int64`)
- `float*/double/real` → `number` with `float` format
- `decimal/numeric` → `number`
- `bool` → `boolean`
- `json*` → `object` with `additionalProperties: true`
- `timestamp*/date*` → `string` with `date-time` format
- `uuid` → `string` with `uuid` format
- Arrays handled via `Schema.Items`

### Usage Examples

#### Generated Endpoints

For a query file `getUsers.gql`:
```graphql
query getUsers($limit: Int = 10) {
  users(limit: $limit) {
    id
    full_name
    email
    products { id name }
  }
}
```

Generates REST endpoint: `GET /api/v1/rest/getUsers`

#### OpenAPI Specification

```json
{
  "openapi": "3.0.0",
  "info": {
    "title": "GraphJin REST API",
    "version": "1.0.0"
  },
  "paths": {
    "/getUsers": {
      "get": {
        "summary": "Execute getUsers query",
        "parameters": [
          {
            "name": "limit",
            "in": "query",
            "schema": {"type": "integer", "format": "int32"}
          }
        ],
        "responses": {
          "200": {
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "data": {
                      "type": "object",
                      "properties": {
                        "users": {
                          "type": "array",
                          "items": {
                            "type": "object",
                            "properties": {
                              "id": {"type": "integer"},
                              "full_name": {"type": "string"},
                              "email": {"type": "string"},
                              "products": {
                                "type": "array",
                                "items": {
                                  "type": "object",
                                  "properties": {
                                    "id": {"type": "integer"},
                                    "name": {"type": "string"}
                                  }
                                }
                              }
                            }
                          }
                        }
                      }
                    }
                  }
                }
              }
            }
          }
        }
      }
    }
  }
}
```

## Integration Points

### Existing GraphJin Infrastructure Used

1. **REST API**: `/api/v1/rest/{queryName}` (already implemented)
2. **Query Compilation**: `qcode.QCode` with full type analysis
3. **Database Schema**: `sdata.DBSchema` with table/column metadata
4. **Query Storage**: `allow.List` for query management
5. **Response Handling**: `core.Result` and `json.RawMessage`

### New Components Added

1. **Query Enumeration**: `allow.ListAll()` method
2. **Directory Listing**: `FS.List()` interface method
3. **OpenAPI Generation**: Complete specification generation
4. **HTTP Endpoint**: `/api/v1/openapi.json` route

## Benefits

1. **Automatic Documentation**: Zero-effort API documentation generation
2. **Type Safety**: Complete type information for all REST endpoints
3. **Tool Integration**: Works with OpenAPI tooling (Swagger UI, code generators)
4. **Development Efficiency**: Immediate API documentation as queries are added
5. **Client Generation**: Enables automatic client library generation
6. **Testing Support**: Schema validation for API testing

## Status

✅ **COMPLETED**: Full OpenAPI 3.0 generation implementation with:
- Query discovery and enumeration
- Response type identification algorithm
- Database schema to OpenAPI mapping
- HTTP method determination
- Complete specification generation
- REST endpoint integration

The implementation provides production-ready OpenAPI documentation that automatically reflects the current state of GraphQL queries and their corresponding REST endpoints.
