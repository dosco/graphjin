package tests_test

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/openapi"
)

// Example_queryWithOpenAPITopLevel exercises a top-level OpenAPI virtual
// table — no parent DB row, args coming from the GraphQL field itself.
//
// Spec: GET /audit-logs?actorId=… returning {data: [{id, action}, ...]}.
// Query: { audit_logs(actorId: "u-7") { id action } }.
//
// The mock upstream verifies both bearer auth and that the actorId
// query parameter was forwarded. End-to-end this exercises the full
// pipeline: classifier → registry → bridge → pre-registered synthetic
// table → qcode RelRemote marking → gstate all-remote skip →
// remote_join parent-less branch → caller HTTP roundtrip → result-path
// strip → field filter → response.
func Example_queryWithOpenAPITopLevel() {
	mux := http.NewServeMux()
	mux.HandleFunc("/audit-logs", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer top-tok" {
			panic(fmt.Sprintf("openapi top-level: missing/wrong auth: %q", got))
		}
		actor := r.URL.Query().Get("actorId")
		if actor == "" {
			panic("openapi top-level: actorId query param missing — args→CallParams broken")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":[{"id":"e1","action":"login by %s"},{"id":"e2","action":"export by %s"}]}`, actor, actor) //nolint:errcheck
	})

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		panic(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	server := &http.Server{Handler: mux}
	go func() {
		log.Fatal(server.Serve(listener)) //nolint:gosec
	}()

	for i := 0; i < 100; i++ {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/audit-logs?actorId=ping", port))
		if err == nil {
			resp.Body.Close() //nolint:errcheck
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	specsDir, err := os.MkdirTemp("", "graphjin-openapi-toplevel-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(specsDir) //nolint:errcheck

	specYAML := fmt.Sprintf(`
openapi: 3.0.0
info: { title: Audit, version: '1.0' }
servers:
  - url: http://localhost:%d
paths:
  /audit-logs:
    get:
      operationId: listAuditLogs
      parameters:
        - { name: actorId, in: query, required: true, schema: { type: string } }
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
                    items:
                      type: object
                      properties:
                        id: { type: string }
                        action: { type: string }
`, port)

	specPath := filepath.Join(specsDir, "audit.yaml")
	if err := os.WriteFile(specPath, []byte(specYAML), 0o644); err != nil {
		panic(err)
	}

	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		DefaultLimit:     2,
		OpenAPISpecsDir:  specsDir,
		OpenAPI: map[string]openapi.SpecConfig{
			"audit": {
				Auth: openapi.AuthConfig{
					Scheme: "bearer",
					Token:  "top-tok",
				},
				Operations: map[string]openapi.OperationOverride{
					"listAuditLogs": {
						ExposeAs: "audit_logs",
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
		audit_logs(actorId: "u-7") {
			id
			action
		}
	}`

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"audit_logs":[{"action":"login by u-7","id":"e1"},{"action":"export by u-7","id":"e2"}]}
}

// Example_queryMixedRootDBPlusOpenAPI exercises a query with one
// real-table root (users) and one top-level OpenAPI remote root
// (audit_logs) in the same GraphQL document. The all-remote shortcut
// must not fire; psql skips the remote root; gstate injects a marker
// for it; execRemoteJoin then resolves the upstream call.
func Example_queryMixedRootDBPlusOpenAPI() {
	mux := http.NewServeMux()
	mux.HandleFunc("/audit-logs", func(w http.ResponseWriter, r *http.Request) {
		actor := r.URL.Query().Get("actorId")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":[{"id":"a1","action":"action by %s"}]}`, actor) //nolint:errcheck
	})
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		panic(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	server := &http.Server{Handler: mux}
	go func() { log.Fatal(server.Serve(listener)) }() //nolint:gosec
	for i := 0; i < 100; i++ {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/audit-logs?actorId=ping", port))
		if err == nil {
			resp.Body.Close() //nolint:errcheck
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	specsDir, err := os.MkdirTemp("", "graphjin-openapi-mixed-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(specsDir) //nolint:errcheck

	specYAML := fmt.Sprintf(`
openapi: 3.0.0
info: { title: Audit, version: '1.0' }
servers:
  - url: http://localhost:%d
paths:
  /audit-logs:
    get:
      operationId: listAuditLogs
      parameters:
        - { name: actorId, in: query, required: true, schema: { type: string } }
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
                    items:
                      type: object
                      properties:
                        id: { type: string }
                        action: { type: string }
`, port)
	if err := os.WriteFile(filepath.Join(specsDir, "audit.yaml"), []byte(specYAML), 0o644); err != nil {
		panic(err)
	}

	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		DefaultLimit:     2,
		OpenAPISpecsDir:  specsDir,
		OpenAPI: map[string]openapi.SpecConfig{
			"audit": {
				Operations: map[string]openapi.OperationOverride{
					"listAuditLogs": {ExposeAs: "audit_logs"},
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
		users(order_by: { id: asc }) { id email }
		audit_logs(actorId: "u-9") { id action }
	}`

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		fmt.Println(err)
	} else {
		printJSON(res.Data)
	}
	// Output: {"audit_logs":[{"action":"action by u-9","id":"a1"}],"users":[{"email":"user1@test.com","id":1},{"email":"user2@test.com","id":2}]}
}
