package openapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoadAndCallUsesServerVariableDefaults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/widgets" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[{"id":1}]`))
	}))
	defer srv.Close()
	document := fmt.Sprintf(`openapi: 3.0.3
info: {title: Widgets, version: '1'}
servers:
  - url: '%s/{version}'
    variables:
      version:
        default: v1
        enum: [v1, v2]
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses:
        '200':
          description: widgets
          content:
            application/json:
              schema: {type: array, items: {type: object, properties: {id: {type: integer}}}}
`, srv.URL)
	for _, override := range []string{"", srv.URL + "/v1"} {
		t.Run(override, func(t *testing.T) {
			loaded, err := Load(LoaderOptions{SpecsDir: t.TempDir()}, map[string]SpecConfig{"widgets": {Document: document, BaseURL: override}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			spec, ok := loaded.Registry.Get("widgets")
			if !ok {
				t.Fatal("spec missing")
			}
			runtime, err := NewSpecRuntime(spec, srv.Client())
			if err != nil {
				t.Fatal(err)
			}
			caller, ok := runtime.Caller("listWidgets")
			if !ok {
				t.Fatal("caller missing")
			}
			body, err := caller.Call(context.Background(), CallParams{})
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != `[{"id":1}]` {
				t.Fatalf("response=%s", body)
			}
		})
	}
}
