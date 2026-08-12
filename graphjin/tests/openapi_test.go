package tests_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/openapi"
)

// Example_queryWithOpenAPIJoin exercises the full OpenAPI integration
// against a real GraphJin engine, end-to-end:
//
//   - A temp OpenAPI 3 spec is dropped in a tempdir.
//   - The mock upstream verifies that bearer auth was applied — if the
//     header is missing the server panics, failing the test loudly.
//   - A GraphJin Config carries an openapi source with credentials and join wiring.
//   - The resulting GraphQL query joins the upstream's response onto the
//     parent users table via the synthesised "openapi" resolver type.
//
// This test runs against every dialect the harness is configured for
// (postgres, mysql, mariadb, sqlite, oracle, mssql, snowflake, mongodb)
// because the dialect-specific work — parent SQL emission with the
// remote-marker placeholder column — flows through the same code path
// as remote_api, which already has dialect coverage. The OpenAPI sub-
// package itself is dialect-independent, so one engine-level test gives
// us proof the wiring works without N copies for N dialects.
func Example_queryWithOpenAPIJoin() {
	// Spin up a mock upstream that mimics a single-record lookup
	// (GET /payments/{paymentId} returning {data: {desc, amount}}). The
	// shape mirrors what Salesforce MC Personalization, Stripe-style
	// services, and most CRUD-y REST APIs publish, so the test exercises
	// the realistic case rather than a contrived one.
	mux := http.NewServeMux()
	mux.HandleFunc("/payments/", func(w http.ResponseWriter, r *http.Request) {
		// Auth check: the only way this header is set is if the auth
		// provider was constructed and wired into the resolver. A
		// regression that bypasses auth surfaces here as a panic.
		if got := r.Header.Get("Authorization"); got != "Bearer test-tok" {
			panic(fmt.Sprintf("openapi join: missing/wrong auth: %q", got))
		}
		id := r.URL.Path[len("/payments/"):]
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":{"desc":"Payment for %s","amount":100}}`, id) //nolint:errcheck
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// Drop a minimal OpenAPI spec into a tempdir. The loader scans this
	// directory at NewGraphJin time and classifies the single GET as a
	// row-join candidate (single trailing path param, JSON response).
	specsDir, err := os.MkdirTemp("", "graphjin-openapi-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(specsDir) //nolint:errcheck

	specYAML := fmt.Sprintf(`
openapi: 3.0.0
info: { title: Payments, version: '1.0' }
servers:
  - url: %s
paths:
  /payments/{paymentId}:
    get:
      operationId: getPaymentById
      parameters:
        - { name: paymentId, in: path, required: true, schema: { type: string } }
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
                      desc:   { type: string }
                      amount: { type: integer }
`, server.URL)

	specPath := filepath.Join(specsDir, "payments.yaml")
	if err := os.WriteFile(specPath, []byte(specYAML), 0o644); err != nil {
		panic(err)
	}

	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		DefaultLimit:     2,
		Sources: []core.SourceConfig{
			{Name: core.DefaultDBName, Kind: "database", Type: dbType, Default: true, Access: core.SourceAccessConfig{
				Read: core.AccessModeAuthenticated,
			}},
			{
				Name:     "upstream",
				Kind:     "api",
				SpecsDir: specsDir,
				Specs: map[string]openapi.SpecConfig{
					// Spec key matches the filename without extension. The
					// loader uses this to look up auth + joins.
					"payments": {
						Auth: openapi.AuthConfig{
							Scheme: "bearer",
							Token:  "test-tok",
						},
						Joins: map[string]openapi.JoinConfig{
							// Wires GET /payments/{paymentId} onto users.stripe_id
							// - the same join shape Example_queryWithRemoteAPIJoin
							// exercises, but spec-driven instead of URL-templated.
							"getPaymentById": {
								ParentTable:  "users",
								ParentColumn: "stripe_id",
								Param:        "paymentId",
								ExposeAs:     "payment",
							},
						},
					},
				},
			},
		},
	})

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer gj.Close()

	gql := `query {
		users(order_by: { id: asc }) {
			email
			payment {
				desc
				amount
			}
		}
	}`

	res, err := gj.GraphQL(sourceModeIntegrationUserContext(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"users":[{"email":"user1@test.com","payment":{"amount":100,"desc":"Payment for payment_id_1001"}},{"email":"user2@test.com","payment":{"amount":100,"desc":"Payment for payment_id_1002"}}]}
}

// TestOpenAPIJoinTablePublishesItsColumns pins the shape a row-join table exposes
// to metadata, and through it to the catalog.
//
// Column synthesis ran for top-level and single-by-id operations but not for row
// joins, and initRemote registered the join table with nil columns. The table was
// queryable, so nothing failed loudly — the catalog simply described it as "0
// columns" with an example selecting an id field the spec never declared. Benchmark
// generation 2028.1 measured the result across three runs of the same suite: 0 of
// 24 cross-source tasks passed, every episode inventing field names like
// health_score and open_risks_count for a table whose real fields GraphJin had
// parsed from the spec at boot.
func TestOpenAPIJoinTablePublishesItsColumns(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/payments/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":{"desc":"Payment","amount":100}}`) //nolint:errcheck
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	specsDir, err := os.MkdirTemp("", "graphjin-openapi-columns-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(specsDir) //nolint:errcheck

	specYAML := fmt.Sprintf(`
openapi: 3.0.0
info: { title: Payments, version: '1.0' }
servers:
  - url: %s
paths:
  /payments/{paymentId}:
    get:
      operationId: getPaymentById
      parameters:
        - { name: paymentId, in: path, required: true, schema: { type: string } }
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
                      desc:   { type: string }
                      amount: { type: integer }
`, server.URL)
	if err := os.WriteFile(filepath.Join(specsDir, "payments.yaml"), []byte(specYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		DefaultLimit:     2,
		Sources: []core.SourceConfig{
			{Name: core.DefaultDBName, Kind: "database", Type: dbType, Default: true, Access: core.SourceAccessConfig{
				Read: core.AccessModeAuthenticated,
			}},
			{
				Name:     "upstream",
				Kind:     "api",
				SpecsDir: specsDir,
				Specs: map[string]openapi.SpecConfig{
					"payments": {
						Auth: openapi.AuthConfig{Scheme: "bearer", Token: "test-tok"},
						Joins: map[string]openapi.JoinConfig{
							"getPaymentById": {
								ParentTable:  "users",
								ParentColumn: "stripe_id",
								Param:        "paymentId",
								ExposeAs:     "payment",
							},
						},
					},
				},
			},
		},
	})

	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close() //nolint:errcheck

	md, err := gj.MetadataSnapshot()
	if err != nil {
		t.Fatal(err)
	}

	var found *core.MetadataTable
	for i := range md.Tables {
		if md.Tables[i].TableName == "payment" {
			found = &md.Tables[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("join table 'payment' missing from metadata: %+v", md.Tables)
	}
	if found.ColumnCount == 0 {
		t.Fatal("join table reports 0 columns; the catalog can only publish a placeholder and consumers must guess field names")
	}

	got := map[string]string{}
	for _, c := range md.Columns {
		if c.TableName == "payment" {
			got[c.ColumnName] = c.Type
		}
	}
	// The response shape is behind result_path "data"; both leaves must surface.
	for _, name := range []string{"desc", "amount"} {
		if _, ok := got[name]; !ok {
			t.Errorf("column %q missing from the join table: got %v", name, got)
		}
	}
	if found.ColumnCount != len(got) {
		t.Errorf("ColumnCount = %d but %d columns published: %v", found.ColumnCount, len(got), got)
	}

	// The join edge has to be published too. Without it the catalog describes the
	// table as standalone, and a caller reasonably queries it top-level with an
	// invented filter — which is what every cross-source episode did — when the only
	// way to reach it is nested under its parent.
	var edge *core.MetadataRelationship
	for i := range md.Relationships {
		if md.Relationships[i].FromTableName == "payment" && md.Relationships[i].ToTableName == "users" {
			edge = &md.Relationships[i]
			break
		}
	}
	if edge == nil {
		t.Fatalf("no payment -> users relationship published; relationships = %+v", md.Relationships)
	}
	if edge.ToColumnName != "stripe_id" {
		t.Errorf("join edge should land on the configured parent column, got %q", edge.ToColumnName)
	}
	if edge.Source != "remote_join" {
		t.Errorf("join edge source = %q, want remote_join", edge.Source)
	}
	// The internal key column must stay off the published column list.
	for name := range got {
		if strings.HasPrefix(name, "__") {
			t.Errorf("internal join key %q must not be published as a column", name)
		}
	}
}
