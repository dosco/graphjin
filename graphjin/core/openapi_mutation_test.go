package core

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	_log "log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/openapi"
	"github.com/dosco/graphjin/core/v3/sourcecap"
	_ "github.com/mattn/go-sqlite3"
)

func TestOpenAPIMutationGraphQLRoundTripAndAuthorization(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/widgets" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Errorf("authorization header = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("X-Request-Id") == "" {
			t.Errorf("headers = %#v", r.Header)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["name"] != "canary" {
			t.Errorf("request body = %#v, err=%v", body, err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"w-1","name":"canary","enabled":true}`))
	}))
	defer upstream.Close()

	gj := newOpenAPIMutationTestEngine(t, upstream.URL, false)
	ctx := context.WithValue(context.Background(), UserIDKey, "user-1")
	ctx = context.WithValue(ctx, IdentityRolesKey, []string{"api_executor"})
	vars := json.RawMessage(`{"request":{"body":{"name":"canary","enabled":true}}}`)
	result, err := gj.GraphQL(ctx, `mutation ($request: JSON!) {
  create_widget(call: $request) { ok status_code operation_id request_id response_json id name enabled }
}`, vars, nil)
	if err != nil {
		t.Fatalf("GraphQL: %v", err)
	}
	var response struct {
		Create struct {
			OK           bool                   `json:"ok"`
			StatusCode   int                    `json:"status_code"`
			OperationID  string                 `json:"operation_id"`
			RequestID    string                 `json:"request_id"`
			ResponseJSON map[string]interface{} `json:"response_json"`
			ID           string                 `json:"id"`
			Name         string                 `json:"name"`
			Enabled      bool                   `json:"enabled"`
		} `json:"create_widget"`
	}
	if err := json.Unmarshal(result.Data, &response); err != nil {
		t.Fatalf("decode response: %v (%s)", err, result.Data)
	}
	if !response.Create.OK || response.Create.StatusCode != 201 || response.Create.OperationID != "createWidget" || response.Create.RequestID == "" || response.Create.ID != "w-1" || response.Create.Name != "canary" || !response.Create.Enabled {
		t.Fatalf("unexpected response: %+v (%s)", response.Create, result.Data)
	}
	if response.Create.ResponseJSON["id"] != "w-1" || calls.Load() != 1 {
		t.Fatalf("response_json/calls = %#v/%d", response.Create.ResponseJSON, calls.Load())
	}

	wrongRole := context.WithValue(context.Background(), UserIDKey, "user-2")
	wrongRole = context.WithValue(wrongRole, IdentityRolesKey, []string{"user"})
	_, err = gj.GraphQL(wrongRole, `mutation ($request: JSON!) { create_widget(call: $request) { ok } }`, vars, nil)
	if err == nil || !strings.Contains(err.Error(), "allowed_roles") {
		t.Fatalf("wrong role error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("unauthorized request reached upstream; calls=%d", calls.Load())
	}

	invalid := json.RawMessage(`{"request":{"body":{"enabled":true}}}`)
	_, err = gj.GraphQL(ctx, `mutation ($request: JSON!) { create_widget(call: $request) { ok } }`, invalid, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid request body") {
		t.Fatalf("invalid body error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("invalid body reached upstream; calls=%d", calls.Load())
	}
}

func TestOpenAPIMutationReadOnlySourceBlocksBeforeNetwork(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer upstream.Close()
	gj := newOpenAPIMutationTestEngine(t, upstream.URL, true)
	ctx := context.WithValue(context.Background(), UserIDKey, "user-1")
	ctx = context.WithValue(ctx, IdentityRolesKey, []string{"api_executor"})
	_, err := gj.GraphQL(ctx, `mutation ($request: JSON!) { create_widget(call: $request) { ok } }`, json.RawMessage(`{"request":{"body":{"name":"x"}}}`), nil)
	if err == nil || !strings.Contains(err.Error(), "source.read_only") || calls.Load() != 0 {
		t.Fatalf("error=%v calls=%d", err, calls.Load())
	}
}

func TestOpenAPIMutationExposureIsRemovedOnReload(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer upstream.Close()
	gj := newOpenAPIMutationTestEngine(t, upstream.URL, false)
	engine, err := gj.getEngine()
	if err != nil {
		t.Fatal(err)
	}
	// Full reloads rebuild from catalogConf, the clean caller-owned snapshot,
	// rather than from conf after it has acquired synthesized tables/resolvers.
	next := engine.catalogConf.clone()
	spec := next.Sources[1].Specs["generic_mutations"]
	override := spec.Operations["createWidget"]
	override.ExposeMutation = false
	spec.Operations["createWidget"] = override
	next.Sources[1].Specs["generic_mutations"] = spec
	if err := next.RenormalizeSources(); err != nil {
		t.Fatal(err)
	}
	engine.catalogConf = next

	if err := gj.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	ctx := context.WithValue(context.Background(), UserIDKey, "user-1")
	ctx = context.WithValue(ctx, IdentityRolesKey, []string{"api_executor"})
	_, err = gj.GraphQL(ctx, `mutation ($request: JSON!) { create_widget(call: $request) { ok } }`, json.RawMessage(`{"request":{"body":{"name":"x"}}}`), nil)
	if err == nil || calls.Load() != 0 {
		t.Fatalf("removed mutation remained executable after reload: err=%v calls=%d", err, calls.Load())
	}
}

func TestOpenAPIMutationIntrospectionIsRoleScoped(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer upstream.Close()
	gj := newOpenAPIMutationTestEngine(t, upstream.URL, false)
	engine, err := gj.getEngine()
	if err != nil {
		t.Fatal(err)
	}

	authorized := context.WithValue(context.Background(), UserIDKey, "user-1")
	authorized = context.WithValue(authorized, IdentityRolesKey, []string{"api_executor"})
	data, err := engine.introQueryForContext(authorized)
	if err != nil {
		t.Fatal(err)
	}
	var intro IntroResult
	if err := json.Unmarshal(data, &intro); err != nil {
		t.Fatal(err)
	}
	mutation := introspectionType(intro.Schema.Types, "Mutation")
	query := introspectionType(intro.Schema.Types, "Query")
	root := introspectionField(mutation.Fields, "create_widget")
	if root == nil || len(root.Args) != 1 || root.Args[0].Name != "call" || root.Args[0].Type.Kind != KIND_NONNULL || root.Args[0].Type.OfType == nil || root.Args[0].Type.OfType.Name == nil || *root.Args[0].Type.OfType.Name != TYPE_JSON {
		t.Fatalf("mutation call introspection = %+v", root)
	}
	if introspectionField(query.Fields, "create_widget") != nil {
		t.Fatal("OpenAPI mutation leaked onto query root")
	}

	blocked := context.WithValue(context.Background(), UserIDKey, "user-2")
	blocked = context.WithValue(blocked, IdentityRolesKey, []string{"user"})
	data, err = engine.introQueryForContext(blocked)
	if err != nil {
		t.Fatal(err)
	}
	intro = IntroResult{}
	if err := json.Unmarshal(data, &intro); err != nil {
		t.Fatal(err)
	}
	if introspectionField(introspectionType(intro.Schema.Types, "Mutation").Fields, "create_widget") != nil {
		t.Fatal("blocked role can discover OpenAPI mutation root")
	}

	data, err = engine.getIntroResult()
	if err != nil {
		t.Fatal(err)
	}
	intro = IntroResult{}
	if err := json.Unmarshal(data, &intro); err != nil {
		t.Fatal(err)
	}
	if introspectionField(introspectionType(intro.Schema.Types, "Mutation").Fields, "create_widget") != nil {
		t.Fatal("identity-free cached introspection can discover caller-scoped OpenAPI mutation root")
	}
}

func TestOpenAPIMutationCompletionEventIsSingleAndRedacted(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"event-result","name":"response-secret-must-not-leak"}`))
	}))
	defer upstream.Close()

	gj := newOpenAPIMutationTestEngine(t, upstream.URL, false)
	engine, err := gj.getEngine()
	if err != nil {
		t.Fatal(err)
	}
	var logBuffer bytes.Buffer
	engine.log = _log.New(&logBuffer, "", 0)

	ctx := context.WithValue(context.Background(), UserIDKey, "user-1")
	ctx = context.WithValue(ctx, IdentityRolesKey, []string{"api_executor"})
	vars := json.RawMessage(`{"request":{"body":{"name":"request-secret-must-not-leak"}}}`)
	if _, err := gj.GraphQL(ctx, `mutation ($request: JSON!) { create_widget(call: $request) { ok } }`, vars, nil); err != nil {
		t.Fatalf("GraphQL: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(logBuffer.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("completion event lines = %d, want 1: %q", len(lines), logBuffer.String())
	}
	var event openAPIMutationCompletion
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatalf("decode completion event: %v (%q)", err, lines[0])
	}
	if event.Event != "openapi_mutation_completion" || event.SourceName != "upstream" || event.SpecKey != "generic_mutations" ||
		event.OperationID != "createWidget" || event.Method != http.MethodPost || event.RoleClass != "api_executor" ||
		event.Authorization != "allowed" || event.Gate != "allowed" || event.Outcome != "success" || event.StatusCode != http.StatusCreated ||
		event.RequestID == "" || event.RequestBytes == 0 || event.ResponseBytes == 0 || event.RequestSHA256 == "" || event.ResponseSHA256 == "" || event.RetryCount != 0 {
		t.Fatalf("unexpected completion event: %+v", event)
	}
	logged := logBuffer.String()
	for _, secret := range []string{"fixture-token", "request-secret-must-not-leak", "response-secret-must-not-leak"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("completion event leaked %q: %s", secret, logged)
		}
	}
}

func TestOpenAPIMutationAliasAndRootIsolation(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"w-2","name":"alias"}`))
	}))
	defer upstream.Close()
	gj := newOpenAPIMutationTestEngine(t, upstream.URL, false)
	ctx := context.WithValue(context.Background(), UserIDKey, "user-1")
	ctx = context.WithValue(ctx, IdentityRolesKey, []string{"api_executor"})
	vars := json.RawMessage(`{"request":{"body":{"name":"alias"}}}`)

	result, err := gj.GraphQL(ctx, `mutation ($request: JSON!) { aliased: create_widget(call: $request) { ok name } }`, vars, nil)
	if err != nil || !strings.Contains(string(result.Data), `"aliased"`) || calls.Load() != 1 {
		t.Fatalf("alias result=%s err=%v calls=%d", result.Data, err, calls.Load())
	}

	_, err = gj.GraphQL(ctx, `mutation ($request: JSON!) { create_widget(call: $request) { ok } }`, json.RawMessage(`{"request":"not-an-object"}`), nil)
	if err == nil || !strings.Contains(err.Error(), "must be a JSON object") || calls.Load() != 1 {
		t.Fatalf("non-object variable err=%v calls=%d", err, calls.Load())
	}

	_, err = gj.GraphQL(ctx, `mutation { create_widget(call: "not-an-object") { ok } }`, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "must be variable or an object") || calls.Load() != 1 {
		t.Fatalf("non-object literal err=%v calls=%d", err, calls.Load())
	}

	_, err = gj.GraphQL(ctx, `mutation ($request: JSON!) {
  first: create_widget(call: $request) { ok }
  second: create_widget(call: $request) { ok }
}`, vars, nil)
	if err == nil || !strings.Contains(err.Error(), "exactly one root") || calls.Load() != 1 {
		t.Fatalf("multiple roots err=%v calls=%d", err, calls.Load())
	}

	_, err = gj.GraphQL(ctx, `mutation ($request: JSON!) {
  create_widget(call: $request) { ok }
  seed(insert: { id: 1 }) { id }
}`, vars, nil)
	if err == nil || calls.Load() != 1 {
		t.Fatalf("mixed SQL/API roots err=%v calls=%d", err, calls.Load())
	}
}

func TestOpenAPIMutationJSONNullSurvivesCompilerValueConversion(t *testing.T) {
	node, err := graph.ParseArgValue(`{"body":null}`, true)
	if err != nil {
		t.Fatal(err)
	}
	value, err := managedNodeToValue(node, nil)
	if err != nil {
		t.Fatal(err)
	}
	call, ok := value.(map[string]interface{})
	if !ok {
		t.Fatalf("call value = %T, want object", value)
	}
	body, exists := call["body"]
	if !exists || body != nil {
		t.Fatalf("body = %#v (exists=%v), want explicit null", body, exists)
	}
}

func introspectionType(types []FullType, name string) FullType {
	for _, item := range types {
		if item.Name == name {
			return item
		}
	}
	return FullType{}
}

func introspectionField(fields []FieldObject, name string) *FieldObject {
	for i := range fields {
		if fields[i].Name == name {
			return &fields[i]
		}
	}
	return nil
}

func newOpenAPIMutationTestEngine(t *testing.T, baseURL string, readOnly bool) *GraphJin {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE seed (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	conf := &Config{
		DBType:           "sqlite",
		DisableAllowList: true,
		Roles:            []Role{{Name: "api_executor"}},
		Sources: []SourceConfig{
			{Name: "main", Kind: sourcecap.KindDatabase, Type: "sqlite", Default: true, Access: SourceAccessConfig{Read: AccessModeAuthenticated}},
			{
				Name: "upstream", Kind: sourcecap.KindAPI, ReadOnly: readOnly,
				SpecsDir:     filepath.Join("openapi", "testdata"),
				Capabilities: map[string]bool{sourcecap.KeyAPIRead: true, sourcecap.KeyAPIWrite: true, sourcecap.KeyAPIDelete: false},
				Access:       SourceAccessConfig{Read: AccessModeAuthenticated, Write: AccessModeAuthenticated, Delete: AccessModeBlocked},
				Specs: map[string]openapi.SpecConfig{
					"generic_mutations": {
						BaseURL: baseURL,
						Auth:    openapi.AuthConfig{Scheme: "bearer", Token: "fixture-token"},
						Operations: map[string]openapi.OperationOverride{
							"createWidget": {ExposeAs: "create_widget", ExposeMutation: true, AllowedRoles: []string{"api_executor"}},
						},
					},
				},
			},
		},
	}
	gj, err := NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gj.Close)
	return gj
}
