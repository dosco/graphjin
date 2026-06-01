package tests_test

import (
	"database/sql"
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

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/serv/v3"
)

func TestCombinedCacheInvalidationSources(t *testing.T) {
	ctx := sourceModeIntegrationUserContext()

	appDBPath := createCacheInvalidationAppDB(t)
	fsRoot := t.TempDir()
	codeRoot := t.TempDir()
	configRoot := filepath.Join(t.TempDir(), "config")

	writeCacheInvalidationFile(t, filepath.Join(fsRoot, "docs", "guide.txt"), "one")
	writeCodeSQLFixture(t, filepath.Join(codeRoot, "main.go"), `package main

func WatchMe() int {
	return 1
}
`)

	apiVersion := atomic.Int64{}
	apiVersion.Store(1)
	apiCalls := atomic.Int64{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls.Add(1)
		id := strings.TrimPrefix(r.URL.Path, "/payments/")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":[{"desc":"payment for %s","version":%d}]}`, id, apiVersion.Load()) //nolint:errcheck
	}))
	t.Cleanup(api.Close)

	gjs, err := serv.NewGraphJinService(&serv.Config{
		Core: core.Config{
			DisableAllowList:     true,
			DBSchemaPollDuration: -1,
			DefaultLimit:         10,
			Sources: []core.SourceConfig{
				{Name: "app", Kind: "database", Type: "sqlite", Path: appDBPath, Default: true, Access: core.SourceAccessConfig{
					Read:  core.AccessModeAuthenticated,
					Write: core.AccessModeAuthenticated,
				}},
				{Name: "code", Kind: "code", Path: codeRoot},
				{Name: "uploads", Kind: "file", Backend: "local", Root: fsRoot},
			},
			Resolvers: []core.ResolverConfig{{
				Name:      "payments",
				Type:      "remote_api",
				Table:     "users",
				Column:    "stripe_id",
				StripPath: "data",
				Props:     core.ResolverProps{"url": api.URL + "/payments/$id"},
			}},
		},
		Serv: serv.Serv{
			ConfigPath: configRoot,
			MCP:        serv.MCPConfig{Disable: true},
			Caching:    serv.CachingConfig{TTL: 3600, FreshTTL: 300},
		},
	})
	if err != nil {
		t.Fatalf("new graphjin service: %v", err)
	}
	t.Cleanup(func() { _ = gjs.Close() })

	gj := gjs.GetGraphJin()
	initial := queryCombinedSources(t, gj)
	assertCombinedEmail(t, initial, "initial@test.com")
	assertCombinedPaymentVersion(t, initial, 1)
	initialETag := assertCombinedUpload(t, initial, "docs/guide.txt")
	initialCodeHash := assertCombinedCode(t, initial, "return 1")
	if got := apiCalls.Load(); got != 1 {
		t.Fatalf("api calls after initial query = %d, want 1", got)
	}

	again := queryCombinedSources(t, gj)
	assertCombinedEmail(t, again, "initial@test.com")
	assertCombinedPaymentVersion(t, again, 1)
	if got := apiCalls.Load(); got != 1 {
		t.Fatalf("api calls after cached query = %d, want 1", got)
	}
	for _, cached := range queryCombinedSourcesConcurrently(t, gj, 8) {
		assertCombinedEmail(t, cached, "initial@test.com")
		assertCombinedPaymentVersion(t, cached, 1)
		assertCombinedUpload(t, cached, "docs/guide.txt")
		assertCombinedCode(t, cached, "return 1")
	}
	if got := apiCalls.Load(); got != 1 {
		t.Fatalf("api calls after concurrent cached queries = %d, want 1", got)
	}

	if _, err := gj.GraphQL(ctx, `mutation {
		users(id: 1, update: { email: "updated@test.com" }) { id email }
	}`, nil, nil); err != nil {
		t.Fatalf("db mutation: %v", err)
	}
	afterDB := queryCombinedSources(t, gj)
	assertCombinedEmail(t, afterDB, "updated@test.com")
	assertCombinedPaymentVersion(t, afterDB, 1)
	if got := apiCalls.Load(); got != 1 {
		t.Fatalf("api calls after db invalidation = %d, want 1", got)
	}

	writeCacheInvalidationFile(t, filepath.Join(fsRoot, "docs", "guide.txt"), "two")
	afterFS := waitForCombinedSources(t, gj, func(out combinedSourcesResult) bool {
		return uploadETag(out, "docs/guide.txt") != "" && uploadETag(out, "docs/guide.txt") != initialETag
	})
	assertCombinedEmail(t, afterFS, "updated@test.com")
	assertCombinedPaymentVersion(t, afterFS, 1)
	if got := apiCalls.Load(); got != 1 {
		t.Fatalf("api calls after filesystem invalidation = %d, want 1", got)
	}

	writeCodeSQLFixture(t, filepath.Join(codeRoot, "main.go"), `package main

func WatchMe() int {
	return 2
}
`)
	afterCode := waitForCombinedSources(t, gj, func(out combinedSourcesResult) bool {
		return len(out.GJCode) == 1 &&
			strings.Contains(out.GJCode[0].Code, "return 2") &&
			out.GJCode[0].Hash != initialCodeHash
	})
	assertCombinedEmail(t, afterCode, "updated@test.com")
	assertCombinedPaymentVersion(t, afterCode, 1)
	if got := apiCalls.Load(); got != 1 {
		t.Fatalf("api calls after codesql invalidation = %d, want 1", got)
	}

	apiVersion.Store(2)
	beforeRemoteInvalidate := queryCombinedSources(t, gj)
	assertCombinedPaymentVersion(t, beforeRemoteInvalidate, 1)
	if got := apiCalls.Load(); got != 1 {
		t.Fatalf("api calls before manual remote invalidation = %d, want 1", got)
	}

	if err := gj.InvalidateCacheRefs(ctx, core.RemoteResolverRefs("payments", "payment_id_1001")); err != nil {
		t.Fatalf("manual remote invalidation: %v", err)
	}
	afterRemote := queryCombinedSources(t, gj)
	assertCombinedEmail(t, afterRemote, "updated@test.com")
	assertCombinedPaymentVersion(t, afterRemote, 2)
	if got := apiCalls.Load(); got != 2 {
		t.Fatalf("api calls after manual remote invalidation = %d, want 2", got)
	}
}

const combinedSourcesQuery = `query CombinedSources {
	users(where: { id: { eq: 1 } }) {
		id
		email
		stripe_id
		payments {
			desc
			version
		}
	}
	uploads(prefix: "docs/") {
		key
		etag
	}
	gj_code(where: { kind: { eq: "symbol" }, name: { eq: "WatchMe" } }, limit: 1) {
		name
		code
		hash
	}
}`

type combinedSourcesResult struct {
	Users []struct {
		ID       int64  `json:"id"`
		Email    string `json:"email"`
		StripeID string `json:"stripe_id"`
		Payments []struct {
			Desc    string `json:"desc"`
			Version int64  `json:"version"`
		} `json:"payments"`
	} `json:"users"`
	Uploads []struct {
		Key  string `json:"key"`
		ETag string `json:"etag"`
	} `json:"uploads"`
	GJCode []struct {
		Name string `json:"name"`
		Code string `json:"code"`
		Hash string `json:"hash"`
	} `json:"gj_code"`
}

func createCacheInvalidationAppDB(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "app.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close() //nolint:errcheck

	if _, err := db.Exec(`CREATE TABLE users (
		id integer primary key,
		email text not null,
		stripe_id text not null
	);`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users(id, email, stripe_id) VALUES (1, 'initial@test.com', 'payment_id_1001')`); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeCacheInvalidationFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func queryCombinedSources(t *testing.T, gj *core.GraphJin) combinedSourcesResult {
	t.Helper()
	out, err := queryCombinedSourcesResult(gj)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func queryCombinedSourcesResult(gj *core.GraphJin) (combinedSourcesResult, error) {
	res, err := gj.GraphQL(sourceModeIntegrationUserContext(), combinedSourcesQuery, nil, nil)
	if err != nil {
		return combinedSourcesResult{}, err
	}
	var out combinedSourcesResult
	if err := json.Unmarshal(res.Data, &out); err != nil {
		return combinedSourcesResult{}, fmt.Errorf("combined response: %w\n%s", err, res.Data)
	}
	return out, nil
}

func queryCombinedSourcesConcurrently(t *testing.T, gj *core.GraphJin, n int) []combinedSourcesResult {
	t.Helper()
	type queryResult struct {
		out combinedSourcesResult
		err error
	}

	results := make(chan queryResult, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			out, err := queryCombinedSourcesResult(gj)
			results <- queryResult{out: out, err: err}
		}()
	}
	wg.Wait()
	close(results)

	out := make([]combinedSourcesResult, 0, n)
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		out = append(out, result.out)
	}
	return out
}

func waitForCombinedSources(t *testing.T, gj *core.GraphJin, ok func(combinedSourcesResult) bool) combinedSourcesResult {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	var last combinedSourcesResult
	for time.Now().Before(deadline) {
		last = queryCombinedSources(t, gj)
		if ok(last) {
			return last
		}
		time.Sleep(75 * time.Millisecond)
	}
	t.Fatalf("combined sources did not satisfy condition before deadline: %+v", last)
	return combinedSourcesResult{}
}

func assertCombinedEmail(t *testing.T, out combinedSourcesResult, want string) {
	t.Helper()
	if len(out.Users) != 1 {
		t.Fatalf("users len = %d, want 1: %+v", len(out.Users), out.Users)
	}
	if out.Users[0].Email != want {
		t.Fatalf("user email = %q, want %q", out.Users[0].Email, want)
	}
}

func assertCombinedPaymentVersion(t *testing.T, out combinedSourcesResult, want int64) {
	t.Helper()
	if len(out.Users) != 1 || len(out.Users[0].Payments) != 1 {
		t.Fatalf("payments = %+v, want one payment", out.Users)
	}
	if out.Users[0].Payments[0].Version != want {
		t.Fatalf("payment version = %d, want %d", out.Users[0].Payments[0].Version, want)
	}
}

func assertCombinedUpload(t *testing.T, out combinedSourcesResult, key string) string {
	t.Helper()
	etag := uploadETag(out, key)
	if etag == "" {
		t.Fatalf("missing upload %q in %+v", key, out.Uploads)
	}
	return etag
}

func uploadETag(out combinedSourcesResult, key string) string {
	for _, upload := range out.Uploads {
		if upload.Key == key {
			return upload.ETag
		}
	}
	return ""
}

func assertCombinedCode(t *testing.T, out combinedSourcesResult, wantSnippet string) string {
	t.Helper()
	if len(out.GJCode) != 1 {
		t.Fatalf("gj_code symbol len = %d, want 1: %+v", len(out.GJCode), out.GJCode)
	}
	if !strings.Contains(out.GJCode[0].Code, wantSnippet) {
		t.Fatalf("code = %q, want snippet %q", out.GJCode[0].Code, wantSnippet)
	}
	if out.GJCode[0].Hash == "" {
		t.Fatalf("missing code file hash: %+v", out.GJCode[0])
	}
	return out.GJCode[0].Hash
}
