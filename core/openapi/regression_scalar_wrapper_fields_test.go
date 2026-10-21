package openapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoadAndCallKeepsScalarWrapperNamedProperties(t *testing.T) {
	for _, name := range []string{"data", "items", "results", "records"} {
		t.Run(name, func(t *testing.T) {
			response := fmt.Sprintf(`{"%s":"plain text","id":7}`, name)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(response)) }))
			defer srv.Close()
			document := fmt.Sprintf(`openapi: 3.0.3
info: {title: Record, version: '1'}
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
                properties:
                  id: {type: integer}
                  %s: {type: string}
`, name)
			loaded, err := Load(LoaderOptions{SpecsDir: t.TempDir()}, map[string]SpecConfig{"record": {Document: document, BaseURL: srv.URL}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			spec, ok := loaded.Registry.Get("record")
			if !ok || len(spec.Operations) != 1 {
				t.Fatalf("record not loaded: %+v", loaded)
			}
			op := spec.Operations[0]
			cols := SynthesiseColumns("public", op.ExposeAs, op.ResponseSchema, op.ResultPath)
			if len(cols) != 2 {
				t.Fatalf("record response lost its fields: %+v; result_path=%v", cols, op.ResultPath)
			}
			runtime, err := NewSpecRuntime(spec, srv.Client())
			if err != nil {
				t.Fatal(err)
			}
			caller, ok := runtime.Caller("getRecord")
			if !ok {
				t.Fatal("caller missing")
			}
			body, err := caller.Call(context.Background(), CallParams{})
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != response {
				t.Fatalf("response was unexpectedly unwrapped: got %s, want %s", body, response)
			}
		})
	}
}
