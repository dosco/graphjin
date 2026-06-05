package serv

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/core/v3"
	jwt "github.com/golang-jwt/jwt/v5"
	_ "modernc.org/sqlite"
)

const sourceModeHTTPJWTSecret = "source-mode-jwt-test-secret"

type graphQLHTTPResponse struct {
	Data   json.RawMessage   `json:"data"`
	Errors []json.RawMessage `json:"errors"`
}

func TestSourceModeHTTPJWTIdentityAndSystemRoots(t *testing.T) {
	handler := newSourceModeJWTHTTPTestHandler(t)
	memberToken := signSourceModeJWT(t, jwt.MapClaims{
		"sub":        "user_member",
		"roles":      []string{"member"},
		"account_id": "acct_1",
	})
	adminToken := signSourceModeJWT(t, jwt.MapClaims{
		"sub":        "admin_user",
		"roles":      []string{"admin"},
		"account_id": "acct_admin",
	})

	usersResp := postGraphQLJWT(t, handler, memberToken, `query {
		users(order_by: { id: asc }) { id name account_id }
	}`, map[string]any{"account_id": "acct_2"})
	assertNoGraphQLErrors(t, usersResp)

	var usersOut struct {
		Users []struct {
			ID        int    `json:"id"`
			Name      string `json:"name"`
			AccountID string `json:"account_id"`
		} `json:"users"`
	}
	if err := json.Unmarshal(usersResp.Data, &usersOut); err != nil {
		t.Fatalf("decode users response: %v\n%s", err, string(usersResp.Data))
	}
	if len(usersOut.Users) != 2 || usersOut.Users[0].ID != 1 || usersOut.Users[1].ID != 3 {
		t.Fatalf("expected account-scoped rows for acct_1, got %s", string(usersResp.Data))
	}
	for _, user := range usersOut.Users {
		if user.AccountID != "acct_1" {
			t.Fatalf("client-supplied account_id should not override JWT claim, got %+v", usersOut.Users)
		}
	}

	catalogResp := postGraphQLJWT(t, handler, memberToken, `query {
		gj_catalog(limit: 1) { id }
	}`, nil)
	assertNoGraphQLErrors(t, catalogResp)
	var catalogOut struct {
		Catalog []struct {
			ID string `json:"id"`
		} `json:"gj_catalog"`
	}
	if err := json.Unmarshal(catalogResp.Data, &catalogOut); err != nil {
		t.Fatalf("decode catalog response: %v\n%s", err, string(catalogResp.Data))
	}
	if len(catalogOut.Catalog) == 0 {
		t.Fatalf("expected authenticated user to read gj_catalog, got %s", string(catalogResp.Data))
	}

	securityDenied := postGraphQLJWT(t, handler, memberToken, `query {
		gj_security(id: "summary") { id }
	}`, nil)
	var deniedOut struct {
		Security *struct {
			ID string `json:"id"`
		} `json:"gj_security"`
	}
	if len(securityDenied.Data) != 0 {
		if err := json.Unmarshal(securityDenied.Data, &deniedOut); err != nil {
			t.Fatalf("decode denied security response: %v\n%s", err, string(securityDenied.Data))
		}
	}
	if deniedOut.Security != nil {
		t.Fatalf("expected non-admin JWT to be denied gj_security, got %s", string(securityDenied.Data))
	}

	securityAdmin := postGraphQLJWT(t, handler, adminToken, `query {
		gj_security(id: "summary") { id }
	}`, nil)
	assertNoGraphQLErrors(t, securityAdmin)
	var adminOut struct {
		Security *struct {
			ID string `json:"id"`
		} `json:"gj_security"`
	}
	if err := json.Unmarshal(securityAdmin.Data, &adminOut); err != nil {
		t.Fatalf("decode admin security response: %v\n%s", err, string(securityAdmin.Data))
	}
	if adminOut.Security == nil || adminOut.Security.ID != "summary" {
		t.Fatalf("expected admin JWT to read gj_security summary, got %s", string(securityAdmin.Data))
	}
}

func TestSourceModeHTTPRuntimeDenialEventsAreRedacted(t *testing.T) {
	handler := newSourceModeJWTHTTPTestHandler(t)
	memberToken := signSourceModeJWT(t, jwt.MapClaims{
		"sub":        "user_member_secret",
		"roles":      []string{"member"},
		"account_id": "acct_member_secret",
	})
	missingAccountToken := signSourceModeJWT(t, jwt.MapClaims{
		"sub":   "user_missing_secret",
		"roles": []string{"member"},
	})
	adminToken := signSourceModeJWT(t, jwt.MapClaims{
		"sub":        "admin_secret",
		"roles":      []string{"admin"},
		"account_id": "acct_admin_secret",
	})

	_ = postGraphQLJWT(t, handler, memberToken, `query {
		gj_security(id: "summary") { id }
	}`, nil)
	_ = postGraphQLJWT(t, handler, missingAccountToken, `query {
		users { id account_id }
	}`, map[string]any{"account_id": "acct_member_secret", "query": "query text should not be stored"})

	runtimeResp := postGraphQLJWT(t, handler, adminToken, `query {
		gj_runtime(where: { kind: { eq: "event" } }, order_by: { created_at: desc }, limit: 20) {
			kind
			phase
			status
			severity
			source
			error_code
			next_action
			details_json
		}
	}`, nil)
	assertNoGraphQLErrors(t, runtimeResp)

	var runtimeOut struct {
		Runtime []struct {
			Kind        string `json:"kind"`
			Phase       string `json:"phase"`
			Status      string `json:"status"`
			Severity    string `json:"severity"`
			Source      string `json:"source"`
			ErrorCode   string `json:"error_code"`
			NextAction  string `json:"next_action"`
			DetailsJSON string `json:"details_json"`
		} `json:"gj_runtime"`
	}
	if err := json.Unmarshal(runtimeResp.Data, &runtimeOut); err != nil {
		t.Fatalf("decode runtime response: %v\n%s", err, string(runtimeResp.Data))
	}

	var sawSecurityDenied, sawMissingIdentity bool
	for _, row := range runtimeOut.Runtime {
		rowText := row.Source + row.NextAction + row.DetailsJSON
		for _, forbidden := range []string{"user_member_secret", "acct_member_secret", "user_missing_secret", "admin_secret", "acct_admin_secret", "query text should not be stored"} {
			if strings.Contains(rowText, forbidden) {
				t.Fatalf("runtime event leaked %q in row %+v", forbidden, row)
			}
		}
		if row.Kind != runtimeKindEvent {
			continue
		}
		if row.Phase == "access" && row.Status == runtimeStatusFailed && row.Severity != "" && row.Source != "" &&
			(row.ErrorCode == "access_blocked" || row.ErrorCode == "access_unauthorized") &&
			strings.Contains(row.DetailsJSON, `"role":"member"`) &&
			strings.Contains(row.DetailsJSON, `"reason":"`+row.ErrorCode+`"`) &&
			strings.Contains(row.DetailsJSON, `"root":"gj_security"`) {
			if !strings.Contains(row.NextAction, `query_catalog(search: "graphjin root access gj_security gj_config admin")`) {
				t.Fatalf("expected root access recovery hint, got %+v", row)
			}
			sawSecurityDenied = true
		}
		if row.Phase == "access" && row.Status == runtimeStatusFailed &&
			row.ErrorCode == "identity_variable_missing" &&
			strings.Contains(row.DetailsJSON, `"identity_variable":"account_id"`) {
			sawMissingIdentity = true
		}
	}
	if !sawSecurityDenied || !sawMissingIdentity {
		t.Fatalf("expected redacted access denial events for gj_security and missing account_id, got %+v", runtimeOut.Runtime)
	}
}

func newSourceModeJWTHTTPTestHandler(t *testing.T) http.Handler {
	t.Helper()

	dbPath := createSourceModeHTTPDB(t)
	svc := newControlPlaneGraphQLTestServiceWithConfig(t, MCPConfig{}, dbPath, func(conf *Config) {
		conf.Core.Mode = modeAgentic
		conf.Core.Roles = append(conf.Core.Roles, core.Role{Name: "member"})
		conf.Serv.Auth = Auth{
			Type: "jwt",
			JWT:  JWTConfig{Secret: sourceModeHTTPJWTSecret},
		}
		conf.Serv.AuthFailBlock = true
	})

	ah, err := auth.NewAuthHandlerFunc(svc.conf.Auth)
	if err != nil {
		t.Fatalf("new auth handler: %v", err)
	}
	hs := &HttpService{}
	hs.Store(svc)
	return hs.GraphQL(ah)
}

func createSourceModeHTTPDB(t *testing.T) string {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "source-mode-http.sqlite3")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	for _, stmt := range []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, account_id TEXT)`,
		`INSERT INTO users (id, name, account_id) VALUES
			(1, 'Ada', 'acct_1'),
			(2, 'Bea', 'acct_2'),
			(3, 'Cal', 'acct_1')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}
	return dbPath
}

func signSourceModeJWT(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(sourceModeHTTPJWTSecret))
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return signed
}

func postGraphQLJWT(t *testing.T, handler http.Handler, token, query string, variables map[string]any) graphQLHTTPResponse {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		t.Fatalf("marshal graphql request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, routeGraphQL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GraphQL HTTP status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var resp graphQLHTTPResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode GraphQL HTTP response: %v\n%s", err, rec.Body.String())
	}
	return resp
}

func assertNoGraphQLErrors(t *testing.T, resp graphQLHTTPResponse) {
	t.Helper()
	if len(resp.Errors) != 0 {
		t.Fatalf("expected no GraphQL errors, got %s", string(mustMarshalTestJSON(resp.Errors)))
	}
}

func mustMarshalTestJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(err.Error())
	}
	return b
}
