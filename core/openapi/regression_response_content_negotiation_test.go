package openapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoadedCallersNegotiateDeclaredResponseMediaTypes(t *testing.T) {
	const vendor = "application/vnd.api+json"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept"), vendor) {
			http.Error(w, "JSON:API Accept required", 406)
			return
		}
		if r.Method == "POST" && r.Header.Get("Content-Type") != "application/json" {
			t.Error("request media type changed")
		}
		w.Header().Set("Content-Type", vendor)
		if r.Method == "POST" {
			w.WriteHeader(201)
		}
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()
	document := fmt.Sprintf(`openapi: 3.0.3
info: {title: Records, version: '1'}
servers: [{url: '%s'}]
paths:
  /records:
    get:
      operationId: getRecord
      responses:
        '200':
          description: record
          content:
            application/vnd.api+json:
              schema: {type: object, properties: {id: {type: integer}}}
    post:
      operationId: createRecord
      requestBody:
        content:
          application/json:
            schema: {type: object}
      responses:
        '201':
          description: created
          content:
            application/vnd.api+json:
              schema: {type: object, properties: {id: {type: integer}}}
`, srv.URL)
	loaded, err := Load(LoaderOptions{SpecsDir: t.TempDir()}, map[string]SpecConfig{"records": {Document: document, Operations: map[string]OperationOverride{"createRecord": {ExposeMutation: true}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	spec, _ := loaded.Registry.Get("records")
	runtime, err := NewSpecRuntime(spec, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range spec.Operations {
		t.Run(op.Method, func(t *testing.T) {
			caller, ok := runtime.Caller(op.OperationID)
			if !ok {
				t.Fatalf("operation skipped: %s", op.SkipReason)
			}
			if op.Mode == OpModeMutation {
				params, err := op.ResolveMutationCall(map[string]interface{}{"body": map[string]interface{}{}})
				if err != nil {
					t.Fatal(err)
				}
				result, err := caller.CallMutation(context.Background(), params)
				if err != nil {
					t.Fatal(err)
				}
				if result.StatusCode != 201 {
					t.Fatalf("status=%d", result.StatusCode)
				}
			} else {
				if _, err := caller.Call(context.Background(), CallParams{}); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
