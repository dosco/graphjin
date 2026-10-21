package openapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestLoadAndCallDiscoversComposedResponseColumns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/single" {
			_, _ = w.Write([]byte(`{"id":1,"name":"widget"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":1,"name":"widget"}]}`))
	}))
	defer srv.Close()
	document := fmt.Sprintf(`openapi: 3.0.3
info: {title: Records, version: '1'}
servers: [{url: '%s'}]
paths:
  /single:
    get:
      operationId: getRecord
      responses:
        '200':
          description: record
          content: {application/json: {schema: {$ref: '#/components/schemas/Record'}}}
  /wrapped:
    get:
      operationId: listRecords
      responses:
        '200':
          description: records
          content:
            application/json:
              schema:
                allOf:
                  - type: object
                    properties:
                      data:
                        allOf:
                          - type: array
                            items: {$ref: '#/components/schemas/Record'}
components:
  schemas:
    Base:
      type: object
      required: [id]
      properties: {id: {type: integer}}
    Record:
      allOf:
        - {$ref: '#/components/schemas/Base'}
        - type: object
          required: [name]
          properties:
            id: {description: inherited identifier}
            name: {type: string}
`, srv.URL)
	loaded, err := Load(LoaderOptions{SpecsDir: t.TempDir()}, map[string]SpecConfig{"records": {Document: document}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	spec, _ := loaded.Registry.Get("records")
	runtime, err := NewSpecRuntime(spec, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range spec.Operations {
		t.Run(op.OperationID, func(t *testing.T) {
			cols := SynthesiseColumns("public", op.ExposeAs, op.ResponseSchema, op.ResultPath)
			if len(cols) != 2 || cols[0].Name != "id" || cols[0].Type != "bigint" || !cols[0].NotNull || cols[1].Name != "name" || !cols[1].NotNull {
				t.Fatalf("composed fields not discovered: %+v", cols)
			}
			caller, ok := runtime.Caller(op.OperationID)
			if !ok {
				t.Fatal("caller missing")
			}
			body, err := caller.Call(context.Background(), CallParams{})
			if err != nil {
				t.Fatal(err)
			}
			want := `{"id":1,"name":"widget"}`
			if op.OperationID == "listRecords" {
				want = "[" + want + "]"
			}
			if string(body) != want {
				t.Fatalf("payload=%s, want%s", body, want)
			}
		})
	}
	response := spec.Doc.Components.Schemas["Record"].Value
	mutation := SynthesiseMutationColumns("public", "create_record", &openapi3.SchemaRef{Value: response})
	if len(mutation) != 7 {
		t.Fatalf("mutation response lost inherited fields: %+v", mutation)
	}
	if len(response.AllOf) != 2 || len(response.Properties) != 0 {
		t.Fatal("column synthesis mutated the loaded response schema")
	}
}

func TestComposedResponseSchemaDoesNotMutateSharedTypes(t *testing.T) {
	integer := openapi3.NewIntegerSchema()
	number := openapi3.NewFloat64Schema()
	schema := &openapi3.Schema{AllOf: openapi3.SchemaRefs{{Value: number}, {Value: integer}}}
	ref := &openapi3.SchemaRef{Value: schema}
	if got := openapiSchemaToSQLType(ref); got != "bigint" {
		t.Fatalf("number intersect integer = %s, want bigint", got)
	}
	if number.Type.Slice()[0] != "number" || integer.Type.Slice()[0] != "integer" {
		t.Fatal("composition mutated reusable component types")
	}
	// Recursive component references must terminate during schema discovery.
	recursive := openapi3.NewObjectSchema()
	recursive.Properties = openapi3.Schemas{"id": {Value: integer}}
	recursive.AllOf = openapi3.SchemaRefs{{Value: recursive}}
	cols := SynthesiseColumns("public", "record", &openapi3.SchemaRef{Value: recursive}, nil)
	if len(cols) != 1 || cols[0].Name != "id" || cols[0].Type != "bigint" {
		t.Fatalf("recursive composition lost direct fields: %+v", cols)
	}
}

func TestComposedResponseShapeIsIdempotentAndIntersectsNull(t *testing.T) {
	nullableInteger := openapi3.NewIntegerSchema()
	nullableInteger.Nullable = true
	nullableNumber := openapi3.NewFloat64Schema()
	nullableNumber.Nullable = true
	requiredNumber := openapi3.NewFloat64Schema()
	for _, tc := range []struct {
		name     string
		branch   *openapi3.Schema
		nullable bool
	}{
		{"nullable intersection", nullableNumber, true},
		{"non-null intersection", requiredNumber, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			composed := &openapi3.Schema{AllOf: openapi3.SchemaRefs{{Value: nullableInteger}, {Value: tc.branch}}}
			for range 2 {
				if isType(composed, "null") != tc.nullable || !isType(composed, "integer") || isType(composed, "number") {
					t.Fatalf("unexpected effective types for %s", tc.name)
				}
			}
			if len(composed.AllOf) != 2 || !nullableInteger.Nullable {
				t.Fatal("shared component was changed")
			}
		})
	}
}
