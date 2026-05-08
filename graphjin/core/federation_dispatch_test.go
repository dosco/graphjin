package core

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// fedTestEngine builds a graphjinEngine with the smallest scaffolding
// handleFederationQuery needs: a primary db with a Schema-set DBInfo,
// a couple of synthetic tables so the SDL has entities to emit, and
// federation enabled.
func fedTestEngine(t *testing.T) *graphjinEngine {
	t.Helper()
	di := sdata.NewDBInfo("postgres", 0, "public", "", []sdata.DBColumn{
		{Schema: "public", Table: "users", Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
		{Schema: "public", Table: "users", Name: "email", Type: "text"},
		{Schema: "public", Table: "products", Name: "id", Type: "bigint", PrimaryKey: true, NotNull: true},
		{Schema: "public", Table: "products", Name: "name", Type: "text"},
	}, nil, nil)
	schema, err := sdata.NewDBSchema(di, nil)
	if err != nil {
		t.Fatal(err)
	}
	gj := &graphjinEngine{
		conf: &Config{Federation: FederationConfig{Enabled: true}},
		databases: map[string]*dbContext{
			"": {dbinfo: di, schema: schema},
		},
		defaultDB: "",
	}
	return gj
}

// TestHandleFederationQuery_Service verifies the runtime path that
// makes GraphJin a valid Apollo subgraph: a `_service { sdl }` query
// must be intercepted before the regular compile pipeline and answered
// with the federation-flavoured SDL wrapped in the expected JSON shape.
func TestHandleFederationQuery_Service(t *testing.T) {
	gj := fedTestEngine(t)

	handled, data, err := gj.handleFederationQuery(GraphqlReq{
		query: []byte(`query { _service { sdl } }`),
	})
	if !handled {
		t.Fatalf("expected handled=true for _service query")
	}
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	var resp struct {
		Service struct {
			SDL string `json:"sdl"`
		} `json:"_service"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("response is not the _service envelope: %v\n%s", err, data)
	}
	if resp.Service.SDL == "" {
		t.Fatalf("expected non-empty SDL, got %s", data)
	}
	for _, want := range []string{
		"@link(url:",
		"@key(fields:",
		"_Service",
		"_entities",
	} {
		if !strings.Contains(resp.Service.SDL, want) {
			t.Errorf("SDL missing %q\n----\n%s\n----", want, resp.Service.SDL)
		}
	}
}

// TestHandleFederationQuery_Entities verifies that `_entities` queries
// return the documented "not yet implemented" error rather than silently
// dropping the request — so a gateway operator sees the gap clearly
// instead of a confusing empty response.
func TestHandleFederationQuery_Entities(t *testing.T) {
	gj := fedTestEngine(t)

	handled, data, err := gj.handleFederationQuery(GraphqlReq{
		query: []byte(`query Q($r: [_Any!]!) { _entities(representations: $r) { __typename } }`),
	})
	if !handled {
		t.Fatalf("expected handled=true for _entities query")
	}
	if !errors.Is(err, errFederationEntitiesNotImplemented) {
		t.Errorf("expected errFederationEntitiesNotImplemented, got %v", err)
	}
	if data != nil {
		t.Errorf("expected nil data for unimplemented path, got %s", data)
	}
}

// TestHandleFederationQuery_PassthroughForRegularQuery confirms that
// non-federation queries flow through the normal compile path
// unchanged. A false positive here would break every other query.
func TestHandleFederationQuery_PassthroughForRegularQuery(t *testing.T) {
	gj := fedTestEngine(t)

	cases := []string{
		`query { users { id } }`,
		`query { _service_entries { id } }`,           // real field happens to contain `_service` substring
		`query { my_service }`,                        // non-token match
		`{ entities }`,                                // bare `entities` shouldn't trigger `_entities`
		`mutation { upsertUser(input: $u) { id } }`,
	}
	for _, q := range cases {
		handled, _, err := gj.handleFederationQuery(GraphqlReq{query: []byte(q)})
		if handled {
			t.Errorf("query %q should not be handled by federation path", q)
		}
		if err != nil {
			t.Errorf("query %q: unexpected error %v", q, err)
		}
	}
}

// TestFederationSDL_CachedAcrossCalls ensures the lazy `sync.Once`
// cache works — a second invocation reuses the first build rather than
// regenerating SDL on every gateway poll.
func TestFederationSDL_CachedAcrossCalls(t *testing.T) {
	gj := fedTestEngine(t)
	first, err := gj.getFederationSDL()
	if err != nil {
		t.Fatal(err)
	}
	second, err := gj.getFederationSDL()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("SDL should be byte-identical across calls; got differences")
	}
}
