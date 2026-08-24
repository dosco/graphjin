package openapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSpecRuntimeAppliesPerSpecTimeout(t *testing.T) {
	tests := []struct {
		name    string
		value   time.Duration
		expects time.Duration
	}{
		{name: "default", expects: DefaultTimeout},
		{name: "configured", value: 125 * time.Millisecond, expects: 125 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := &Spec{Key: "timeout", Timeout: tt.value, Operations: []OpDescriptor{{
				OperationID: "list", Method: http.MethodGet, PathTemplate: "/items", Mode: OpModeList,
			}}}
			runtime, err := NewSpecRuntime(spec, &http.Client{})
			if err != nil {
				t.Fatal(err)
			}
			caller, ok := runtime.Caller("list")
			if !ok {
				t.Fatal("caller not registered")
			}
			if caller.http.Timeout != tt.expects {
				t.Fatalf("HTTP timeout = %s, want %s", caller.http.Timeout, tt.expects)
			}
		})
	}

	t.Run("preserves stricter host timeout", func(t *testing.T) {
		spec := &Spec{Key: "timeout", Timeout: time.Second, Operations: []OpDescriptor{{
			OperationID: "list", Method: http.MethodGet, PathTemplate: "/items", Mode: OpModeList,
		}}}
		runtime, err := NewSpecRuntime(spec, &http.Client{Timeout: 50 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		caller, ok := runtime.Caller("list")
		if !ok {
			t.Fatal("caller not registered")
		}
		if caller.http.Timeout != 50*time.Millisecond {
			t.Fatalf("HTTP timeout = %s, want stricter host timeout 50ms", caller.http.Timeout)
		}
	})
}

func TestSpecRuntimeRejectsNegativeTimeout(t *testing.T) {
	_, err := NewSpecRuntime(&Spec{Key: "invalid", Timeout: -time.Second}, &http.Client{})
	if err == nil || !strings.Contains(err.Error(), "timeout must not be negative") {
		t.Fatalf("negative timeout error = %v", err)
	}
}

// TestEndToEndLoadAndCall exercises the full pipeline: spec on disk →
// loader → runtime → caller hitting an httptest server. This is the
// closest we can get to "drop a spec into config/specs and it works"
// without spinning up a real GraphJin engine.
func TestEndToEndLoadAndCall(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "interaction_studio.yaml")
	specYAML := `
openapi: 3.0.0
info: { title: IS, version: '1.0' }
paths:
  /users/{userId}:
    get:
      operationId: getUserById
      parameters:
        - { name: userId, in: path, required: true, schema: { type: string } }
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  data:
                    type: object
                    properties:
                      id: { type: string }
                      email: { type: string }
  /audit-logs:
    get:
      operationId: listAuditLogs
      parameters:
        - { name: actorId, in: query, schema: { type: string } }
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  data:
                    type: array
                    items: { type: object }
  /exports/{jobId}:
    get:
      operationId: downloadExport
      parameters:
        - { name: jobId, in: path, required: true, schema: { type: string } }
      responses:
        '200':
          description: ok
          content: { application/octet-stream: { schema: { type: string, format: binary } } }
`
	if err := os.WriteFile(specPath, []byte(specYAML), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	// Mock upstream — receives both row-join and list calls.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if got := r.Header.Get("Authorization"); got != "Bearer is-tok" {
			t.Errorf("Authorization = %q", got)
		}
		switch r.URL.Path {
		case "/users/u-42":
			_, _ = w.Write([]byte(`{"data":{"id":"u-42","email":"u@x"}}`))
		case "/audit-logs":
			if r.URL.Query().Get("actorId") != "u-42" {
				t.Errorf("actorId = %q", r.URL.Query().Get("actorId"))
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"e1"},{"id":"e2"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	configs := map[string]SpecConfig{
		"interaction_studio": {
			BaseURL: srv.URL,
			Auth: AuthConfig{
				Scheme: "bearer",
				Token:  "is-tok",
			},
			Joins: map[string]JoinConfig{
				"getUserById": {
					ParentTable:  "users",
					ParentColumn: "email",
					Param:        "userId",
					ExposeAs:     "is_profile",
				},
			},
		},
	}

	res, err := Load(LoaderOptions{SpecsDir: dir}, configs, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if res.Registry == nil || len(res.Registry.Specs) != 1 {
		t.Fatalf("registry: %+v", res.Registry)
	}

	// Verify classification: 1 row-join, 1 list, 1 skipped (binary).
	spec := res.Registry.Specs[0]
	var rowJoin, list, skipped int
	for _, op := range spec.Operations {
		switch op.Mode {
		case OpModeRowJoin:
			rowJoin++
		case OpModeList:
			list++
		case OpModeSkipped:
			skipped++
		}
	}
	if rowJoin != 1 || list != 1 || skipped != 1 {
		t.Errorf("classification: rowJoin=%d list=%d skipped=%d (want 1,1,1) — ops=%+v", rowJoin, list, skipped, spec.Operations)
	}

	rt, _, err := NewRuntime(res.Registry, srv.Client())
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	// Row-join call: parent column value flows through PathValues.
	caller, ok := rt.Caller("interaction_studio", "getUserById")
	if !ok {
		t.Fatal("getUserById caller not registered")
	}
	body, err := caller.Call(context.Background(), CallParams{
		PathValues: map[string]string{"userId": "u-42"},
	})
	if err != nil {
		t.Fatalf("row-join call: %v", err)
	}
	// ResultPath strips {data: ...} wrapper.
	if string(body) != `{"email":"u@x","id":"u-42"}` && string(body) != `{"id":"u-42","email":"u@x"}` {
		t.Errorf("row-join body = %s", body)
	}

	// List call: query params flow through QueryValues.
	caller, ok = rt.Caller("interaction_studio", "listAuditLogs")
	if !ok {
		t.Fatal("listAuditLogs caller not registered")
	}
	body, err = caller.Call(context.Background(), CallParams{
		QueryValues: map[string]string{"actorId": "u-42"},
	})
	if err != nil {
		t.Fatalf("list call: %v", err)
	}
	if string(body) != `[{"id":"e1"},{"id":"e2"}]` {
		t.Errorf("list body = %s", body)
	}

	// Skipped op is not a registered caller.
	if _, ok := rt.Caller("interaction_studio", "downloadExport"); ok {
		t.Error("downloadExport should not have a registered caller (binary response)")
	}
}

func TestLoadCanonicalisesLowercaseOpKeys(t *testing.T) {
	dir := t.TempDir()
	specYAML := `
openapi: 3.0.0
info: { title: Test, version: '1.0' }
paths:
  /api/dataset/{datasetId}/users.json:
    get:
      operationId: exportUsers
      parameters:
        - { name: datasetId, in: path, required: true, schema: { type: string } }
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  items:
                    type: array
                    items: { type: object, properties: { id: { type: string } } }
`
	if err := os.WriteFile(filepath.Join(dir, "is.yaml"), []byte(specYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Simulate viper's lowercasing — user wrote exportUsers, viper stored it as exportusers.
	configs := map[string]SpecConfig{
		"is": {
			Operations: map[string]OperationOverride{
				"exportusers": {ExposeTopLevel: true, ExposeAs: "is_users"},
			},
		},
	}

	res, err := Load(LoaderOptions{SpecsDir: dir}, configs, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(res.Registry.Specs) != 1 || len(res.Registry.Specs[0].Operations) != 1 {
		t.Fatalf("expected 1 spec with 1 op, got %+v", res.Registry)
	}
	op := res.Registry.Specs[0].Operations[0]
	if op.Mode != OpModeList {
		t.Errorf("Mode = %v, want OpModeList — case-folded override should still match", op.Mode)
	}
	if op.ExposeAs != "is_users" {
		t.Errorf("ExposeAs = %q, want is_users", op.ExposeAs)
	}
}

func TestLoadMissingDirectoryIsBenign(t *testing.T) {
	res, err := Load(LoaderOptions{SpecsDir: "/nonexistent/path/xyz"}, nil, nil)
	if err != nil {
		t.Errorf("missing dir should not error, got %v", err)
	}
	if res.Registry == nil {
		t.Error("Registry should be non-nil even when no specs found")
	}
}

func TestLoadRequiresAndRetainsOwningSource(t *testing.T) {
	dir := t.TempDir()
	specYAML := `
openapi: 3.0.0
info: { title: External API, version: '1.0' }
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema: { type: array, items: { type: object } }
`
	if err := os.WriteFile(filepath.Join(dir, "external.yaml"), []byte(specYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	missing, err := Load(LoaderOptions{SpecsDir: dir, RequireSource: true}, map[string]SpecConfig{"external": {}}, nil)
	if err != nil {
		t.Fatalf("load unowned spec: %v", err)
	}
	if len(missing.Registry.Specs) != 0 || len(missing.Warnings) == 0 {
		t.Fatalf("unowned source should be skipped with a warning: %+v", missing)
	}

	owned, err := Load(LoaderOptions{SpecsDir: dir, RequireSource: true}, map[string]SpecConfig{
		"external": {SourceName: "external_api"},
	}, nil)
	if err != nil {
		t.Fatalf("load owned spec: %v", err)
	}
	if len(owned.Registry.Specs) != 1 || owned.Registry.Specs[0].SourceName != "external_api" ||
		len(owned.Registry.Specs[0].Operations) != 1 || owned.Registry.Specs[0].Operations[0].SourceName != "external_api" {
		t.Fatalf("source provenance was not retained: %+v", owned.Registry.Specs)
	}
}

func TestGenericMutationFixtureCoversVerbAndStatusMatrix(t *testing.T) {
	overrides := map[string]OperationOverride{}
	for _, operationID := range []string{"createWidget", "replaceWidget", "patchWidget", "deleteWidget", "oversizedResponse"} {
		overrides[operationID] = OperationOverride{ExposeMutation: true, AllowedRoles: []string{"operator"}}
	}
	res, err := Load(LoaderOptions{SpecsDir: "testdata", RequireSource: true}, map[string]SpecConfig{
		"generic_mutations": {SourceName: "fixture_api", Operations: overrides},
	}, nil)
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	spec, ok := res.Registry.Get("generic_mutations")
	if !ok {
		t.Fatal("generic mutation fixture was not loaded")
	}
	want := map[string]struct {
		method    string
		status    int
		mediaType string
	}{
		"createWidget":      {http.MethodPost, http.StatusCreated, "application/json"},
		"replaceWidget":     {http.MethodPut, http.StatusOK, "application/json"},
		"patchWidget":       {http.MethodPatch, http.StatusAccepted, "application/merge-patch+json"},
		"deleteWidget":      {http.MethodDelete, http.StatusNoContent, ""},
		"oversizedResponse": {http.MethodPost, http.StatusOK, "application/json"},
	}
	for i := range spec.Operations {
		op := &spec.Operations[i]
		expected, exists := want[op.OperationID]
		if !exists {
			continue
		}
		if op.Mode != OpModeMutation || op.Method != expected.method || len(op.SuccessStatuses) != 1 || op.SuccessStatuses[0] != expected.status || op.RequestMediaType != expected.mediaType {
			t.Fatalf("operation %s = %+v, want method=%s status=%d media=%q", op.OperationID, op, expected.method, expected.status, expected.mediaType)
		}
		delete(want, op.OperationID)
	}
	if len(want) != 0 {
		t.Fatalf("fixture operations missing from registry: %+v", want)
	}
}

// TestLoadIgnoresNonYAMLFiles confirms the loader's filter — a stray
// README.md or .json file in config/specs shouldn't trip the parser.
func TestLoadIgnoresNonYAMLFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Load(LoaderOptions{SpecsDir: dir}, nil, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(res.Registry.Specs) != 0 {
		t.Errorf("expected 0 specs, got %d", len(res.Registry.Specs))
	}
}

// TestLoadParseErrorIsWarning confirms a malformed spec produces a
// warning rather than aborting load — other specs should still be
// processed.
func TestLoadParseErrorIsWarning(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("not: { valid: openapi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "good.yaml"), []byte(`
openapi: 3.0.0
info: { title: ok, version: '1' }
paths:
  /things:
    get:
      operationId: listThings
      responses:
        '200':
          description: ok
          content: { application/json: { schema: { type: object } } }
`), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Load(LoaderOptions{SpecsDir: dir}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Bad spec produces a warning; good spec loads successfully.
	if len(res.Registry.Specs) != 1 {
		t.Errorf("expected 1 spec loaded, got %d", len(res.Registry.Specs))
	}
	var sawBadWarning bool
	for _, w := range res.Warnings {
		if contains(w, "bad.yaml") {
			sawBadWarning = true
		}
	}
	if !sawBadWarning {
		t.Errorf("expected warning mentioning bad.yaml, got %v", res.Warnings)
	}
}
