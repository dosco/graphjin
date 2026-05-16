package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

func TestCodeSQLMultiDBInitializesManagedSQLiteRuntime(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "main.go"), `package main

func Handler() {}
`)

	conf := &Config{
		Core: Core{
			Sources: []core.SourceConfig{
				{Name: "code", Kind: "code", Path: source},
			},
		},
		Serv: Serv{
			ConfigPath: filepath.Join(t.TempDir(), "config"),
			MCP:        MCPConfig{Disable: true},
		},
	}

	s, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestService(s)

	if got := s.conf.Core.Databases["code"].Type; got != "codesql" {
		t.Fatalf("logical config type = %q, want codesql", got)
	}
	runtime := s.runtimeCore.Databases["code"]
	if runtime.Type != "sqlite" {
		t.Fatalf("runtime type = %q, want sqlite", runtime.Type)
	}
	if !runtime.ReadOnly {
		t.Fatalf("runtime read_only = false, want true")
	}
	if runtime.AnalyticsMode == nil || !*runtime.AnalyticsMode {
		t.Fatalf("runtime analytics_mode = %v, want true", runtime.AnalyticsMode)
	}
	if !strings.HasPrefix(filepath.Base(runtime.Path), "code-") {
		t.Fatalf("cache filename = %q, want database-name prefix", filepath.Base(runtime.Path))
	}
	assertGraphJinTable(t, s, "code", "gj_code")
	assertServiceCount(t, s, "code", `SELECT count(*) FROM gj_code WHERE kind = 'symbol' AND name = 'Handler'`, 1)
	if managed := s.managedDBs["code"]; !managed.watch {
		t.Fatalf("codesql watcher disabled in development, want enabled")
	}
}

func TestCodeSQLProductionDisablesLiveWatcherByDefault(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "main.go"), `package main

func Handler() {}
`)

	conf := &Config{
		Core: Core{
			DisableAllowList: true,
			Sources: []core.SourceConfig{
				{Name: "code", Kind: "code", Path: source},
			},
		},
		Serv: Serv{
			Production: true,
			ConfigPath: filepath.Join(t.TempDir(), "config"),
			MCP:        MCPConfig{Disable: true},
		},
	}

	s, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestService(s)

	assertServiceCount(t, s, "code", `SELECT count(*) FROM gj_code WHERE kind = 'symbol' AND name = 'Handler'`, 1)
	if managed := s.managedDBs["code"]; managed.watch {
		t.Fatalf("codesql watcher enabled in production by default, want disabled")
	}
}

func TestCodeSQLProductionEnablesLiveWatcherWithCapability(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "main.go"), `package main

func Handler() {}
`)

	conf := &Config{
		Core: Core{
			DisableAllowList: true,
			Sources: []core.SourceConfig{
				{Name: "code", Kind: "code", Path: source, Capabilities: map[string]bool{"code.watch": true}},
			},
		},
		Serv: Serv{
			Production: true,
			ConfigPath: filepath.Join(t.TempDir(), "config"),
			MCP:        MCPConfig{Disable: true},
		},
	}

	s, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestService(s)

	assertServiceCount(t, s, "code", `SELECT count(*) FROM gj_code WHERE kind = 'symbol' AND name = 'Handler'`, 1)
	if managed := s.managedDBs["code"]; !managed.watch {
		t.Fatalf("codesql watcher disabled with code.watch capability, want enabled")
	}
}

func TestCodeSQLReadOnlyDisablesLiveWatcher(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "main.go"), `package main

func Handler() {}
`)

	conf := &Config{
		Core: Core{
			DisableAllowList: true,
			Sources: []core.SourceConfig{
				{Name: "code", Kind: "code", Path: source, ReadOnly: true},
			},
		},
		Serv: Serv{
			Production: true,
			ConfigPath: filepath.Join(t.TempDir(), "config"),
			MCP:        MCPConfig{Disable: true},
		},
	}

	s, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestService(s)

	assertServiceCount(t, s, "code", `SELECT count(*) FROM gj_code WHERE kind = 'symbol' AND name = 'Handler'`, 1)
	if managed := s.managedDBs["code"]; managed.watch {
		t.Fatalf("codesql watcher enabled for read-only database, want disabled")
	}
}

func TestCodeSQLReadOnlyBlocksSourceMutation(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "main.go"), `package main

func Handler() {}
`)

	conf := &Config{
		Core: Core{
			DisableAllowList: true,
			Sources: []core.SourceConfig{
				{Name: "code", Kind: "code", Path: source, ReadOnly: true},
			},
		},
		Serv: Serv{
			ConfigPath: filepath.Join(t.TempDir(), "config"),
			MCP:        MCPConfig{Disable: true},
		},
	}

	s, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestService(s)

	_, err = s.gj.GraphQL(context.Background(), `mutation {
		gj_code(insert: {
			kind: "lock",
			action: "acquire",
			path: "main.go",
			owner: "test",
			ranges: [{ start_byte: 0, end_byte: 20 }]
		}) { id status }
	}`, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("source mutation err = %v, want read-only block", err)
	}
}

func TestCodeSQLLegacyConfigRejected(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "main.go"), `package main

func Legacy() {}
`)

	conf := &Config{
		Serv: Serv{
			ConfigPath: filepath.Join(t.TempDir(), "config"),
			DB:         Database{Type: "codesql", Path: source},
			MCP:        MCPConfig{Disable: true},
		},
	}

	s, err := newGraphJinService(conf, nil)
	if err == nil {
		closeTestService(s)
		t.Fatal("expected legacy CodeSQL database config to be rejected")
	}
	if !strings.Contains(err.Error(), "kind: code") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCodeSQLGraphQLSourceMutationsPreviewApplyAndLocks(t *testing.T) {
	source := t.TempDir()
	const before = `package main

func LoadUser(id int64) int64 {
    return id
}
`
	writeTestFile(t, filepath.Join(source, "main.go"), before)

	conf := &Config{
		Core: Core{
			DisableAllowList: true,
			Sources: []core.SourceConfig{
				{Name: "code", Kind: "code", Path: source},
			},
		},
		Serv: Serv{
			ConfigPath: filepath.Join(t.TempDir(), "config"),
			MCP:        MCPConfig{Disable: true},
		},
	}

	s, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestService(s)

	query := `query GetLoadUserSource {
		gj_code(where: { kind: { eq: "symbol" }, name: { eq: "LoadUser" } }) {
			name
			start_byte
			end_byte
			code
			code_context
			path
			hash
		}
	}`
	res, err := s.gj.GraphQL(context.Background(), query, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var read struct {
		GJCode []struct {
			Name        string `json:"name"`
			StartByte   int64  `json:"start_byte"`
			EndByte     int64  `json:"end_byte"`
			Code        string `json:"code"`
			CodeContext string `json:"code_context"`
			Path        string `json:"path"`
			Hash        string `json:"hash"`
		} `json:"gj_code"`
	}
	if err := json.Unmarshal(res.Data, &read); err != nil {
		t.Fatalf("read response: %v\n%s", err, res.Data)
	}
	if len(read.GJCode) != 1 {
		t.Fatalf("gj_code symbols len = %d, want 1; data=%s", len(read.GJCode), res.Data)
	}
	sym := read.GJCode[0]
	if !strings.Contains(sym.Code, "func LoadUser") || !strings.Contains(sym.CodeContext, "package main") {
		t.Fatalf("virtual code fields missing source: %#v", sym)
	}
	oldText := string([]byte(before)[sym.StartByte:sym.EndByte])
	if sym.Code != oldText {
		t.Fatalf("code field = %q, want file slice %q", sym.Code, oldText)
	}
	if sym.Path != "main.go" || sym.Hash == "" {
		t.Fatalf("gj_code symbol source fields = %#v", sym)
	}

	newText := strings.Replace(oldText, "return id", "return id + 1", 1)
	previewVars, err := json.Marshal(map[string]interface{}{
		"input": map[string]interface{}{
			"kind":   "change_set",
			"action": "preview",
			"title":  "increment LoadUser",
			"edits": []map[string]interface{}{{
				"path":          "main.go",
				"expected_hash": sym.Hash,
				"replacements": []map[string]interface{}{{
					"start_byte": sym.StartByte,
					"end_byte":   sym.EndByte,
					"old_text":   oldText,
					"new_text":   newText,
				}},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	preview := `mutation {
		gj_code(insert: $input) { id kind status diff errors_json }
	}`
	res, err = s.gj.GraphQL(context.Background(), preview, previewVars, nil)
	if err != nil {
		t.Fatal(err)
	}
	var previewOut struct {
		GJCode struct {
			ID     int64    `json:"id"`
			Kind   string   `json:"kind"`
			Status string   `json:"status"`
			Diff   string   `json:"diff"`
			Errors []string `json:"errors_json"`
		} `json:"gj_code"`
	}
	if err := json.Unmarshal(res.Data, &previewOut); err != nil {
		t.Fatalf("preview response: %v\n%s", err, res.Data)
	}
	if previewOut.GJCode.ID == 0 || previewOut.GJCode.Kind != "change_set" || previewOut.GJCode.Status != "previewed" || len(previewOut.GJCode.Errors) != 0 {
		t.Fatalf("preview output = %#v data=%s", previewOut.GJCode, res.Data)
	}
	if !strings.Contains(previewOut.GJCode.Diff, "+func LoadUser") {
		t.Fatalf("preview diff missing new source: %s", previewOut.GJCode.Diff)
	}
	if got := readTestFile(t, filepath.Join(source, "main.go")); got != before {
		t.Fatalf("preview changed source file:\n%s", got)
	}

	apply := fmt.Sprintf(`mutation {
		gj_code(id: "change_set:%d", update: { kind: "change_set", id: %d, action: "apply" }) {
			id
			kind
			status
			files_changed
			files_reindexed
			errors_json
		}
	}`, previewOut.GJCode.ID, previewOut.GJCode.ID)
	res, err = s.gj.GraphQL(context.Background(), apply, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var applyOut struct {
		GJCode struct {
			Kind           string   `json:"kind"`
			Status         string   `json:"status"`
			FilesChanged   []string `json:"files_changed"`
			FilesReindexed []string `json:"files_reindexed"`
			Errors         []string `json:"errors_json"`
		} `json:"gj_code"`
	}
	if err := json.Unmarshal(res.Data, &applyOut); err != nil {
		t.Fatalf("apply response: %v\n%s", err, res.Data)
	}
	if applyOut.GJCode.Kind != "change_set" || applyOut.GJCode.Status != "applied" || len(applyOut.GJCode.Errors) != 0 {
		t.Fatalf("apply output = %#v data=%s", applyOut.GJCode, res.Data)
	}
	if len(applyOut.GJCode.FilesChanged) != 1 || applyOut.GJCode.FilesChanged[0] != "main.go" {
		t.Fatalf("files_changed = %#v", applyOut.GJCode.FilesChanged)
	}
	if got := readTestFile(t, filepath.Join(source, "main.go")); !strings.Contains(got, "return id + 1") {
		t.Fatalf("apply did not update source:\n%s", got)
	}
	assertServiceCount(t, s, "code", `SELECT count(*) FROM gj_code WHERE kind = 'symbol' AND name = 'LoadUser'`, 1)
	res, err = s.gj.GraphQL(context.Background(), query, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var reread struct {
		GJCode []struct {
			Code string `json:"code"`
			Hash string `json:"hash"`
		} `json:"gj_code"`
	}
	if err := json.Unmarshal(res.Data, &reread); err != nil {
		t.Fatalf("reread response: %v\n%s", err, res.Data)
	}
	if len(reread.GJCode) != 1 {
		t.Fatalf("reread symbols = %d, want 1", len(reread.GJCode))
	}
	if !strings.Contains(reread.GJCode[0].Code, "return id + 1") {
		t.Fatalf("expected refreshed code after apply, got %q", reread.GJCode[0].Code)
	}
	if reread.GJCode[0].Hash == sym.Hash {
		t.Fatalf("expected refreshed hash after apply, still %q", reread.GJCode[0].Hash)
	}

	lockMutation := `mutation {
		gj_code(insert: {
			kind: "lock",
			action: "acquire",
			path: "main.go",
			owner: "test",
			ranges: [{ start_byte: 0, end_byte: 20 }]
		}) { id kind status lease_token path }
	}`
	res, err = s.gj.GraphQL(context.Background(), lockMutation, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var lockOut struct {
		GJCode struct {
			ID         int64  `json:"id"`
			Kind       string `json:"kind"`
			Status     string `json:"status"`
			LeaseToken string `json:"lease_token"`
			Path       string `json:"path"`
		} `json:"gj_code"`
	}
	if err := json.Unmarshal(res.Data, &lockOut); err != nil {
		t.Fatalf("lock response: %v\n%s", err, res.Data)
	}
	if lockOut.GJCode.ID == 0 || lockOut.GJCode.Kind != "lock" || lockOut.GJCode.Status != "active" || lockOut.GJCode.LeaseToken == "" || lockOut.GJCode.Path != "main.go" {
		t.Fatalf("lock output = %#v data=%s", lockOut.GJCode, res.Data)
	}
	release := fmt.Sprintf(`mutation {
		gj_code(id: "lock:%d", update: { kind: "lock", id: %d, action: "release", lease_token: %q }) {
			id kind status path
		}
	}`, lockOut.GJCode.ID, lockOut.GJCode.ID, lockOut.GJCode.LeaseToken)
	res, err = s.gj.GraphQL(context.Background(), release, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Data), `"status":"released"`) {
		t.Fatalf("release response = %s", res.Data)
	}
}

func TestCodeSQLGraphQLFileLifecycleMutations(t *testing.T) {
	source := t.TempDir()
	const deleteSrc = "package main\n\nfunc DeleteMe() {}\n"
	const moveSrc = "package main\n\nfunc MoveMe() {}\n"
	writeTestFile(t, filepath.Join(source, "delete_me.go"), deleteSrc)
	writeTestFile(t, filepath.Join(source, "move_me.go"), moveSrc)

	conf := &Config{
		Core: Core{
			DisableAllowList: true,
			Sources: []core.SourceConfig{
				{Name: "code", Kind: "code", Path: source},
			},
		},
		Serv: Serv{
			ConfigPath: filepath.Join(t.TempDir(), "config"),
			MCP:        MCPConfig{Disable: true},
		},
	}

	s, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestService(s)

	hashes := map[string]string{}
	res, err := s.gj.GraphQL(context.Background(), `query {
		gj_code(where: { kind: { eq: "file" } }, order_by: { path: asc }) { path hash }
	}`, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var filesOut struct {
		GJCode []struct {
			Path string `json:"path"`
			Hash string `json:"hash"`
		} `json:"gj_code"`
	}
	if err := json.Unmarshal(res.Data, &filesOut); err != nil {
		t.Fatalf("gj_code file response: %v\n%s", err, res.Data)
	}
	for _, file := range filesOut.GJCode {
		hashes[file.Path] = file.Hash
	}
	if hashes["delete_me.go"] == "" || hashes["move_me.go"] == "" {
		t.Fatalf("missing source hashes: %#v", hashes)
	}

	vars, err := json.Marshal(map[string]interface{}{
		"input": map[string]interface{}{
			"kind":   "change_set",
			"action": "preview",
			"title":  "file lifecycle batch",
			"edits": []map[string]interface{}{
				{
					"op":      "create",
					"path":    "created.go",
					"content": "package main\n\nfunc Created() {}\n",
				},
				{
					"op":            "delete",
					"path":          "delete_me.go",
					"expected_hash": hashes["delete_me.go"],
				},
				{
					"op":            "rename",
					"path":          "move_me.go",
					"new_path":      "pkg/moved.go",
					"expected_hash": hashes["move_me.go"],
					"mkdirs":        true,
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err = s.gj.GraphQL(context.Background(), `mutation {
		gj_code(insert: $input) {
			id
			kind
			status
			diff
			errors_json
		}
	}`, vars, nil)
	if err != nil {
		t.Fatal(err)
	}
	var previewOut struct {
		GJCode struct {
			ID     int64    `json:"id"`
			Kind   string   `json:"kind"`
			Status string   `json:"status"`
			Diff   string   `json:"diff"`
			Errors []string `json:"errors_json"`
		} `json:"gj_code"`
	}
	if err := json.Unmarshal(res.Data, &previewOut); err != nil {
		t.Fatalf("preview response: %v\n%s", err, res.Data)
	}
	if previewOut.GJCode.Kind != "change_set" || previewOut.GJCode.Status != "previewed" || len(previewOut.GJCode.Errors) != 0 {
		t.Fatalf("preview output = %#v data=%s", previewOut.GJCode, res.Data)
	}
	for _, part := range []string{"+++ b/created.go", "--- a/delete_me.go", "+++ b/pkg/moved.go"} {
		if !strings.Contains(previewOut.GJCode.Diff, part) {
			t.Fatalf("preview diff missing %q:\n%s", part, previewOut.GJCode.Diff)
		}
	}

	apply := fmt.Sprintf(`mutation {
		gj_code(id: "change_set:%d", update: { kind: "change_set", id: %d, action: "apply" }) {
			kind
			status
			files_changed
			files_reindexed
			errors_json
		}
	}`, previewOut.GJCode.ID, previewOut.GJCode.ID)
	res, err = s.gj.GraphQL(context.Background(), apply, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var applyOut struct {
		GJCode struct {
			Kind           string   `json:"kind"`
			Status         string   `json:"status"`
			FilesChanged   []string `json:"files_changed"`
			FilesReindexed []string `json:"files_reindexed"`
			Errors         []string `json:"errors_json"`
		} `json:"gj_code"`
	}
	if err := json.Unmarshal(res.Data, &applyOut); err != nil {
		t.Fatalf("apply response: %v\n%s", err, res.Data)
	}
	if applyOut.GJCode.Kind != "change_set" || applyOut.GJCode.Status != "applied" || len(applyOut.GJCode.Errors) != 0 {
		t.Fatalf("apply output = %#v data=%s", applyOut.GJCode, res.Data)
	}
	wantChanged := []string{"created.go", "delete_me.go", "move_me.go", "pkg/moved.go"}
	if strings.Join(applyOut.GJCode.FilesChanged, ",") != strings.Join(wantChanged, ",") {
		t.Fatalf("files_changed = %#v, want %#v", applyOut.GJCode.FilesChanged, wantChanged)
	}
	if _, err := os.Stat(filepath.Join(source, "delete_me.go")); !os.IsNotExist(err) {
		t.Fatalf("delete_me.go stat err = %v, want not exists", err)
	}
	if got := readTestFile(t, filepath.Join(source, "created.go")); !strings.Contains(got, "func Created") {
		t.Fatalf("created.go content = %q", got)
	}
	if got := readTestFile(t, filepath.Join(source, "pkg", "moved.go")); got != moveSrc {
		t.Fatalf("moved.go content = %q", got)
	}
	assertServiceCount(t, s, "code", `SELECT count(*) FROM gj_code WHERE kind = 'file' AND path = 'created.go'`, 1)
	assertServiceCount(t, s, "code", `SELECT count(*) FROM gj_code WHERE kind = 'file' AND path = 'delete_me.go'`, 0)
	assertServiceCount(t, s, "code", `SELECT count(*) FROM gj_code WHERE kind = 'file' AND path = 'move_me.go'`, 0)
	assertServiceCount(t, s, "code", `SELECT count(*) FROM gj_code WHERE kind = 'file' AND path = 'pkg/moved.go'`, 1)
}

func TestCodeSQLRawTableMutationRootsUnavailable(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "main.go"), `package main

func Blocked() {}
`)

	conf := &Config{
		Core: Core{
			DisableAllowList: true,
			Sources: []core.SourceConfig{
				{Name: "code", Kind: "code", Path: source},
			},
		},
		Serv: Serv{
			ConfigPath: filepath.Join(t.TempDir(), "config"),
			MCP:        MCPConfig{Disable: true},
		},
	}
	s, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestService(s)

	_, err = s.gj.GraphQL(context.Background(), `mutation {
		code_files(insert: { path: "x.go", abs_path: "x.go", language: "go", hash: "x", size: 1, mtime_unix: 1, indexed_at: "now" }) { id }
	}`, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("direct derived mutation err = %v, want blocked raw CodeSQL root", err)
	}
}

func TestCodeSQLWatcherInvalidatesResponseCache(t *testing.T) {
	source := t.TempDir()
	const before = `package main

func WatchMe() int {
    return 1
}
`
	writeTestFile(t, filepath.Join(source, "main.go"), before)

	conf := &Config{
		Core: Core{
			DisableAllowList: true,
			Sources: []core.SourceConfig{
				{Name: "code", Kind: "code", Path: source},
			},
		},
		Serv: Serv{
			ConfigPath: filepath.Join(t.TempDir(), "config"),
			MCP:        MCPConfig{Disable: true},
		},
	}

	s, err := newGraphJinService(conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestService(s)

	query := `query GetWatchMe {
		gj_code(where: { kind: { eq: "symbol" }, name: { eq: "WatchMe" } }) {
			code
			hash
		}
	}`
	res, err := s.gj.GraphQL(context.Background(), query, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var initial struct {
		GJCode []struct {
			Code string `json:"code"`
			Hash string `json:"hash"`
		} `json:"gj_code"`
	}
	if err := json.Unmarshal(res.Data, &initial); err != nil {
		t.Fatalf("initial response: %v\n%s", err, res.Data)
	}
	if len(initial.GJCode) != 1 {
		t.Fatalf("initial symbols = %d, want 1", len(initial.GJCode))
	}

	after := strings.Replace(before, "return 1", "return 2", 1)
	writeTestFile(t, filepath.Join(source, "main.go"), after)

	deadline := time.Now().Add(5 * time.Second)
	for {
		res, err = s.gj.GraphQL(context.Background(), query, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		var current struct {
			GJCode []struct {
				Code string `json:"code"`
				Hash string `json:"hash"`
			} `json:"gj_code"`
		}
		if err := json.Unmarshal(res.Data, &current); err != nil {
			t.Fatalf("current response: %v\n%s", err, res.Data)
		}
		for _, symbol := range current.GJCode {
			if strings.Contains(symbol.Code, "return 2") &&
				symbol.Hash != initial.GJCode[0].Hash {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("watcher did not invalidate cached response in time; last=%s", res.Data)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func writeTestFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertServiceCount(t *testing.T, s *graphjinService, dbName, query string, want int) {
	t.Helper()
	db := s.dbs[dbName]
	if db == nil {
		t.Fatalf("database %q not connected", dbName)
	}
	var got int
	if err := db.QueryRow(query).Scan(&got); err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
}

func assertGraphJinTable(t *testing.T, s *graphjinService, dbName, tableName string) {
	t.Helper()
	if s.gj == nil || !s.gj.SchemaReady() {
		t.Fatalf("GraphJin schema is not ready")
	}
	for _, table := range s.gj.GetTablesForDatabase(dbName) {
		if table.Name == tableName {
			return
		}
	}
	t.Fatalf("GraphJin did not discover table %q", tableName)
}

func closeTestService(s *graphjinService) {
	if s.gj != nil {
		s.gj.Close()
	}
	closed := s.closeManagedDBs(nil)
	for name, db := range s.dbs {
		if _, ok := closed[name]; ok {
			continue
		}
		db.Close() //nolint:errcheck
	}
}
