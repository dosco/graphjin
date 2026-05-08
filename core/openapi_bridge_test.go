package core

import (
	"context"
	"io"
	_log "log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dosco/graphjin/core/v3/openapi"
)

// silentLogger discards log output so tests don't pollute stdout.
// We don't need to assert on log content for these tests; the warnings
// are exercised via the LoadResult.Warnings slice instead.
func silentLogger(t *testing.T) *_log.Logger {
	t.Helper()
	return _log.New(io.Discard, "", 0)
}

// TestSynthesiseRowJoinResolvers verifies the bridge converts row-join
// operations into the ResolverConfig shape that initRemote consumes.
// Operations in other modes (skipped, top-level) must NOT produce
// ResolverConfigs since they don't fit the row-join machinery.
func TestSynthesiseRowJoinResolvers(t *testing.T) {
	reg := &openapi.Registry{
		Specs: []*openapi.Spec{{
			Key: "is",
			Operations: []openapi.OpDescriptor{
				{
					SpecKey:      "is",
					OperationID:  "getUserById",
					Mode:         openapi.OpModeRowJoin,
					ExposeAs:     "is_profile",
					PathTemplate: "/users/{userId}",
					PathParams:   []openapi.ParamSpec{{Name: "userId", In: openapi.ParamInPath}},
					Join: &openapi.JoinConfig{
						ParentTable:  "users",
						ParentColumn: "email",
						Param:        "userId",
					},
				},
				{
					// Skipped op should not produce a ResolverConfig.
					OperationID: "exportFile",
					Mode:        openapi.OpModeSkipped,
				},
				{
					// Top-level list op also shouldn't produce a row-join
					// ResolverConfig (top-level support is a separate path).
					OperationID: "listAuditLogs",
					Mode:        openapi.OpModeList,
				},
			},
		}},
	}

	got := synthesiseRowJoinResolvers(reg)
	if len(got) != 1 {
		t.Fatalf("synthesised %d configs, want 1: %+v", len(got), got)
	}
	rc := got[0]
	if rc.Type != "openapi" {
		t.Errorf("Type = %q", rc.Type)
	}
	if rc.Name != "is_profile" || rc.Table != "users" || rc.Column != "email" {
		t.Errorf("Name/Table/Column = %q/%q/%q", rc.Name, rc.Table, rc.Column)
	}
	if rc.Props["spec_key"] != "is" || rc.Props["operation_id"] != "getUserById" {
		t.Errorf("props = %+v", rc.Props)
	}
	if rc.Props["path_param"] != "userId" {
		t.Errorf("path_param = %v", rc.Props["path_param"])
	}
}

// TestOpenAPIBridgeResolve verifies the bridge translates the existing
// ResolverReq (parent column value as req.ID) into an OpenAPI CallParams
// with the join key in the right path-parameter slot.
func TestOpenAPIBridgeResolve(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/u-99" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"u-99","name":"Test"}`))
	}))
	defer srv.Close()

	op := openapi.OpDescriptor{
		OperationID:  "getUserById",
		Method:       "GET",
		PathTemplate: "/users/{userId}",
		Mode:         openapi.OpModeRowJoin,
		PathParams:   []openapi.ParamSpec{{Name: "userId", In: openapi.ParamInPath, Required: true}},
	}
	spec := &openapi.Spec{
		Key:        "is",
		BaseURL:    srv.URL,
		Operations: []openapi.OpDescriptor{op},
	}
	rt, err := openapi.NewSpecRuntime(spec, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	caller, ok := rt.Caller("getUserById")
	if !ok {
		t.Fatal("caller not found")
	}

	bridge := &openapiBridge{caller: caller, pathName: "userId"}
	body, err := bridge.Resolve(context.Background(), ResolverReq{ID: "u-99"})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"id":"u-99","name":"Test"}` {
		t.Errorf("body = %s", body)
	}
}

// TestLoadOpenAPIIntegrationDormantWithoutSpecs confirms that a deployment
// with no config/specs directory boots cleanly — OpenAPI integration
// must be optional, never required.
func TestLoadOpenAPIIntegrationDormantWithoutSpecs(t *testing.T) {
	// Use an explicit non-existent dir rather than the default; the
	// default may exist on a developer's box in the wrong way.
	conf := &Config{OpenAPISpecsDir: filepath.Join(os.TempDir(), "graphjin-test-nonexistent-xyz")}
	gj := &graphjinEngine{conf: conf, log: silentLogger(t)}
	if err := gj.loadOpenAPIIntegration(); err != nil {
		t.Errorf("loadOpenAPIIntegration with no specs should not error, got %v", err)
	}
	if gj.openapiRuntime != nil {
		t.Error("runtime should be nil when no specs are loaded")
	}
}
