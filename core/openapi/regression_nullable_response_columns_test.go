package openapi

import (
	"fmt"
	"testing"
)

func TestLoadedResponseColumnsHonorNullableSchemas(t *testing.T) {
	for _, schema := range []struct{ name, version, property string }{
		{"openapi30", "3.0.3", "type: integer, nullable: true"},
		{"openapi31", "3.1.0", "type: ['null', integer]"},
	} {
		t.Run(schema.name, func(t *testing.T) {
			document := fmt.Sprintf(`openapi: %s
info: {title: Records, version: '1'}
paths:
  /record:
    get:
      operationId: getRecord
      responses:
        '200':
          description: record
          content:
            application/json:
              schema:
                type: object
                required: [id, next_id]
                properties:
                  id: {type: integer}
                  next_id: {%s}
`, schema.version, schema.property)
			loaded, err := Load(LoaderOptions{SpecsDir: t.TempDir()}, map[string]SpecConfig{"record": {Document: document}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			spec, ok := loaded.Registry.Get("record")
			if !ok || len(spec.Operations) != 1 {
				t.Fatalf("record not loaded: %+v", loaded)
			}
			op := spec.Operations[0]
			query := SynthesiseColumns("public", op.ExposeAs, op.ResponseSchema, op.ResultPath)
			mutation := SynthesiseMutationColumns("public", op.ExposeAs, op.ResponseSchema)
			for _, cols := range []bool{false, true} {
				fields := query
				if cols {
					fields = mutation
				}
				found := false
				for _, field := range fields {
					if field.Name == "id" && !field.NotNull {
						t.Error("required non-null id became nullable")
					}
					if field.Name == "next_id" {
						found = true
						if field.NotNull || field.Type != "bigint" {
							t.Errorf("nullable integer next_id = %+v", field)
						}
					}
				}
				if !found {
					t.Fatal("next_id column missing")
				}
			}
		})
	}
}
