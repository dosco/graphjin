package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
