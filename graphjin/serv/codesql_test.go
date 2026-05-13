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
			Databases: map[string]core.DatabaseConfig{
				"code": {Type: "codesql", Path: source},
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
	assertGraphJinTable(t, s, "code", "code_symbols")
	assertServiceCount(t, s, "code", `SELECT count(*) FROM code_symbols WHERE name = 'Handler'`, 1)
	if managed := s.managedDBs["code"]; !managed.watch {
		t.Fatalf("codesql watcher disabled in development, want enabled")
	}
}

func TestCodeSQLProductionEnablesLiveWatcher(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "main.go"), `package main

func Handler() {}
`)

	conf := &Config{
		Core: Core{
			DisableAllowList: true,
			Databases: map[string]core.DatabaseConfig{
				"code": {Type: "codesql", Path: source},
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

	assertServiceCount(t, s, "code", `SELECT count(*) FROM code_symbols WHERE name = 'Handler'`, 1)
	if managed := s.managedDBs["code"]; !managed.watch {
		t.Fatalf("codesql watcher disabled in production, want enabled")
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
			Databases: map[string]core.DatabaseConfig{
				"code": {Type: "codesql", Path: source, ReadOnly: true},
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

	assertServiceCount(t, s, "code", `SELECT count(*) FROM code_symbols WHERE name = 'Handler'`, 1)
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
			Databases: map[string]core.DatabaseConfig{
				"code": {Type: "codesql", Path: source, ReadOnly: true},
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
		code_locks(insert: {
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

func TestCodeSQLLegacyUsesDefaultCachePrefix(t *testing.T) {
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
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestService(s)

	runtime := s.runtimeCore.Databases[core.DefaultDBName]
	if runtime.Type != "sqlite" {
		t.Fatalf("runtime type = %q, want sqlite", runtime.Type)
	}
	if !strings.HasPrefix(filepath.Base(runtime.Path), "default-") {
		t.Fatalf("cache filename = %q, want default prefix", filepath.Base(runtime.Path))
	}
	assertGraphJinTable(t, s, core.DefaultDBName, "code_symbols")
	assertServiceCount(t, s, core.DefaultDBName, `SELECT count(*) FROM code_symbols WHERE name = 'Legacy'`, 1)
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
			Databases: map[string]core.DatabaseConfig{
				"code": {Type: "codesql", Path: source},
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
		code_symbols(where: { name: { eq: "LoadUser" } }) {
			name
			start_byte
			end_byte
			code
			code_context
			code_files { path hash }
		}
	}`
	res, err := s.gj.GraphQL(context.Background(), query, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var read struct {
		CodeSymbols []struct {
			Name        string `json:"name"`
			StartByte   int64  `json:"start_byte"`
			EndByte     int64  `json:"end_byte"`
			Code        string `json:"code"`
			CodeContext string `json:"code_context"`
			CodeFiles   struct {
				Path string `json:"path"`
				Hash string `json:"hash"`
			} `json:"code_files"`
		} `json:"code_symbols"`
	}
	if err := json.Unmarshal(res.Data, &read); err != nil {
		t.Fatalf("read response: %v\n%s", err, res.Data)
	}
	if len(read.CodeSymbols) != 1 {
		t.Fatalf("code_symbols len = %d, want 1; data=%s", len(read.CodeSymbols), res.Data)
	}
	sym := read.CodeSymbols[0]
	if !strings.Contains(sym.Code, "func LoadUser") || !strings.Contains(sym.CodeContext, "package main") {
		t.Fatalf("virtual code fields missing source: %#v", sym)
	}
	oldText := string([]byte(before)[sym.StartByte:sym.EndByte])
	if sym.Code != oldText {
		t.Fatalf("code field = %q, want file slice %q", sym.Code, oldText)
	}
	if sym.CodeFiles.Path != "main.go" || sym.CodeFiles.Hash == "" {
		t.Fatalf("code_files relation = %#v", sym.CodeFiles)
	}

	newText := strings.Replace(oldText, "return id", "return id + 1", 1)
	previewVars, err := json.Marshal(map[string]interface{}{
		"input": map[string]interface{}{
			"action": "preview",
			"title":  "increment LoadUser",
			"edits": []map[string]interface{}{{
				"path":          "main.go",
				"expected_hash": sym.CodeFiles.Hash,
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
		code_change_sets(insert: $input) { id status diff errors }
	}`
	res, err = s.gj.GraphQL(context.Background(), preview, previewVars, nil)
	if err != nil {
		t.Fatal(err)
	}
	var previewOut struct {
		CodeChangeSets struct {
			ID     int64    `json:"id"`
			Status string   `json:"status"`
			Diff   string   `json:"diff"`
			Errors []string `json:"errors"`
		} `json:"code_change_sets"`
	}
	if err := json.Unmarshal(res.Data, &previewOut); err != nil {
		t.Fatalf("preview response: %v\n%s", err, res.Data)
	}
	if previewOut.CodeChangeSets.ID == 0 || previewOut.CodeChangeSets.Status != "previewed" || len(previewOut.CodeChangeSets.Errors) != 0 {
		t.Fatalf("preview output = %#v data=%s", previewOut.CodeChangeSets, res.Data)
	}
	if !strings.Contains(previewOut.CodeChangeSets.Diff, "+func LoadUser") {
		t.Fatalf("preview diff missing new source: %s", previewOut.CodeChangeSets.Diff)
	}
	if got := readTestFile(t, filepath.Join(source, "main.go")); got != before {
		t.Fatalf("preview changed source file:\n%s", got)
	}

	apply := fmt.Sprintf(`mutation {
		code_change_sets(id: %d, update: { id: %d, action: "apply" }) {
			id
			status
			files_changed
			files_reindexed
			errors
		}
	}`, previewOut.CodeChangeSets.ID, previewOut.CodeChangeSets.ID)
	res, err = s.gj.GraphQL(context.Background(), apply, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var applyOut struct {
		CodeChangeSets struct {
			Status         string   `json:"status"`
			FilesChanged   []string `json:"files_changed"`
			FilesReindexed []string `json:"files_reindexed"`
			Errors         []string `json:"errors"`
		} `json:"code_change_sets"`
	}
	if err := json.Unmarshal(res.Data, &applyOut); err != nil {
		t.Fatalf("apply response: %v\n%s", err, res.Data)
	}
	if applyOut.CodeChangeSets.Status != "applied" || len(applyOut.CodeChangeSets.Errors) != 0 {
		t.Fatalf("apply output = %#v data=%s", applyOut.CodeChangeSets, res.Data)
	}
	if len(applyOut.CodeChangeSets.FilesChanged) != 1 || applyOut.CodeChangeSets.FilesChanged[0] != "main.go" {
		t.Fatalf("files_changed = %#v", applyOut.CodeChangeSets.FilesChanged)
	}
	if got := readTestFile(t, filepath.Join(source, "main.go")); !strings.Contains(got, "return id + 1") {
		t.Fatalf("apply did not update source:\n%s", got)
	}
	assertServiceCount(t, s, "code", `SELECT count(*) FROM code_symbols WHERE name = 'LoadUser'`, 1)
	res, err = s.gj.GraphQL(context.Background(), query, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var reread struct {
		CodeSymbols []struct {
			Code      string `json:"code"`
			CodeFiles struct {
				Hash string `json:"hash"`
			} `json:"code_files"`
		} `json:"code_symbols"`
	}
	if err := json.Unmarshal(res.Data, &reread); err != nil {
		t.Fatalf("reread response: %v\n%s", err, res.Data)
	}
	if len(reread.CodeSymbols) != 1 {
		t.Fatalf("reread symbols = %d, want 1", len(reread.CodeSymbols))
	}
	if !strings.Contains(reread.CodeSymbols[0].Code, "return id + 1") {
		t.Fatalf("expected refreshed code after apply, got %q", reread.CodeSymbols[0].Code)
	}
	if reread.CodeSymbols[0].CodeFiles.Hash == sym.CodeFiles.Hash {
		t.Fatalf("expected refreshed hash after apply, still %q", reread.CodeSymbols[0].CodeFiles.Hash)
	}

	lockMutation := `mutation {
		code_locks(insert: {
			action: "acquire",
			path: "main.go",
			owner: "test",
			ranges: [{ start_byte: 0, end_byte: 20 }]
		}) { id status lease_token path }
	}`
	res, err = s.gj.GraphQL(context.Background(), lockMutation, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var lockOut struct {
		CodeLocks struct {
			ID         int64  `json:"id"`
			Status     string `json:"status"`
			LeaseToken string `json:"lease_token"`
			Path       string `json:"path"`
		} `json:"code_locks"`
	}
	if err := json.Unmarshal(res.Data, &lockOut); err != nil {
		t.Fatalf("lock response: %v\n%s", err, res.Data)
	}
	if lockOut.CodeLocks.ID == 0 || lockOut.CodeLocks.Status != "active" || lockOut.CodeLocks.LeaseToken == "" || lockOut.CodeLocks.Path != "main.go" {
		t.Fatalf("lock output = %#v data=%s", lockOut.CodeLocks, res.Data)
	}
	release := fmt.Sprintf(`mutation {
		code_locks(id: %d, update: { id: %d, action: "release", lease_token: %q }) {
			id status path
		}
	}`, lockOut.CodeLocks.ID, lockOut.CodeLocks.ID, lockOut.CodeLocks.LeaseToken)
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
			Databases: map[string]core.DatabaseConfig{
				"code": {Type: "codesql", Path: source},
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
		code_files(order_by: { path: asc }) { path hash }
	}`, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var filesOut struct {
		CodeFiles []struct {
			Path string `json:"path"`
			Hash string `json:"hash"`
		} `json:"code_files"`
	}
	if err := json.Unmarshal(res.Data, &filesOut); err != nil {
		t.Fatalf("code_files response: %v\n%s", err, res.Data)
	}
	for _, file := range filesOut.CodeFiles {
		hashes[file.Path] = file.Hash
	}
	if hashes["delete_me.go"] == "" || hashes["move_me.go"] == "" {
		t.Fatalf("missing source hashes: %#v", hashes)
	}

	vars, err := json.Marshal(map[string]interface{}{
		"input": map[string]interface{}{
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
		code_change_sets(insert: $input) {
			id
			status
			diff
			errors
		}
	}`, vars, nil)
	if err != nil {
		t.Fatal(err)
	}
	var previewOut struct {
		CodeChangeSets struct {
			ID     int64    `json:"id"`
			Status string   `json:"status"`
			Diff   string   `json:"diff"`
			Errors []string `json:"errors"`
		} `json:"code_change_sets"`
	}
	if err := json.Unmarshal(res.Data, &previewOut); err != nil {
		t.Fatalf("preview response: %v\n%s", err, res.Data)
	}
	if previewOut.CodeChangeSets.Status != "previewed" || len(previewOut.CodeChangeSets.Errors) != 0 {
		t.Fatalf("preview output = %#v data=%s", previewOut.CodeChangeSets, res.Data)
	}
	for _, part := range []string{"+++ b/created.go", "--- a/delete_me.go", "+++ b/pkg/moved.go"} {
		if !strings.Contains(previewOut.CodeChangeSets.Diff, part) {
			t.Fatalf("preview diff missing %q:\n%s", part, previewOut.CodeChangeSets.Diff)
		}
	}

	apply := fmt.Sprintf(`mutation {
		code_change_sets(id: %d, update: { id: %d, action: "apply" }) {
			status
			files_changed
			files_reindexed
			errors
		}
	}`, previewOut.CodeChangeSets.ID, previewOut.CodeChangeSets.ID)
	res, err = s.gj.GraphQL(context.Background(), apply, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var applyOut struct {
		CodeChangeSets struct {
			Status         string   `json:"status"`
			FilesChanged   []string `json:"files_changed"`
			FilesReindexed []string `json:"files_reindexed"`
			Errors         []string `json:"errors"`
		} `json:"code_change_sets"`
	}
	if err := json.Unmarshal(res.Data, &applyOut); err != nil {
		t.Fatalf("apply response: %v\n%s", err, res.Data)
	}
	if applyOut.CodeChangeSets.Status != "applied" || len(applyOut.CodeChangeSets.Errors) != 0 {
		t.Fatalf("apply output = %#v data=%s", applyOut.CodeChangeSets, res.Data)
	}
	wantChanged := []string{"created.go", "delete_me.go", "move_me.go", "pkg/moved.go"}
	if strings.Join(applyOut.CodeChangeSets.FilesChanged, ",") != strings.Join(wantChanged, ",") {
		t.Fatalf("files_changed = %#v, want %#v", applyOut.CodeChangeSets.FilesChanged, wantChanged)
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
	assertServiceCount(t, s, "code", `SELECT count(*) FROM code_files WHERE path = 'created.go'`, 1)
	assertServiceCount(t, s, "code", `SELECT count(*) FROM code_files WHERE path = 'delete_me.go'`, 0)
	assertServiceCount(t, s, "code", `SELECT count(*) FROM code_files WHERE path = 'move_me.go'`, 0)
	assertServiceCount(t, s, "code", `SELECT count(*) FROM code_files WHERE path = 'pkg/moved.go'`, 1)
}

func TestCodeSQLDerivedTableMutationBlocked(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "main.go"), `package main

func Blocked() {}
`)

	conf := &Config{
		Core: Core{
			DisableAllowList: true,
			Databases: map[string]core.DatabaseConfig{
				"code": {Type: "codesql", Path: source},
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
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("direct derived mutation err = %v, want read-only block", err)
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
			Databases: map[string]core.DatabaseConfig{
				"code": {Type: "codesql", Path: source},
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
		code_symbols(where: { name: { eq: "WatchMe" } }) {
			code
			code_files { hash }
		}
	}`
	res, err := s.gj.GraphQL(context.Background(), query, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var initial struct {
		CodeSymbols []struct {
			Code      string `json:"code"`
			CodeFiles struct {
				Hash string `json:"hash"`
			} `json:"code_files"`
		} `json:"code_symbols"`
	}
	if err := json.Unmarshal(res.Data, &initial); err != nil {
		t.Fatalf("initial response: %v\n%s", err, res.Data)
	}
	if len(initial.CodeSymbols) != 1 {
		t.Fatalf("initial symbols = %d, want 1", len(initial.CodeSymbols))
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
			CodeSymbols []struct {
				Code      string `json:"code"`
				CodeFiles struct {
					Hash string `json:"hash"`
				} `json:"code_files"`
			} `json:"code_symbols"`
		}
		if err := json.Unmarshal(res.Data, &current); err != nil {
			t.Fatalf("current response: %v\n%s", err, res.Data)
		}
		for _, symbol := range current.CodeSymbols {
			if strings.Contains(symbol.Code, "return 2") &&
				symbol.CodeFiles.Hash != initial.CodeSymbols[0].CodeFiles.Hash {
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
