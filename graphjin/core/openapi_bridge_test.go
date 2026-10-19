package core

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	_log "log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"github.com/dosco/graphjin/core/v3/openapi"
	"github.com/getkin/kin-openapi/openapi3"
)

// silentLogger discards log output so tests don't pollute stdout.
// We don't need to assert on log content for these tests; the warnings
// are exercised via the LoadResult.Warnings slice instead.
func silentLogger(t *testing.T) *_log.Logger {
	t.Helper()
	return _log.New(io.Discard, "", 0)
}

func TestSynthesiseResolvers(t *testing.T) {
	reg := &openapi.Registry{
		Specs: []*openapi.Spec{{
			Key: "is",
			Operations: []openapi.OpDescriptor{
				{
					SpecKey:     "is",
					OperationID: "getUserById",
					Mode:        openapi.OpModeRowJoin,
					ExposeAs:    "is_profile",
					PathParams:  []openapi.ParamSpec{{Name: "userId", In: openapi.ParamInPath}},
					Join: &openapi.JoinConfig{
						ParentTable:  "users",
						ParentColumn: "email",
						Param:        "userId",
					},
				},
				{
					OperationID: "exportFile",
					Mode:        openapi.OpModeSkipped,
				},
				{
					SpecKey:     "is",
					OperationID: "listAuditLogs",
					Mode:        openapi.OpModeList,
					ExposeAs:    "is_audit_logs",
				},
				{
					SpecKey:     "is",
					OperationID: "getOrgById",
					Mode:        openapi.OpModeSingleByID,
					ExposeAs:    "is_org",
				},
			},
		}},
	}

	got := synthesiseResolvers(reg)
	if len(got) != 3 {
		t.Fatalf("synthesised %d configs, want 3: %+v", len(got), got)
	}

	rj := got[0]
	if rj.Name != "is_profile" || rj.Table != "users" || rj.Column != "email" || rj.Props["path_param"] != "userId" {
		t.Errorf("row-join config wrong: %+v", rj)
	}

	for _, tl := range got[1:] {
		if tl.Table != "" || tl.Column != "" {
			t.Errorf("top-level config should have empty Table/Column: %+v", tl)
		}
		if tl.Props["spec_key"] != "is" {
			t.Errorf("top-level missing spec_key: %+v", tl.Props)
		}
	}
}

func TestOpenAPISpecFingerprintIncludesTimeout(t *testing.T) {
	first := &openapi.Spec{Key: "api", Timeout: time.Second}
	second := &openapi.Spec{Key: "api", Timeout: 2 * time.Second}
	if openAPISpecFingerprint(first) == openAPISpecFingerprint(second) {
		t.Fatal("per-spec timeout must participate in resolver cache fingerprinting")
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

func newTestEngineWithTables(t *testing.T, conf *Config, primarySchema string, names ...string) (*graphjinEngine, *bytes.Buffer) {
	t.Helper()
	tables := make([]sdata.DBTable, 0, len(names))
	for _, n := range names {
		tables = append(tables, sdata.DBTable{Schema: primarySchema, Name: n, Type: "table"})
	}
	var logBuf bytes.Buffer
	gj := &graphjinEngine{
		conf:      conf,
		log:       _log.New(&logBuf, "", 0),
		defaultDB: "primary",
		databases: map[string]*dbContext{
			"primary": {name: "primary", dbinfo: &sdata.DBInfo{Schema: primarySchema, Tables: tables}},
		},
	}
	return gj, &logBuf
}

func TestResolverDBInfoParsesSourceQualifiedTable(t *testing.T) {
	fallback := &sdata.DBInfo{Schema: "main"}
	mesInfo := &sdata.DBInfo{Schema: "public"}
	gj := &graphjinEngine{
		databases: map[string]*dbContext{
			"mes": {name: "mes", dbinfo: mesInfo},
		},
	}
	rc := ResolverConfig{Schema: "main", Table: "mes:public.bom_items"}
	got, err := gj.resolverDBInfo(&rc, fallback)
	if err != nil {
		t.Fatalf("resolverDBInfo: %v", err)
	}
	if got != mesInfo {
		t.Fatalf("expected mes DBInfo, got %#v", got)
	}
	if rc.Schema != "public" || rc.Table != "bom_items" {
		t.Fatalf("unexpected parsed resolver table: schema=%q table=%q", rc.Schema, rc.Table)
	}
}

func TestCollisionWithRealTableErrors(t *testing.T) {
	gj, _ := newTestEngineWithTables(t, &Config{}, "public", "users", "orders")
	synth := []ResolverConfig{
		{
			Name:  "users", // collides with real public.users
			Type:  "openapi",
			Table: "tenants", Column: "id",
			Props: ResolverProps{"spec_key": "is", "operation_id": "getUserById"},
		},
	}
	err := gj.validateOpenAPINoCollisions(synth)
	if err == nil {
		t.Fatal("expected error for collision with real table, got nil")
	}
	if !strings.Contains(err.Error(), `"users"`) {
		t.Errorf("error should name the colliding table; got: %v", err)
	}
	if !strings.Contains(err.Error(), "expose_as") {
		t.Errorf("error should mention 'expose_as' as the fix; got: %v", err)
	}
	if !strings.Contains(err.Error(), "is/getUserById") {
		t.Errorf("error should identify the offending operation; got: %v", err)
	}
}

func TestCollisionAcrossSpecsErrors(t *testing.T) {
	gj, _ := newTestEngineWithTables(t, &Config{}, "public", "users")
	synth := []ResolverConfig{
		{
			Name: "audit", Type: "openapi",
			Table: "users", Column: "id",
			Props: ResolverProps{"spec_key": "is", "operation_id": "getAudit"},
		},
		{
			Name:  "audit", // same expose_as as above
			Type:  "openapi",
			Table: "orders", Column: "id",
			Props: ResolverProps{"spec_key": "stripe", "operation_id": "getAuditEvent"},
		},
	}
	err := gj.validateOpenAPINoCollisions(synth)
	if err == nil {
		t.Fatal("expected error for cross-spec ExposeAs collision, got nil")
	}
	if !strings.Contains(err.Error(), "is/getAudit") || !strings.Contains(err.Error(), "stripe/getAuditEvent") {
		t.Errorf("error should identify both colliding operations; got: %v", err)
	}
}

func TestCollisionWithAliasWarnsButPasses(t *testing.T) {
	conf := &Config{
		Tables: []Table{
			{Name: "customers", Table: "users"}, // alias customers → users
		},
	}
	gj, logBuf := newTestEngineWithTables(t, conf, "public", "users")
	synth := []ResolverConfig{
		{
			Name:  "customers", // collides with the alias, not a real table
			Type:  "openapi",
			Table: "users", Column: "id",
			Props: ResolverProps{"spec_key": "crm", "operation_id": "getCustomer"},
		},
	}
	if err := gj.validateOpenAPINoCollisions(synth); err != nil {
		t.Fatalf("alias collision should warn, not error; got: %v", err)
	}
	if !strings.Contains(logBuf.String(), "alias") {
		t.Errorf("expected log warning mentioning alias; got log: %q", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "crm/getCustomer") {
		t.Errorf("warning should identify the operation; got log: %q", logBuf.String())
	}
}

func TestCollisionWithExistingResolverWarns(t *testing.T) {
	conf := &Config{
		Resolvers: []ResolverConfig{
			{Name: "ledger", Type: "remote_api", Table: "accounts", Column: "id"},
		},
	}
	gj, logBuf := newTestEngineWithTables(t, conf, "public", "accounts")
	synth := []ResolverConfig{
		{
			Name:  "ledger", // matches existing resolver name
			Type:  "openapi",
			Table: "accounts", Column: "id",
			Props: ResolverProps{"spec_key": "fin", "operation_id": "getLedger"},
		},
	}
	if err := gj.validateOpenAPINoCollisions(synth); err != nil {
		t.Fatalf("existing-resolver collision should warn, not error; got: %v", err)
	}
	if !strings.Contains(logBuf.String(), "fin/getLedger") {
		t.Errorf("expected warning identifying the operation; got log: %q", logBuf.String())
	}
}

func TestNoCollisionsHappyPath(t *testing.T) {
	gj, logBuf := newTestEngineWithTables(t, &Config{}, "public", "users", "orders")
	synth := []ResolverConfig{
		{
			Name: "is_get_user_by_id", Type: "openapi",
			Table: "users", Column: "email",
			Props: ResolverProps{"spec_key": "is", "operation_id": "getUserById"},
		},
		{
			Name: "stripe_get_payment_by_id", Type: "openapi",
			Table: "orders", Column: "stripe_id",
			Props: ResolverProps{"spec_key": "stripe", "operation_id": "getPaymentById"},
		},
	}
	if err := gj.validateOpenAPINoCollisions(synth); err != nil {
		t.Fatalf("happy path should not error: %v", err)
	}
	if logBuf.Len() != 0 {
		t.Errorf("happy path should produce no log output; got: %q", logBuf.String())
	}
}

func TestCollisionCheckHandlesMissingDBInfo(t *testing.T) {
	gj := &graphjinEngine{
		conf:      &Config{},
		log:       silentLogger(t),
		defaultDB: "primary",
		databases: map[string]*dbContext{"primary": {name: "primary"}}, // no dbinfo
	}
	synth := []ResolverConfig{
		{Name: "x", Props: ResolverProps{"spec_key": "a", "operation_id": "op1"}},
		{Name: "x", Props: ResolverProps{"spec_key": "b", "operation_id": "op2"}}, // dup
	}
	err := gj.validateOpenAPINoCollisions(synth)
	if err == nil {
		t.Fatal("cross-spec collision should still be detected without dbinfo")
	}
	if !strings.Contains(err.Error(), "a/op1") || !strings.Contains(err.Error(), "b/op2") {
		t.Errorf("error should identify both ops; got: %v", err)
	}
}

func TestOpenAPIMutationRootsParticipateInCollisionChecks(t *testing.T) {
	reg := &openapi.Registry{Specs: []*openapi.Spec{{
		Key: "contract",
		Operations: []openapi.OpDescriptor{
			{SourceName: "api", SpecKey: "contract", OperationID: "list", ExposeAs: "shared_root", Mode: openapi.OpModeList},
			{SourceName: "api", SpecKey: "contract", OperationID: "create", ExposeAs: "shared_root", Mode: openapi.OpModeMutation},
		},
	}}}
	gj := &graphjinEngine{conf: &Config{}, log: silentLogger(t)}
	allRoots := append(synthesiseResolvers(reg), synthesiseMutationCollisionResolvers(reg)...)
	if len(allRoots) != 2 {
		t.Fatalf("collision roots = %+v", allRoots)
	}
	if err := gj.validateOpenAPINoCollisions(allRoots); err == nil || !strings.Contains(err.Error(), "shared_root") {
		t.Fatalf("collision error = %v", err)
	}
}

func TestMutationOnlyRegistryStillProducesRegistrationRoot(t *testing.T) {
	reg := &openapi.Registry{Specs: []*openapi.Spec{{
		Key: "contract",
		Operations: []openapi.OpDescriptor{{
			SourceName: "api", SpecKey: "contract", OperationID: "create", ExposeAs: "create_item", Mode: openapi.OpModeMutation,
		}},
	}}}
	if got := synthesiseResolvers(reg); len(got) != 0 {
		t.Fatalf("mutation must not use resolver dispatch: %+v", got)
	}
	got := synthesiseMutationCollisionResolvers(reg)
	if len(got) != 1 || got[0].Name != "create_item" || got[0].Props["source_name"] != "api" {
		t.Fatalf("mutation registration roots = %+v", got)
	}
}

type openAPICollisionManagedMutationHandler struct{}

func (openAPICollisionManagedMutationHandler) ManagedMutationTables() []string {
	return []string{"gj_task"}
}

func (openAPICollisionManagedMutationHandler) ExecuteManagedMutation(context.Context, ManagedMutationRequest) (json.RawMessage, error) {
	return nil, nil
}

func TestOpenAPIRootCollisionWithManagedRootErrorsCaseInsensitively(t *testing.T) {
	gj, _ := newTestEngineWithTables(t, &Config{}, "public")
	gj.managedMutationHandlers = map[string]ManagedMutationHandler{
		gj.defaultDB: openAPICollisionManagedMutationHandler{},
	}
	synth := []ResolverConfig{{
		Name: "GJ_TASK",
		Type: "openapi_mutation",
		Props: ResolverProps{
			"spec_key":     "tasks",
			"operation_id": "createTask",
		},
	}}

	err := gj.validateOpenAPINoCollisions(synth)
	if err == nil {
		t.Fatal("expected managed-root collision error")
	}
	if !strings.Contains(err.Error(), `managed root "gj_task"`) || !strings.Contains(err.Error(), "tasks/createTask") {
		t.Fatalf("managed-root collision error = %v", err)
	}
}

// newTestEngineWithRealDBInfo builds an engine over a fully constructed DBInfo.
// The collision-check helper above hand-rolls its DBInfo without the lookup
// maps, which is fine for those tests but panics the moment a table with
// columns is registered.
func newTestEngineWithRealDBInfo(t *testing.T, schema string, tables ...string) *graphjinEngine {
	t.Helper()
	cols := make([]sdata.DBColumn, 0, len(tables))
	for i, name := range tables {
		cols = append(cols, sdata.DBColumn{
			ID: int32(i), Schema: schema, Table: name, Name: "id", Type: "bigint", PrimaryKey: true,
		})
	}
	return &graphjinEngine{
		conf:      &Config{},
		log:       silentLogger(t),
		defaultDB: "primary",
		rmap:      make(map[string]resItem),
		databases: map[string]*dbContext{
			"primary": {name: "primary", dbinfo: sdata.NewDBInfo("postgres", 150000, schema, "test", cols, nil, nil)},
		},
	}
}

// objectResponseSchema builds the kind of response schema a real spec declares:
// an object with named properties, which is what lets GraphJin know the table's
// column surface in the first place.
func objectResponseSchema(names ...string) *openapi3.SchemaRef {
	props := openapi3.Schemas{}
	for _, name := range names {
		props[name] = openapi3.NewSchemaRef("", openapi3.NewStringSchema())
	}
	schema := openapi3.NewObjectSchema()
	schema.Properties = props
	return openapi3.NewSchemaRef("", schema)
}

// TestOpenAPITablesCloseTheirColumnSurfaceWhenTheSpecDeclaresIt pins the
// registration half of a benchmark failure. A join child selecting fields that
// do not exist used to compile, ride the remote pass-through, and come back as
// an empty object with no error — the episode that asked account_health for
// open_risks and health_color answered that the risks were undefined.
//
// validateRemoteField has always had the right error; it only fires on tables
// that declare a closed surface, and the two paths that register spec-described
// tables never did. Leniency is still correct where the shape is genuinely
// unknown, so it survives exactly there.
func TestOpenAPITablesCloseTheirColumnSurfaceWhenTheSpecDeclaresIt(t *testing.T) {
	gj := newTestEngineWithRealDBInfo(t, "public", "users")
	reg := &openapi.Registry{Specs: []*openapi.Spec{{
		Key: "health",
		Operations: []openapi.OpDescriptor{
			{
				SpecKey: "health", OperationID: "listHealth", Mode: openapi.OpModeList,
				ExposeAs:       "described_rows",
				ResponseSchema: objectResponseSchema("health", "open_risk_count"),
			},
			{
				// A response the spec never described: GraphJin cannot know
				// which selections are wrong, so it must not guess.
				SpecKey: "health", OperationID: "listOpaque", Mode: openapi.OpModeList,
				ExposeAs: "opaque_rows",
			},
		},
	}}}
	if err := gj.preRegisterOpenAPITables(reg); err != nil {
		t.Fatal(err)
	}
	dbinfo := gj.primaryDB().dbinfo
	described, err := dbinfo.GetTable("public", "described_rows")
	if err != nil {
		t.Fatal(err)
	}
	if !described.StrictColumns {
		t.Error("a spec-described response must close its column surface")
	}
	opaque, err := dbinfo.GetTable("public", "opaque_rows")
	if err != nil {
		t.Fatal(err)
	}
	if opaque.StrictColumns {
		t.Error("an undescribed response must keep the lenient pass-through")
	}
}

// The row-join path registers its tables in initRemote rather than the
// pre-register sweep, and it is the one the demo's account_health uses — so it
// is the one the recorded failure actually went through.
func TestRowJoinTableClosesItsColumnSurfaceWhenColumnsAreKnown(t *testing.T) {
	gj := newTestEngineWithRealDBInfo(t, "public", "users")
	rtmap := map[string]ResolverFn{
		"openapi": func(ResolverProps) (Resolver, error) { return &stubRemoteResolver{}, nil },
	}
	dbinfo := gj.primaryDB().dbinfo
	described := ResolverConfig{
		Name: "described_join", Type: "openapi", Schema: "public", Table: "users", Column: "id",
		remoteColumns: []sdata.DBColumn{{Name: "health"}, {Name: "open_risk_count"}},
	}
	if err := gj.initRemote(described, rtmap, dbinfo); err != nil {
		t.Fatal(err)
	}
	// A hand-written resolver carries no column list, so GraphJin never learned
	// its shape and the historical pass-through is the honest default.
	opaque := ResolverConfig{
		Name: "opaque_join", Type: "openapi", Schema: "public", Table: "users", Column: "id",
	}
	if err := gj.initRemote(opaque, rtmap, dbinfo); err != nil {
		t.Fatal(err)
	}
	got, err := dbinfo.GetTable("public", "described_join")
	if err != nil {
		t.Fatal(err)
	}
	if !got.StrictColumns {
		t.Error("a join whose response columns are known must reject unknown selections")
	}
	got, err = dbinfo.GetTable("public", "opaque_join")
	if err != nil {
		t.Fatal(err)
	}
	if got.StrictColumns {
		t.Error("a join with no known columns must stay lenient")
	}
}

type stubRemoteResolver struct{}

func (stubRemoteResolver) Resolve(context.Context, ResolverReq) ([]byte, error) {
	return []byte(`{}`), nil
}
