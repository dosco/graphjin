package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3/fstable"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// fsTestEngine builds the smallest graphjinEngine that the filesystem
// bridge needs: a primary db context with an empty (but Schema-set)
// dbinfo, the built-in factory map, and the supplied Filesystems config.
func fsTestEngine(t *testing.T, schema string, fs []FilesystemConfig) *graphjinEngine {
	t.Helper()
	gj := &graphjinEngine{
		conf: &Config{Filesystems: fs},
	}
	// Use the public constructor so the internal maps (colMap,
	// tableMap) are initialised — AddTable panics otherwise.
	di := sdata.NewDBInfo("postgres", 0, schema, "", nil, nil, nil)
	gj.databases = map[string]*dbContext{
		"": {dbinfo: di},
	}
	gj.defaultDB = ""
	gj.registerBuiltinFilesystemFactories()
	return gj
}

func mkfile(t *testing.T, root, rel string, body []byte) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestBridge_LocalRoundtrip wires a Filesystems config through the full
// bridge pipeline and verifies (a) the synthetic table appears in the
// schema graph, and (b) the resolver returns the expected JSON shape.
func TestBridge_LocalRoundtrip(t *testing.T) {
	root := t.TempDir()
	for _, k := range []string{"a/1.png", "a/2.png", "b/3.png"} {
		mkfile(t, root, k, []byte("hi"))
	}

	gj := fsTestEngine(t, "public", []FilesystemConfig{
		{Name: "avatars", Backend: "local", Root: root},
	})
	if err := gj.loadFilesystemIntegration(); err != nil {
		t.Fatalf("loadFilesystemIntegration: %v", err)
	}

	pdb := gj.primaryDB()
	tab, err := pdb.dbinfo.GetTable("public", "avatars")
	if err != nil {
		t.Fatalf("synthetic table not registered: %v", err)
	}
	if tab.Type != "remote" {
		t.Errorf("expected Type=remote, got %q", tab.Type)
	}
	colNames := make([]string, 0, len(tab.Columns))
	for _, c := range tab.Columns {
		colNames = append(colNames, c.Name)
	}
	for _, want := range []string{"key", "size", "content_type", "etag", "modified_at", "url", "data"} {
		found := false
		for _, n := range colNames {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("synthetic table missing column %q (have %v)", want, colNames)
		}
	}

	rfn := gj.newFilesystemResolverFn()
	r, err := rfn(ResolverProps{"fs_name": "avatars"})
	if err != nil {
		t.Fatalf("resolver factory: %v", err)
	}
	resp, err := r.Resolve(context.Background(), ResolverReq{
		Sel: &qcode.Select{ExtraArgs: map[string]string{"prefix": "a/"}},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	var wrap struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(resp, &wrap); err != nil {
		t.Fatalf("response is not the expected wrapper shape: %v\n%s", err, resp)
	}
	if len(wrap.Items) != 2 {
		t.Errorf("expected 2 entries under prefix a/, got %d: %s", len(wrap.Items), resp)
	}
	for _, it := range wrap.Items {
		if !strings.HasPrefix(it["key"].(string), "a/") {
			t.Errorf("unexpected key under a/ prefix: %v", it["key"])
		}
		if it["url"] == nil || it["url"].(string) == "" {
			t.Errorf("expected non-empty url, got %v", it["url"])
		}
		if it["data"] != nil {
			t.Errorf("expected data nil when inline_data not set, got %v", it["data"])
		}
	}

	resp2, err := r.Resolve(context.Background(), ResolverReq{
		Sel: &qcode.Select{ExtraArgs: map[string]string{"key": "a/1.png", "inline_data": "true"}},
	})
	if err != nil {
		t.Fatalf("Resolve(single): %v", err)
	}
	var wrap2 struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal(resp2, &wrap2)
	if len(wrap2.Items) != 1 {
		t.Fatalf("single-key fetch: want 1 item, got %d (%s)", len(wrap2.Items), resp2)
	}
	if wrap2.Items[0]["data"] == nil {
		t.Errorf("expected base64-populated data when inline_data=true")
	}
}

func TestBridge_RejectsDuplicateName(t *testing.T) {
	root1 := t.TempDir()
	root2 := t.TempDir()
	gj := fsTestEngine(t, "public", []FilesystemConfig{
		{Name: "x", Backend: "local", Root: root1},
		{Name: "x", Backend: "local", Root: root2},
	})
	if err := gj.loadFilesystemIntegration(); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("expected duplicate-name error, got %v", err)
	}
}

func TestBridge_RejectsUnknownBackend(t *testing.T) {
	gj := fsTestEngine(t, "public", []FilesystemConfig{
		{Name: "x", Backend: "bogus"},
	})
	if err := gj.loadFilesystemIntegration(); err == nil || !strings.Contains(err.Error(), "unknown backend") {
		t.Errorf("expected unknown-backend error, got %v", err)
	}
}

func TestBridge_PublicBaseURLOverride(t *testing.T) {
	root := t.TempDir()
	mkfile(t, root, "x.png", []byte("hi"))
	gj := fsTestEngine(t, "public", []FilesystemConfig{
		{Name: "cdn", Backend: "local", Root: root, PublicBaseURL: "https://cdn.example.com/"},
	})
	if err := gj.loadFilesystemIntegration(); err != nil {
		t.Fatal(err)
	}
	rfn := gj.newFilesystemResolverFn()
	r, _ := rfn(ResolverProps{"fs_name": "cdn"})
	resp, err := r.Resolve(context.Background(), ResolverReq{
		Sel: &qcode.Select{ExtraArgs: map[string]string{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(resp), `"url":"https://cdn.example.com/x.png"`) {
		t.Errorf("expected CDN-rewritten url, got: %s", resp)
	}
}

func TestBridge_GraphJinStyleWhereLimitAndOrder(t *testing.T) {
	root := t.TempDir()
	for _, k := range []string{"a/1.png", "a/2.png", "a/3.png", "b/1.png"} {
		mkfile(t, root, k, []byte(k))
	}

	gj := fsTestEngine(t, "public", []FilesystemConfig{
		{Name: "avatars", Backend: "local", Root: root, MaxListPageSize: 2},
	})
	if err := gj.loadFilesystemIntegration(); err != nil {
		t.Fatal(err)
	}
	rfn := gj.newFilesystemResolverFn()
	r, err := rfn(ResolverProps{"fs_name": "avatars"})
	if err != nil {
		t.Fatal(err)
	}

	keyCol := sdata.DBColumn{Name: "key"}
	where := &qcode.Exp{Op: qcode.OpLike}
	where.Left.Col = keyCol
	where.Right.ValType = qcode.ValStr
	where.Right.Val = "a/%"
	resp, err := r.Resolve(context.Background(), ResolverReq{
		Sel: &qcode.Select{
			Paging:  qcode.Paging{Limit: 2},
			Where:   qcode.Filter{Exp: where},
			OrderBy: []qcode.OrderBy{{Col: keyCol, Order: qcode.OrderDesc}},
		},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	items := decodeFilesystemItems(t, resp)
	if got := filesystemItemKeys(items); strings.Join(got, ",") != "a/3.png,a/2.png" {
		t.Fatalf("keys = %v, want a/3.png,a/2.png (resp=%s)", got, resp)
	}
}

func TestBridge_GraphJinStyleWhereKeyVariableUsesStat(t *testing.T) {
	root := t.TempDir()
	mkfile(t, root, "a/1.png", []byte("one"))
	mkfile(t, root, "a/2.png", []byte("two"))

	gj := fsTestEngine(t, "public", []FilesystemConfig{
		{Name: "avatars", Backend: "local", Root: root},
	})
	if err := gj.loadFilesystemIntegration(); err != nil {
		t.Fatal(err)
	}
	rfn := gj.newFilesystemResolverFn()
	r, err := rfn(ResolverProps{"fs_name": "avatars"})
	if err != nil {
		t.Fatal(err)
	}

	keyCol := sdata.DBColumn{Name: "key"}
	where := &qcode.Exp{Op: qcode.OpEquals}
	where.Left.Col = keyCol
	where.Right.ValType = qcode.ValVar
	where.Right.Val = "key"
	resp, err := r.Resolve(context.Background(), ResolverReq{
		Sel: &qcode.Select{
			Where: qcode.Filter{Exp: where},
		},
		Vars: map[string]json.RawMessage{"key": json.RawMessage(`"a/2.png"`)},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	items := decodeFilesystemItems(t, resp)
	if got := filesystemItemKeys(items); strings.Join(got, ",") != "a/2.png" {
		t.Fatalf("keys = %v, want a/2.png (resp=%s)", got, resp)
	}
}

func TestBridge_GraphJinQuerySurfaceEndToEnd(t *testing.T) {
	root := t.TempDir()
	mkfile(t, root, "a/1.txt", []byte("one"))
	mkfile(t, root, "a/2.txt", []byte("two"))
	mkfile(t, root, "a/3.txt", []byte("three"))
	mkfile(t, root, "b/1.txt", []byte("nope"))

	cfgRoot := t.TempDir()
	schema := []byte(`# dbinfo:postgres,120005,public

type users {
  id: Bigint! @id @unique
}
`)
	if err := os.WriteFile(filepath.Join(cfgRoot, "db.ddl"), schema, 0o644); err != nil {
		t.Fatal(err)
	}

	gj, err := NewGraphJinWithFS(&Config{
		MockDB:           true,
		EnableSchema:     true,
		DisableAllowList: true,
		DBType:           "postgres",
		Sources: []SourceConfig{
			{Name: "app", Kind: "database", Type: "postgres", Default: true},
			{Name: "avatars", Kind: "file", Backend: "local", Root: root, MaxListPageSize: 2},
		},
	}, nil, NewOsFS(cfgRoot))
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	res, err := gj.GraphQL(context.Background(), `query Avatars($prefix: String!, $limit: Int!) {
		avatars(where: { key: { like: $prefix } }, order_by: { key: desc }, limit: $limit) {
			key
			size
		}
	}`, json.RawMessage(`{"prefix":"a/%","limit":2}`), nil)
	if err != nil {
		t.Fatalf("GraphQL list: %v", err)
	}
	var list struct {
		Avatars []struct {
			Key  string `json:"key"`
			Size int64  `json:"size"`
		} `json:"avatars"`
	}
	if err := json.Unmarshal(res.Data, &list); err != nil {
		t.Fatalf("list response: %v\n%s", err, res.Data)
	}
	if got := []string{list.Avatars[0].Key, list.Avatars[1].Key}; strings.Join(got, ",") != "a/3.txt,a/2.txt" {
		t.Fatalf("keys = %v, want a/3.txt,a/2.txt (data=%s)", got, res.Data)
	}

	res, err = gj.GraphQL(context.Background(), `query Avatar($key: ID!) {
		avatars(id: $key) {
			key
			data
		}
	}`, json.RawMessage(`{"key":"a/1.txt"}`), nil)
	if err != nil {
		t.Fatalf("GraphQL id/data: %v", err)
	}
	var one struct {
		Avatars struct {
			Key  string  `json:"key"`
			Data *string `json:"data"`
		} `json:"avatars"`
	}
	if err := json.Unmarshal(res.Data, &one); err != nil {
		t.Fatalf("single response: %v\n%s", err, res.Data)
	}
	if one.Avatars.Key != "a/1.txt" || one.Avatars.Data == nil || *one.Avatars.Data != "b25l" {
		t.Fatalf("single = %+v, want key a/1.txt with base64 data b25l (data=%s)", one.Avatars, res.Data)
	}

	res, err = gj.GraphQL(context.Background(), `query AvatarsPage1 {
		avatars(first: 2, order_by: { key: asc }) {
			key
		}
		avatars_cursor
	}`, nil, nil)
	if err != nil {
		t.Fatalf("GraphQL cursor page 1: %v", err)
	}
	var page struct {
		Avatars []struct {
			Key string `json:"key"`
		} `json:"avatars"`
		Cursor string `json:"avatars_cursor"`
	}
	if err := json.Unmarshal(res.Data, &page); err != nil {
		t.Fatalf("page response: %v\n%s", err, res.Data)
	}
	if len(page.Avatars) != 2 || page.Avatars[0].Key != "a/1.txt" || page.Avatars[1].Key != "a/2.txt" {
		t.Fatalf("page 1 = %+v, want a/1.txt,a/2.txt (data=%s)", page.Avatars, res.Data)
	}
	if !strings.HasPrefix(page.Cursor, string(decPrefix)) {
		t.Fatalf("cursor = %q, want encrypted cursor prefix", page.Cursor)
	}

	res, err = gj.GraphQL(context.Background(), `query AvatarsPage2($cursor: Cursor) {
		avatars(first: 1, after: $cursor, order_by: { key: asc }) {
			key
		}
	}`, json.RawMessage(fmt.Sprintf(`{"cursor":%q}`, page.Cursor)), nil)
	if err != nil {
		t.Fatalf("GraphQL cursor page 2: %v", err)
	}
	var page2 struct {
		Avatars []struct {
			Key string `json:"key"`
		} `json:"avatars"`
	}
	if err := json.Unmarshal(res.Data, &page2); err != nil {
		t.Fatalf("page 2 response: %v\n%s", err, res.Data)
	}
	if len(page2.Avatars) != 1 || page2.Avatars[0].Key != "a/3.txt" {
		t.Fatalf("page 2 = %+v, want a/3.txt (data=%s)", page2.Avatars, res.Data)
	}
}

func TestBridge_PresignRerunsForEachResolve(t *testing.T) {
	backend := &rotatingPresignBackend{}
	bridge := &filesystemBridge{
		name:    "uploads",
		backend: backend,
		conf:    FilesystemConfig{Name: "uploads", Backend: "s3", PresignTTL: time.Minute},
	}
	sel := &qcode.Select{ExtraArgs: map[string]string{"key": "users/1/avatar.png"}}

	first, err := bridge.Resolve(context.Background(), ResolverReq{Sel: sel})
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	second, err := bridge.Resolve(context.Background(), ResolverReq{Sel: sel})
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if !strings.Contains(string(first), `"url":"https://signed.example/1/users/1/avatar.png"`) {
		t.Fatalf("first response did not include first signed url: %s", first)
	}
	if !strings.Contains(string(second), `"url":"https://signed.example/2/users/1/avatar.png"`) {
		t.Fatalf("second response did not include second signed url: %s", second)
	}
}

type rotatingPresignBackend struct {
	count int
}

func (b *rotatingPresignBackend) Name() string { return "s3" }

func (b *rotatingPresignBackend) List(context.Context, fstable.ListOpts) ([]fstable.Entry, string, error) {
	return nil, "", nil
}

func (b *rotatingPresignBackend) Stat(_ context.Context, key string) (fstable.Entry, error) {
	return fstable.Entry{Key: key, ModifiedAt: time.Now()}, nil
}

func (b *rotatingPresignBackend) Get(context.Context, string) (io.ReadCloser, fstable.Entry, error) {
	return nil, fstable.Entry{}, fstable.ErrUnsupported
}

func (b *rotatingPresignBackend) Put(context.Context, string, io.Reader, fstable.PutMeta) (fstable.Entry, error) {
	return fstable.Entry{}, fstable.ErrUnsupported
}

func (b *rotatingPresignBackend) Delete(context.Context, string) error { return nil }

func (b *rotatingPresignBackend) Presign(_ context.Context, key string, _ fstable.PresignOp, _ time.Duration) (string, error) {
	b.count++
	return fmt.Sprintf("https://signed.example/%d/%s", b.count, key), nil
}

func decodeFilesystemItems(t *testing.T, resp []byte) []map[string]any {
	t.Helper()
	var wrap struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(resp, &wrap); err != nil {
		t.Fatalf("response is not the expected wrapper shape: %v\n%s", err, resp)
	}
	return wrap.Items
}

func filesystemItemKeys(items []map[string]any) []string {
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item["key"].(string))
	}
	return keys
}

type recordingResponseCache struct {
	refs []RowRef
}

func (c *recordingResponseCache) Get(context.Context, string) ([]byte, bool, bool) {
	return nil, false, false
}

func (c *recordingResponseCache) Set(context.Context, string, []byte, []RowRef, time.Time) error {
	return nil
}

func (c *recordingResponseCache) InvalidateRows(_ context.Context, refs []RowRef) error {
	c.refs = append(c.refs, refs...)
	return nil
}

func TestBridge_LocalWritesInvalidateFilesystemCacheRefs(t *testing.T) {
	root := t.TempDir()
	cache := &recordingResponseCache{}
	gj := fsTestEngine(t, "public", []FilesystemConfig{
		{Name: "uploads", Backend: "local", Root: root},
	})
	gj.responseCache = cache

	if err := gj.loadFilesystemIntegration(); err != nil {
		t.Fatal(err)
	}

	backend, ok := gj.fsBackends["uploads"]
	if !ok {
		t.Fatal("expected uploads backend to be registered")
	}
	if _, err := backend.Put(
		context.Background(),
		"users/1/avatar.png",
		strings.NewReader("hi"),
		fstable.PutMeta{ContentType: "image/png"},
	); err != nil {
		t.Fatalf("Put: %v", err)
	}

	want := map[string]bool{
		RowRef{Source: CacheSourceFS, Scope: "uploads", Kind: CacheKindKey, ID: "users/1/avatar.png"}.DependencyKey(): false,
		RowRef{Source: CacheSourceFS, Scope: "uploads", Kind: CacheKindPrefix, ID: ""}.DependencyKey():                false,
		RowRef{Source: CacheSourceFS, Scope: "uploads", Kind: CacheKindPrefix, ID: "users/"}.DependencyKey():          false,
		RowRef{Source: CacheSourceFS, Scope: "uploads", Kind: CacheKindPrefix, ID: "users"}.DependencyKey():           false,
		RowRef{Source: CacheSourceFS, Scope: "uploads", Kind: CacheKindPrefix, ID: "users/1/"}.DependencyKey():        false,
		RowRef{Source: CacheSourceFS, Scope: "uploads", Kind: CacheKindPrefix, ID: "users/1"}.DependencyKey():         false,
	}
	for _, ref := range cache.refs {
		key := ref.DependencyKey()
		if _, ok := want[key]; ok {
			want[key] = true
		}
		if ref.Source == CacheSourceDB {
			t.Fatalf("filesystem write invalidated DB ref unexpectedly: %+v", ref)
		}
	}
	for key, found := range want {
		if !found {
			t.Fatalf("missing invalidated ref %q; got %+v", key, cache.refs)
		}
	}
}

func TestBridge_ReadOnlyFilesystemBlocksManagedWrites(t *testing.T) {
	root := t.TempDir()
	gj := fsTestEngine(t, "public", []FilesystemConfig{
		{Name: "uploads", Backend: "local", Root: root, ReadOnly: true},
	})

	if err := gj.loadFilesystemIntegration(); err != nil {
		t.Fatal(err)
	}

	backend, ok := gj.fsBackends["uploads"]
	if !ok {
		t.Fatal("expected uploads backend to be registered")
	}
	if _, err := backend.Put(
		context.Background(),
		"users/1/avatar.png",
		strings.NewReader("hi"),
		fstable.PutMeta{ContentType: "image/png"},
	); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("Put err = %v, want read-only", err)
	}
	if err := backend.Delete(context.Background(), "users/1/avatar.png"); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("Delete err = %v, want read-only", err)
	}
}

func TestParsePositiveInt(t *testing.T) {
	cases := map[string]bool{
		"":     false,
		"0":    false,
		"-1":   false,
		"abc":  false,
		"12":   true,
		"1000": true,
	}
	for in, ok := range cases {
		_, err := parsePositiveInt(in)
		if (err == nil) != ok {
			t.Errorf("parsePositiveInt(%q) ok=%v, want %v", in, err == nil, ok)
		}
	}
}
