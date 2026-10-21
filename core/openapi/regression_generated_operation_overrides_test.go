package openapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoadAndCallAppliesOverridesToGeneratedOperationIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/teams/team-7/members" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[{"id":42}]`))
	}))
	defer srv.Close()
	document := fmt.Sprintf(`openapi: 3.0.3
info: {title: Teams, version: '1'}
servers: [{url: '%s'}]
paths:
  /teams/{teamId}/members:
    get:
      parameters:
        - {name: teamId, in: path, required: true, schema: {type: string}}
      responses:
        '200':
          description: members
          content:
            application/json:
              schema: {type: array, items: {type: object, properties: {id: {type: integer}}}}
`, srv.URL)
	loaded, err := Load(LoaderOptions{SpecsDir: t.TempDir()}, map[string]SpecConfig{"teams": {
		Document:   document,
		Operations: map[string]OperationOverride{"get_teams_teamid_members": {ExposeTopLevel: true, ExposeAs: "team_members", Defaults: map[string]string{"teamid": "team-7"}}},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	spec, ok := loaded.Registry.Get("teams")
	if !ok || len(spec.Operations) != 1 {
		t.Fatal("operation missing")
	}
	op := spec.Operations[0]
	if op.Mode != OpModeList || op.ExposeAs != "team_members" {
		t.Fatalf("generated operation override ignored: %+v", op)
	}
	params, err := op.ResolveCallParams(nil)
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
	body, err := caller.Call(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `[{"id":42}]` {
		t.Fatalf("response=%s", body)
	}
}
