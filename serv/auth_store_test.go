package serv

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dosco/graphjin/auth/v3/oidc"
	"github.com/dosco/graphjin/core/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/spf13/afero"
)

const authStoreTestSchema = "_graphjin"

// openAuthStoreReplica opens its own connection to the shared store file and
// returns a login service on top of it, standing in for one GraphJin replica.
func openAuthStoreReplica(t *testing.T, path string) (*authLoginService, *sql.DB) {
	t.Helper()
	db, err := openManagedArtifactSQLite(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() }) //nolint:errcheck
	for _, stmt := range authStoreDDL("sqlite", authStoreTestSchema) {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("ddl: %v", err)
		}
	}
	if err := migrateAuthStoreSchema(context.Background(), db, "sqlite", authStoreTestSchema); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	a := newStubAuthLogin(t)
	a.store = newSQLAuthStore(db, "sqlite", authStoreTestSchema)
	return a, db
}

func authStoreRowCount(t *testing.T, db *sql.DB, where string, args ...any) int {
	t.Helper()
	var n int
	q := `SELECT COUNT(*) FROM "_graphjin_auth_sessions"`
	if where != "" {
		q += " WHERE " + where
	}
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestAuthStoreAuthCodeRedeemedOnceAcrossReplicas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.db")
	issuerSvc, _ := openAuthStoreReplica(t, path)
	replicas := []*authLoginService{issuerSvc}
	for i := 0; i < 3; i++ {
		a, _ := openAuthStoreReplica(t, path)
		replicas = append(replicas, a)
	}

	resource := "https://graphjin.example.com/api/v1/mcp"
	verifier := "verifier-with-enough-entropy-for-the-test"
	if err := issuerSvc.state().putOAuthCode(context.Background(), "code-1", &mcpOAuthCode{
		ClientID:    "client-1",
		RedirectURI: "http://127.0.0.1/callback",
		Scope:       "mcp",
		Resource:    resource,
		Challenge:   pkceChallenge(verifier),
		Identity:    &oidc.Identity{Subject: "u1", Email: "u1@example.com", Issuer: "https://idp.example"},
		ExpiresAt:   time.Now().Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	form := url.Values{
		"code":          {"code-1"},
		"client_id":     {"client-1"},
		"redirect_uri":  {"http://127.0.0.1/callback"},
		"code_verifier": {verifier},
		"resource":      {resource},
	}.Encode()

	const attemptsPerReplica = 4
	var wg sync.WaitGroup
	codes := make(chan int, len(replicas)*attemptsPerReplica)
	bodies := make(chan string, len(replicas)*attemptsPerReplica)
	for _, a := range replicas {
		for i := 0; i < attemptsPerReplica; i++ {
			wg.Add(1)
			go func(a *authLoginService) {
				defer wg.Done()
				req := httptest.NewRequest(http.MethodPost, routeMCPOAuthToken, strings.NewReader(form))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if err := req.ParseForm(); err != nil {
					t.Error(err)
					return
				}
				rec := httptest.NewRecorder()
				a.handleMCPOAuthAuthorizationCodeToken(rec, req)
				codes <- rec.Code
				bodies <- rec.Body.String()
			}(a)
		}
	}
	wg.Wait()
	close(codes)
	close(bodies)

	ok := 0
	for c := range codes {
		b := <-bodies
		switch {
		case c == http.StatusOK:
			ok++
		case c == http.StatusBadRequest && strings.Contains(b, "invalid_grant"):
		default:
			t.Errorf("unexpected response %d %s", c, b)
		}
	}
	if ok != 1 {
		t.Fatalf("authorization code redeemed %d times across replicas, want exactly 1", ok)
	}
}

func TestAuthStoreDeviceFlowAcrossReplicas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.db")
	a, db := openAuthStoreReplica(t, path)
	b, _ := openAuthStoreReplica(t, path)
	ctx := context.Background()

	// Replica A starts the session.
	req := httptest.NewRequest(http.MethodPost, routeAuthDevice, bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	a.handleDevice(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("device start: %d %s", rec.Code, rec.Body.String())
	}
	var start map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &start); err != nil {
		t.Fatal(err)
	}
	dc, uc := start["device_code"].(string), start["user_code"].(string)

	// No bearer secret is stored in plaintext.
	if n := authStoreRowCount(t, db, "id = ? OR lookup_key = ? OR data LIKE ? OR data LIKE ?", dc, uc, "%"+dc+"%", "%"+uc+"%"); n != 0 {
		t.Fatalf("device or user code stored in plaintext (%d rows)", n)
	}

	// The browser lands on replica B: user code lookup, OIDC redirect state,
	// and the callback all resolve against A's session.
	ref, ok, err := b.state().lookupUserCode(ctx, uc, time.Now())
	if err != nil || !ok {
		t.Fatalf("lookupUserCode on replica B: ok=%v err=%v", ok, err)
	}
	if err := b.state().bindDeviceState(ctx, ref, "oidc-state-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	gotRef, err := a.state().deviceByState(ctx, "oidc-state-1", time.Now())
	if err != nil || gotRef != ref {
		t.Fatalf("deviceByState on replica A: ref=%q err=%v", gotRef, err)
	}
	ident := &oidc.Identity{Subject: "u1", Email: "u1@example.com", Issuer: "https://idp.example", Raw: map[string]any{"secret_claim": "x"}}
	if err := a.state().completeDevice(ctx, ref, "oidc-state-1", ident); err != nil {
		t.Fatal(err)
	}
	if n := authStoreRowCount(t, db, "data LIKE ?", "%secret_claim%"); n != 0 {
		t.Fatal("raw id_token claims were persisted")
	}

	// Both replicas poll concurrently; exactly one redeems the token.
	body, _ := json.Marshal(map[string]string{"device_code": dc})
	var wg sync.WaitGroup
	var mu sync.Mutex
	var tokens []string
	for i := 0; i < 8; i++ {
		svc := a
		if i%2 == 1 {
			svc = b
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, routeAuthDeviceToken, bytes.NewReader(body))
			rec := httptest.NewRecorder()
			svc.handleDeviceToken(rec, req)
			var r map[string]any
			_ = json.Unmarshal(rec.Body.Bytes(), &r)
			switch {
			case rec.Code == http.StatusOK:
				mu.Lock()
				tokens = append(tokens, r["token"].(string))
				mu.Unlock()
			case r["error"] == "slow_down" || r["error"] == "access_denied":
			default:
				t.Errorf("unexpected poll response %d %s", rec.Code, rec.Body.String())
			}
		}()
	}
	wg.Wait()
	if len(tokens) != 1 {
		t.Fatalf("device session redeemed %d times, want exactly 1", len(tokens))
	}
	claims := jwt.MapClaims{}
	if _, err := jwt.ParseWithClaims(tokens[0], claims, func(*jwt.Token) (any, error) {
		return []byte("test-secret-plenty-long"), nil
	}); err != nil {
		t.Fatalf("minted token does not verify: %v", err)
	}
	if claims["email"] != "u1@example.com" {
		t.Fatalf("token claims = %v", claims)
	}

	// Later polls on either replica (past the slow_down window) see the
	// redemption.
	if err := a.store.(*sqlAuthStore).updateDevice(ctx, ref, func(row *authStoreRow, s *deviceSession) (string, error) {
		s.LastPollAt = time.Now().Add(-time.Hour)
		return row.stateKey, nil
	}); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	b.handleDeviceToken(rec, httptest.NewRequest(http.MethodPost, routeAuthDeviceToken, bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "already redeemed") {
		t.Fatalf("poll after redemption: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAuthStoreRefreshTokenSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.db")
	resource := "https://graphjin.example.com/api/v1/mcp"

	before, db := openAuthStoreReplica(t, path)
	rec := httptest.NewRecorder()
	before.writeMCPOAuthTokenResponse(context.Background(), rec, "client-1", "mcp", resource,
		&oidc.Identity{Subject: "u1", Email: "u1@example.com", Issuer: "https://idp.example"})
	if rec.Code != http.StatusOK {
		t.Fatalf("token response: %d %s", rec.Code, rec.Body.String())
	}
	var tok map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &tok); err != nil {
		t.Fatal(err)
	}
	refresh := tok["refresh_token"].(string)
	if n := authStoreRowCount(t, db, "id = ? OR data LIKE ?", refresh, "%"+refresh+"%"); n != 0 {
		t.Fatal("refresh token stored in plaintext")
	}
	if n := authStoreRowCount(t, db, "kind = ? AND id = ?", authKindRefreshToken, authStoreKey(refresh)); n != 1 {
		t.Fatalf("hashed refresh token rows = %d, want 1", n)
	}
	db.Close() //nolint:errcheck

	// A fresh process (new connection, empty memory) honours the token.
	after, _ := openAuthStoreReplica(t, path)
	form := url.Values{"client_id": {"client-1"}, "refresh_token": {refresh}, "resource": {resource}}.Encode()
	req := httptest.NewRequest(http.MethodPost, routeMCPOAuthToken, strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	after.handleMCPOAuthRefreshToken(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh after restart: %d %s", rec.Code, rec.Body.String())
	}
	if len(after.oauthRefreshTokens) != 0 {
		t.Fatal("store-backed service must not fall back to the in-memory maps")
	}
}

func TestAuthStoreSweepRemovesExpiredRows(t *testing.T) {
	a, db := openAuthStoreReplica(t, filepath.Join(t.TempDir(), "store.db"))
	ctx := context.Background()
	st := a.state()
	past, future := time.Now().Add(-time.Minute), time.Now().Add(time.Hour)

	mustNil := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	mustNil(st.createDevice(ctx, &deviceSession{DeviceCode: "dc-old", UserCode: "OLD", ExpiresAt: past}))
	mustNil(st.createDevice(ctx, &deviceSession{DeviceCode: "dc-new", UserCode: "NEW", ExpiresAt: future}))
	mustNil(st.putOAuthRequest(ctx, "state-old", &mcpOAuthAuthRequest{ExpiresAt: past}))
	mustNil(st.putOAuthCode(ctx, "code-old", &mcpOAuthCode{ExpiresAt: past}))
	mustNil(st.putRefreshToken(ctx, "rt-old", &mcpOAuthRefreshToken{ExpiresAt: past}))
	mustNil(st.putRefreshToken(ctx, "rt-new", &mcpOAuthRefreshToken{ExpiresAt: future}))
	mustNil(st.putOAuthClient(ctx, &mcpOAuthClient{ClientID: "cimd-old"}, past))
	mustNil(st.putOAuthClient(ctx, &mcpOAuthClient{ClientID: "dcr-client"}, time.Time{}))

	mustNil(st.sweep(ctx, time.Now()))

	if n := authStoreRowCount(t, db, ""); n != 3 {
		t.Fatalf("rows after sweep = %d, want 3 (live device, live refresh token, non-expiring client)", n)
	}
	for _, live := range []struct{ kind, secret string }{
		{authKindDevice, "dc-new"},
		{authKindRefreshToken, "rt-new"},
		{authKindOAuthClient, "dcr-client"},
	} {
		if n := authStoreRowCount(t, db, "kind = ? AND id = ?", live.kind, authStoreKey(live.secret)); n != 1 {
			t.Errorf("%s row was swept", live.kind)
		}
	}
}

func TestAuthStoreExpiredDeviceReportsExpired(t *testing.T) {
	a, db := openAuthStoreReplica(t, filepath.Join(t.TempDir(), "store.db"))
	if err := a.state().createDevice(context.Background(), &deviceSession{
		DeviceCode: "dc1", UserCode: "X-Y", ExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"device_code": "dc1"})
	rec := httptest.NewRecorder()
	a.handleDeviceToken(rec, httptest.NewRequest(http.MethodPost, routeAuthDeviceToken, bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "expired_token") {
		t.Fatalf("poll expired: %d %s", rec.Code, rec.Body.String())
	}
	if n := authStoreRowCount(t, db, ""); n != 0 {
		t.Fatal("expired session should be evicted on poll")
	}
}

func TestAuthStorePostgresPlaceholders(t *testing.T) {
	st := newSQLAuthStore(nil, "postgres", "_graphjin")
	got := st.q(`DELETE FROM {t} WHERE kind = ? AND id = ?`)
	want := `DELETE FROM "_graphjin"."auth_sessions" WHERE kind = $1 AND id = $2`
	if got != want {
		t.Fatalf("q = %s", got)
	}
	if got := newSQLAuthStore(nil, "mysql", "_graphjin").q(`SELECT ? FROM {t}`); got != "SELECT ? FROM `_graphjin_auth_sessions`" {
		t.Fatalf("mysql q = %s", got)
	}
}

func TestAuthStoreRedisCachesImmutableReads(t *testing.T) {
	client := startRedisUnixForTest(t)
	a, db := openAuthStoreReplica(t, filepath.Join(t.TempDir(), "store.db"))
	st := a.store.(*sqlAuthStore)
	st.cache = client
	ctx := context.Background()

	expires := time.Now().Add(2 * time.Minute)
	if err := st.putRefreshToken(ctx, "rt-1", &mcpOAuthRefreshToken{ClientID: "c1", ExpiresAt: expires}); err != nil {
		t.Fatal(err)
	}
	if rt, err := st.getRefreshToken(ctx, "rt-1", time.Now()); err != nil || rt == nil || rt.ClientID != "c1" {
		t.Fatalf("first read: %v %v", rt, err)
	}
	key := authStoreCachePrefix + authKindRefreshToken + ":" + authStoreKey("rt-1")
	ttl, err := client.TTL(ctx, key).Result()
	if err != nil || ttl <= 0 {
		t.Fatalf("refresh token not cached: ttl=%v err=%v", ttl, err)
	}
	// TTL is reported in whole seconds.
	if ttl > time.Until(expires)+time.Second {
		t.Fatalf("cache ttl %v outlives the row (%v)", ttl, time.Until(expires))
	}
	if strings.Contains(client.Get(ctx, key).Val(), "rt-1") {
		t.Fatal("plaintext refresh token reached Redis")
	}

	// Client re-registration invalidates the cached copy.
	if err := st.putOAuthClient(ctx, &mcpOAuthClient{ClientID: "c1", ClientName: "v1"}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if c, _ := st.getOAuthClient(ctx, "c1", time.Now()); c == nil || c.ClientName != "v1" {
		t.Fatalf("client v1 read = %v", c)
	}
	if err := st.putOAuthClient(ctx, &mcpOAuthClient{ClientID: "c1", ClientName: "v2"}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if c, _ := st.getOAuthClient(ctx, "c1", time.Now()); c == nil || c.ClientName != "v2" {
		t.Fatalf("client read after update = %v", c)
	}
	if n := authStoreRowCount(t, db, "kind = ?", authKindOAuthClient); n != 1 {
		t.Fatalf("client rows = %d, want 1 after upsert", n)
	}
}

// TestAuthStoreServiceInitCreatesBlockedTable covers the serve-startup path:
// the table is created in the artifact store and no role, including the
// internal store role, can reach it through GraphQL.
func TestAuthStoreServiceInitCreatesBlockedTable(t *testing.T) {
	tmp := t.TempDir()
	dsn := "file:" + filepath.Join(tmp, "app.db")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() }) //nolint:errcheck

	autoInit := true
	conf := &Config{Core: core.Config{
		Sources: []core.SourceConfig{
			{Name: "app", Kind: "database", Type: "sqlite", Path: dsn, Default: true},
		},
		Artifacts: core.ArtifactsConfig{Enabled: true, Source: "app", AutoInit: &autoInit, GlobalsPath: "."},
	}}
	conf.AuthLogin.Enabled = true
	conf.ConfigPath = tmp
	if err := conf.Core.NormalizeSources(); err != nil {
		t.Fatal(err)
	}
	svc := &graphjinService{conf: conf, dbs: map[string]*sql.DB{"app": db}, fs: newAferoFS(afero.NewMemMapFs(), "/")}

	store, err := svc.openAuthStateStore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.(*sqlAuthStore); !ok {
		t.Fatalf("store = %T, want *sqlAuthStore", store)
	}
	if !sqliteTableExists(t, db, "_graphjin_auth_sessions") {
		t.Fatal("auth store table not created")
	}
	// Re-running startup is idempotent.
	if _, err := svc.openAuthStateStore(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatal(err)
	}
	if err := store.putRefreshToken(context.Background(), "rt-1", &mcpOAuthRefreshToken{ClientID: "c1", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	startSQLiteArtifactCore(t, svc, db)
	coreConf := &svc.conf.Core
	if svc.runtimeCore != nil {
		coreConf = svc.runtimeCore
	}
	blocked := map[string]bool{}
	for _, role := range coreConf.Roles {
		for _, rt := range role.Tables {
			if rt.Name == "_graphjin_auth_sessions" {
				if rt.Query == nil || !rt.Query.Block || rt.Insert == nil || !rt.Insert.Block {
					t.Fatalf("role %s can reach the auth store: %+v", role.Name, rt)
				}
				blocked[role.Name] = true
			}
		}
	}
	for _, role := range []string{graphjinInternalStoreRole, "user", "anon"} {
		if !blocked[role] {
			t.Errorf("auth store not blocked for role %q", role)
		}
	}

	// End to end, no role reads the rows. In this source-mode config other
	// defaults also refuse user/anon, so the role-table assertions above are
	// what pin the explicit block.
	for _, role := range []string{"user", "anon", graphjinInternalStoreRole} {
		ctx := contextWithUserRole(context.Background(), role)
		if role != "anon" {
			ctx = context.WithValue(ctx, core.UserIDKey, "u1")
		}
		res, err := svc.gj.GraphQL(ctx, `query { _graphjin_auth_sessions { id data } }`, nil, &core.RequestConfig{})
		if err == nil && res != nil && strings.Contains(string(res.Data), authKindRefreshToken) {
			t.Fatalf("role %s read auth store rows through GraphQL: %s", role, res.Data)
		}
	}

	// Without the artifact store the service keeps the in-memory fallback.
	conf.Core.Artifacts.Enabled = false
	if store, err := svc.openAuthStateStore(context.Background()); err != nil || store != nil {
		t.Fatalf("artifact store disabled: store=%v err=%v", store, err)
	}
}

// Compile-time checks that both backends satisfy the store contract.
var (
	_ authStateStore = (*sqlAuthStore)(nil)
	_ authStateStore = memoryAuthStore{}
)
