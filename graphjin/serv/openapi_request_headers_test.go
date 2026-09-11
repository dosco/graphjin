package serv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/openapi"
	jwt "github.com/golang-jwt/jwt/v5"
)

func TestOpenAPIRequestCredentialsHTTP(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token != "alice" && token != "bob" {
			t.Errorf("unexpected upstream identity: %q", token)
		}
		if r.Header.Get("X-Unrelated-Secret") != "" || r.Header.Get("Cookie") != "" {
			t.Error("unconfigured headers reached upstream")
		}
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "1", "name": token})
	}))
	defer upstream.Close()
	specDir := t.TempDir()
	spec, err := os.ReadFile(filepath.Join("..", "core", "openapi", "testdata", "generic_mutations.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	spec = bytes.ReplaceAll(spec, []byte("/widgets/{id}"), []byte("/widgets/{widgetId}"))
	spec = bytes.ReplaceAll(spec, []byte("name: id, in: path"), []byte("name: widgetId, in: path"))
	for _, name := range []string{"personal", "joined"} {
		if err := os.WriteFile(filepath.Join(specDir, name+".yaml"), spec, 0600); err != nil {
			t.Fatal(err)
		}
	}
	svc := newControlPlaneGraphQLTestServiceWithConfig(t, MCPConfig{AllowRawQueries: true, AllowMutations: true}, createSourceModeHTTPDB(t), func(conf *Config) {
		conf.Core.Roles = []core.Role{{Name: "member"}}
		authConfig := openapi.AuthConfig{Scheme: "bearer", TokenFromRequest: &openapi.TokenFromRequest{Header: "X-Workspace-Token"}}
		conf.Core.Sources = append(conf.Core.Sources, core.SourceConfig{
			Name: "workspace", Kind: "api", SpecsDir: specDir,
			Capabilities: map[string]bool{"api.read": true, "api.write": true},
			Access:       core.SourceAccessConfig{Read: core.AccessModeAuthenticated, Write: core.AccessModeAuthenticated},
			Specs: map[string]openapi.SpecConfig{
				"personal": {BaseURL: upstream.URL, Auth: authConfig, Operations: map[string]openapi.OperationOverride{
					"getWidget":    {ExposeAs: "personal_widget", ExposeTopLevel: true},
					"createWidget": {ExposeAs: "create_personal_widget", ExposeMutation: true, AllowedRoles: []string{"member"}},
				}},
				"joined": {BaseURL: upstream.URL, Auth: authConfig, Joins: map[string]openapi.JoinConfig{
					"getWidget": {ParentTable: "users", ParentColumn: "id", Param: "widgetId", ExposeAs: "personal_profile"},
				}},
			},
		})
		conf.Serv.Auth = Auth{Type: "jwt", JWT: JWTConfig{Secret: sourceModeHTTPJWTSecret}}
		conf.Serv.AuthFailBlock = true
		conf.Serv.CacheControl = "public, max-age=3600"
	})
	cache := &personalCredentialTestCache{data: make(map[string][]byte)}
	opts := append(svc.buildCoreOptionsFor(svc.dbs, nil), core.OptionSetResponseCache(cache))
	svc.gj.SetOptions(opts...)
	if err := svc.gj.Reload(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cache.personalWrites.Load() != 0 {
			t.Error("personal API results entered the shared fragment cache")
		}
		if cache.writes.Load() == 0 {
			t.Error("test did not exercise enabled fragment caching")
		}
	})
	ah, err := auth.NewAuthHandlerFunc(svc.conf.Auth)
	if err != nil {
		t.Fatal(err)
	}
	hs := &HttpService{}
	hs.Store(svc)
	t.Cleanup(svc.closeMCPHTTPTransport)
	tokens := map[string]string{}
	for _, user := range []string{"alice", "bob"} {
		tokens[user] = signSourceModeJWT(t, jwt.MapClaims{"sub": user, "role": "member", "roles": []string{"member"}, "account_id": "acct_1"})
	}
	const query = `query Personal { personal_widget(widgetId: "1") { id name } users(id: 1) { id personal_profile { id name } } }`
	const mutation = `mutation PersonalWrite($call: JSON!) { create_personal_widget(call: $call) { ok response_json } }`
	for _, transport := range []string{"graphql", "mcp", "mcp-message"} {
		t.Run(transport, func(t *testing.T) {
			handler := hs.GraphQL(ah)
			if transport == "mcp" {
				handler = hs.MCPHandlerWithAuth(ah)
			}
			if transport == "mcp-message" {
				handler = hs.MCPMessageHandlerWithAuth(ah)
			}
			send := func(user, credential, gql string, vars map[string]any) string {
				payload := map[string]any{"query": gql, "variables": vars}
				if transport != "graphql" {
					payload = map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{
						"name": "execute_graphql", "arguments": payload,
						"_meta": map[string]any{
							"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
							"io.modelcontextprotocol/clientCapabilities": map[string]any{},
							"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "credential-test", "version": "1"},
						},
					}}
				}
				body, _ := json.Marshal(payload)
				req := httptest.NewRequest(http.MethodPost, "/api/v1/"+transport, bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", "application/json")
				if transport != "graphql" {
					req.Header.Set("Accept", "application/json, text/event-stream")
				}
				req.Header.Set("Authorization", "Bearer "+tokens[user])
				req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
				req.Header.Set("Mcp-Method", "tools/call")
				req.Header.Set("Mcp-Name", "execute_graphql")
				req.Header.Set("X-Unrelated-Secret", "do-not-forward")
				req.Header.Set("Cookie", "session=do-not-forward")
				if credential != "" {
					req.Header.Set("X-Workspace-Token", credential)
				}
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Errorf("HTTP %d: %s", rec.Code, rec.Body.String())
				}
				if rec.Header().Get("Cache-Control") != "private, no-store" {
					t.Error("personal response was cacheable")
				}
				return rec.Body.String()
			}
			assertAccount := func(user, credential string) {
				for _, gql := range []string{query, mutation} {
					result := send(user, credential, gql, map[string]any{"call": map[string]any{"body": map[string]any{"name": "widget"}}})
					other := "alice"
					if credential == "alice" {
						other = "bob"
					}
					if !strings.Contains(result, credential) || strings.Contains(result, other) || strings.Contains(result, `"errors"`) || strings.Contains(result, `"isError":true`) {
						t.Errorf("wrong account response for %s: %s", credential, result)
					}
				}
			}
			// Compile once before concurrent calls; then exercise both accounts
			// and reconnecting the same app identity to another upstream account.
			assertAccount("alice", "alice")
			var wg sync.WaitGroup
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func(i int) { defer wg.Done(); user := []string{"alice", "bob"}[i%2]; assertAccount(user, user) }(i)
			}
			wg.Wait()
			assertAccount("alice", "bob")
			before := calls.Load()
			for _, gql := range []string{query, mutation} {
				result := send("alice", "", gql, map[string]any{"call": map[string]any{"body": map[string]any{"name": "widget"}}, "X-Workspace-Token": "alice"})
				if !strings.Contains(result, "pass-through header") {
					t.Errorf("missing credentials not rejected: %s", result)
				}
			}
			if calls.Load() != before {
				t.Error("missing credentials reached upstream")
			}
		})
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/graphql?query="+"%7Bpersonal_widget(widgetId:%221%22)%7Bid%20name%7D%7D", nil)
	req.Header.Set("Authorization", "Bearer "+tokens["alice"])
	req.Header.Set("X-Workspace-Token", "alice")
	rec := httptest.NewRecorder()
	hs.GraphQL(ah).ServeHTTP(rec, req)
	if rec.Header().Get("Cache-Control") != "private, no-store" || rec.Header().Get("ETag") != "" {
		t.Fatal("GET exposed public caching")
	}
	ctx := context.WithValue(context.Background(), core.UserIDKey, "alice")
	ctx = context.WithValue(ctx, core.UserRoleKey, "member")
	ctx = openapi.WithRequestHeaders(ctx, http.Header{"X-Workspace-Token": {"alice"}})
	var subscriptions sync.WaitGroup
	for i := 0; i < 16; i++ {
		subscriptions.Add(1)
		go func() {
			defer subscriptions.Done()
			member, err := svc.gj.Subscribe(ctx, `subscription PersonalSubscription { users(id: 1) { personal_profile { name } } }`, nil, nil)
			if err == nil {
				member.Unsubscribe()
				t.Error("personal subscription was accepted")
			} else if !strings.Contains(fmt.Sprint(err), "request-scoped OpenAPI credentials") {
				t.Errorf("unexpected subscription error: %v", err)
			}
		}()
	}
	subscriptions.Wait()
}

// Retain real DB fragments so the test exercises cache hits as well as misses.
type personalCredentialTestCache struct {
	mu             sync.Mutex
	data           map[string][]byte
	writes         atomic.Int32
	personalWrites atomic.Int32
}

func (c *personalCredentialTestCache) Get(_ context.Context, key string) ([]byte, bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, ok := c.data[key]
	return data, false, ok
}
func (c *personalCredentialTestCache) Set(_ context.Context, key string, data []byte, refs []core.RowRef, _ time.Time) error {
	c.writes.Add(1)
	for _, ref := range refs {
		if ref.Source == core.CacheSourceRemote {
			c.personalWrites.Add(1)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = append([]byte(nil), data...)
	return nil
}
func (c *personalCredentialTestCache) InvalidateRows(context.Context, []core.RowRef) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.data)
	return nil
}
