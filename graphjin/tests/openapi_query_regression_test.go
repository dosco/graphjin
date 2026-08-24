package tests_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/openapi"
	"github.com/dosco/graphjin/serv/v3"
)

func openAPIQueryRegressionConfig(t *testing.T, databaseType, upstreamURL string, timeout time.Duration) *core.Config {
	t.Helper()
	specsDir := t.TempDir()
	spec := fmt.Sprintf(`
openapi: "3.0.0"
info: { title: Query Regression, version: "1.0.0" }
servers: [{ url: %s }]
paths:
  /alerts:
    get:
      operationId: listAlerts
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    id: { type: string }
                    severity: { type: string }
                    warningCount: { type: integer }
  /projects/{id}:
    get:
      operationId: getProject
      parameters:
        - { name: id, in: path, required: true, schema: { type: string } }
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  id: { type: string }
                  warningCount: { type: integer }
`, upstreamURL)
	if err := os.WriteFile(filepath.Join(specsDir, "regression.yaml"), []byte(spec), 0o600); err != nil {
		t.Fatal(err)
	}
	return newConfig(&core.Config{
		Mode:                 "dev",
		DBType:               databaseType,
		DisableAllowList:     true,
		DBSchemaPollDuration: -1,
		DefaultLimit:         10,
		Sources: []core.SourceConfig{
			{Name: core.DefaultDBName, Kind: "database", Type: databaseType, Default: true, Access: core.SourceAccessConfig{Read: core.AccessModePublic}},
			{
				Name: "upstream", Kind: "api", SpecsDir: specsDir,
				Access: core.SourceAccessConfig{Read: core.AccessModePublic},
				Specs: map[string]openapi.SpecConfig{
					"regression": {
						Timeout: timeout,
						Joins: map[string]openapi.JoinConfig{
							"getProject": {ParentTable: "users", ParentColumn: "id", Param: "id", ExposeAs: "live"},
						},
						Operations: map[string]openapi.OperationOverride{
							"listAlerts": {ExposeAs: "alerts"},
						},
					},
				},
			},
		},
	})
}

func skipOpenAPIQueryRegressionForUnscopedFixture(t *testing.T, conf *core.Config) {
	t.Helper()
	// Some non-relational suites describe their shared fixture with unscoped
	// tables. GraphJin correctly rejects those tables once an API source is also
	// configured, before these OpenAPI response semantics can be exercised.
	for _, table := range conf.Tables {
		if strings.TrimSpace(table.Source) == "" {
			t.Skipf("%s: shared fixture declares table %q without a source, which cannot coexist with an api source", dbType, table.Name)
		}
	}
}

func TestOpenAPIWhereOrderAndPagingAreApplied(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/alerts" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[
			{"id":"a1","severity":"critical","warningCount":5},
			{"id":"a2","severity":"warning","warningCount":9},
			{"id":"a3","severity":"critical","warningCount":3},
			{"id":"a4","severity":"critical","warningCount":1}
		]`)
	}))
	defer upstream.Close()

	conf := openAPIQueryRegressionConfig(t, dbType, upstream.URL, time.Second)
	skipOpenAPIQueryRegressionForUnscopedFixture(t, conf)
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	vars := json.RawMessage(`{"severity":"critical","minimum":1}`)
	res, err := gj.GraphQL(sourceModeIntegrationUserContext(), `query FilterAlerts(
		$severity: String!, $minimum: Int!
	) {
		alerts(
			where: {severity: {eq: $severity}, warningCount: {gt: $minimum}}
			order_by: {warningCount: desc}
			offset: 1
			limit: 1
		) { id severity warningCount }
	}`, vars, nil)
	if err != nil || len(res.Errors) != 0 {
		t.Fatalf("filtered OpenAPI query: err=%v errors=%+v data=%s", err, res.Errors, res.Data)
	}
	if got, want := string(res.Data), `{"alerts":[{"id":"a3","severity":"critical","warningCount":3}]}`; got != want {
		t.Fatalf("filtered OpenAPI response = %s, want %s", got, want)
	}
}

func TestOpenAPIRowJoinWhereNullsNonMatchingObject(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/projects/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"project","warningCount":2}`)
	}))
	defer upstream.Close()

	conf := openAPIQueryRegressionConfig(t, dbType, upstream.URL, time.Second)
	skipOpenAPIQueryRegressionForUnscopedFixture(t, conf)
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	res, err := gj.GraphQL(sourceModeIntegrationUserContext(), `query {
		users(limit: 1, order_by: {id: asc}) {
			id
			live(where: {warningCount: {gt: 999}}) { id warningCount }
		}
	}`, nil, nil)
	if err != nil || len(res.Errors) != 0 {
		t.Fatalf("row-join filter: err=%v errors=%+v data=%s", err, res.Errors, res.Data)
	}
	var data struct {
		Users []struct {
			Live json.RawMessage `json:"live"`
		} `json:"users"`
	}
	if err := json.Unmarshal(res.Data, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Users) != 1 || string(data.Users[0].Live) != "null" {
		t.Fatalf("non-matching row join must be null: %s", res.Data)
	}
}

func TestOpenAPITimeoutReturnsHTTPPartialDataAndError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/alerts" {
			http.NotFound(w, r)
			return
		}
		<-r.Context().Done()
	}))
	defer upstream.Close()

	dbPath := filepath.Join(t.TempDir(), "app.sqlite")
	appDB, err := sql.Open("sqlite3_regexp", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appDB.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT); INSERT INTO users VALUES (1, 'healthy@example.com')`); err != nil {
		_ = appDB.Close()
		t.Fatal(err)
	}

	conf := openAPIQueryRegressionConfig(t, "sqlite", upstream.URL, 40*time.Millisecond)
	gjs, err := serv.NewGraphJinService(&serv.Config{
		Core: *conf,
		Serv: serv.Serv{ConfigPath: t.TempDir(), MCP: serv.MCPConfig{Disable: true}},
	}, serv.OptionSetDB(appDB))
	if err != nil {
		_ = appDB.Close()
		t.Fatal(err)
	}
	defer gjs.Close()

	payload, _ := json.Marshal(map[string]any{"query": `query {
		users(limit: 1, order_by: {id: asc}) { id email }
		alerts { id severity }
	}`})
	server := httptest.NewUnstartedServer(gjs.GraphQL(nil))
	server.Config.WriteTimeout = 100 * time.Millisecond
	server.Start()
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/graphql", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: time.Second}).Do(req)
	if err != nil {
		t.Fatalf("GraphQL HTTP request: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) == 0 {
		t.Fatal("timed-out OpenAPI root returned an empty HTTP body")
	}
	var response struct {
		Data struct {
			Users []struct {
				Email string `json:"email"`
			} `json:"users"`
			Alerts json.RawMessage `json:"alerts"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode GraphQL HTTP response: %v\n%s", err, body)
	}
	if len(response.Data.Users) != 1 || response.Data.Users[0].Email != "healthy@example.com" {
		t.Fatalf("healthy database root was not preserved: %s", body)
	}
	if string(response.Data.Alerts) != "null" {
		t.Fatalf("failed OpenAPI root must be null: %s", body)
	}
	if len(response.Errors) != 1 || !strings.Contains(response.Errors[0].Message, "timed out") {
		t.Fatalf("timeout must surface as a GraphQL error: %s", body)
	}
}
