package tests_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

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
