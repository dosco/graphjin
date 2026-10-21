package openapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestRequiredQueryArgumentsAreResolvedBeforeHTTP(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Query().Get("tenantId") != "tenant-7" {
			http.Error(w, "tenant missing", 400)
			return
		}
		_, _ = w.Write([]byte(`[{"id":1}]`))
	}))
	defer srv.Close()
	document := fmt.Sprintf(`openapi: 3.0.3
info: {title: Records, version: '1'}
servers: [{url: '%s'}]
paths:
  /records:
    get:
      operationId: listRecords
      parameters:
        - {name: tenantId, in: query, required: true, schema: {type: string}}
      responses:
        '200':
          description: records
          content:
            application/json:
              schema: {type: array, items: {type: object, properties: {id: {type: integer}}}}
`, srv.URL)
	for _, tc := range []struct {
		name           string
		args, defaults map[string]string
		wantError      bool
	}{
		{name: "missing", wantError: true},
		{name: "provided", args: map[string]string{"tenantId": "tenant-7"}},
		{name: "configured default", defaults: map[string]string{"tenantid": "tenant-7"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loaded, err := Load(LoaderOptions{SpecsDir: t.TempDir()}, map[string]SpecConfig{"records": {Document: document, Operations: map[string]OperationOverride{"listRecords": {Defaults: tc.defaults}}}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			spec, _ := loaded.Registry.Get("records")
			op := spec.Operations[0]
			before := requests.Load()
			params, err := op.ResolveCallParams(tc.args)
			if tc.wantError {
				if err == nil {
					t.Fatal("missing required query argument was accepted")
				}
				if requests.Load() != before {
					t.Fatal("invalid input reached upstream")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			runtime, err := NewSpecRuntime(spec, srv.Client())
			if err != nil {
				t.Fatal(err)
			}
			caller, ok := runtime.Caller(op.OperationID)
			if !ok {
				t.Fatal("caller missing")
			}
			if _, err := caller.Call(context.Background(), params); err != nil {
				t.Fatal(err)
			}
		})
	}
}
