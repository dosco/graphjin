package localtool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	ax "github.com/ax-llm/ax/packages/go"
	"github.com/open-neko/openneko/apps/ax-harness/internal/agent"
)

func TestFileFreshnessAndContainment(t *testing.T) {
	workspace := t.TempDir()
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "secret.txt"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := OpenFiles(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cap := f.Capabilities()
	read := func(path string) (string, error) {
		input, _ := json.Marshal(map[string]string{"path": path})
		data, err := cap[0].Call(context.Background(), input)
		var output struct{ Version string }
		_ = json.Unmarshal(data, &output)
		return output.Version, err
	}
	edit := func(old, new string) error {
		input, _ := json.Marshal(map[string]string{"path": "note.txt", "old_string": old, "new_string": new})
		_, err := cap[1].Call(context.Background(), input)
		return err
	}
	if err := edit("first", "bad"); err == nil {
		t.Fatal("edit before read admitted")
	}
	version, err := read("note.txt")
	if err != nil || len(version) != 64 {
		t.Fatalf("read version=%q err=%v", version, err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := edit("external", "bad"); err == nil {
		t.Fatal("stale edit admitted")
	}
	if _, err = read("note.txt"); err != nil || edit("external", "second") != nil || edit("second", "third") != nil {
		t.Fatalf("fresh edits failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(workspace, "note.txt"))
	if err != nil || string(data) != "third" {
		t.Fatalf("edited content=%q err=%v", data, err)
	}
	if _, err := read("../" + filepath.Base(other) + "/secret.txt"); err == nil {
		t.Fatal("cross-run traversal admitted")
	}
	if err := os.Symlink(filepath.Join(other, "secret.txt"), filepath.Join(workspace, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := read("link.txt"); err == nil {
		t.Fatal("symlink escape admitted")
	}
}

func TestReadCanOverlapReadButEditWaits(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := OpenFiles(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	read := f.Capabilities()[0].Call
	edit := f.Capabilities()[1].Call
	input := json.RawMessage(`{"path":"note.txt"}`)
	if _, err := read(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	editInput := json.RawMessage(`{"path":"note.txt","old_string":"first","new_string":"second"}`)
	f.gate.RLock() // Simulates an admitted read still in progress.
	if _, err := read(context.Background(), input); err != nil {
		t.Fatalf("second read did not overlap: %v", err)
	}
	finished := make(chan error, 1)
	go func() { _, err := edit(context.Background(), editInput); finished <- err }()
	select {
	case err := <-finished:
		t.Fatalf("edit overlapped read: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	f.gate.RUnlock()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("edit stayed blocked")
	}
}

func TestFileWriteCreatesOnlyInsideWorkspace(t *testing.T) {
	workspace, other := t.TempDir(), t.TempDir()
	f, err := OpenFiles(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	write := f.Capabilities()[2].Call
	input := json.RawMessage(`{"path":"result.txt","content":"ready"}`)
	result, err := write(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct{ Path, Version string }
	if json.Unmarshal(result, &receipt) != nil || receipt.Path != "result.txt" || receipt.Version != digest([]byte("ready")) {
		t.Fatalf("invalid write receipt: %s", result)
	}
	if _, err := write(context.Background(), json.RawMessage(`{"path":"result.txt","content":"replaced"}`)); err == nil {
		t.Fatal("write replaced an existing file")
	}
	if data, err := os.ReadFile(filepath.Join(workspace, "result.txt")); err != nil || string(data) != "ready" {
		t.Fatalf("existing file changed: %q %v", data, err)
	}
	if _, err := write(context.Background(), json.RawMessage(`{"path":"../escape.txt","content":"no"}`)); err == nil {
		t.Fatal("write escaped workspace")
	}
	if err := os.Symlink(other, filepath.Join(workspace, "outside")); err != nil {
		t.Fatal(err)
	}
	if _, err := write(context.Background(), json.RawMessage(`{"path":"outside/escape.txt","content":"no"}`)); err == nil {
		t.Fatal("write followed external symlink")
	}
	if _, err := os.Stat(filepath.Join(other, "escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("external file created: %v", err)
	}
}

func TestFileSearchStaysInsideWorkspace(t *testing.T) {
	workspace, other := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "nested", "note.txt"), []byte("The answer is here"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "secret.txt"), []byte("The answer is private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, filepath.Join(workspace, "outside")); err != nil {
		t.Fatal(err)
	}
	f, err := OpenFiles(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	result, err := f.Capabilities()[3].Call(context.Background(), json.RawMessage(`{"pattern":"answer"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != `{"matches":[{"path":"nested/note.txt","line":1,"text":"The answer is here"}],"truncated":false}` {
		t.Fatalf("unexpected search result: %s", result)
	}
}

func TestEditReplacesOneExactMatch(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "a.txt"), []byte("x = 1\ny = 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := OpenFiles(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	read, edit := f.Capabilities()[0].Call, f.Capabilities()[1].Call
	if _, err := read(context.Background(), json.RawMessage(`{"path":"a.txt"}`)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`{"path":"a.txt","old_string":"= 1","new_string":"= 2"}`, `{"path":"a.txt","old_string":"z","new_string":"w"}`, `{"path":"a.txt","old_string":"x","new_string":"x"}`} {
		if _, err := edit(context.Background(), json.RawMessage(bad)); err == nil {
			t.Fatalf("edit admitted: %s", bad)
		}
	}
	result, err := edit(context.Background(), json.RawMessage(`{"path":"a.txt","old_string":"= 1","new_string":"= 2","replace_all":true}`))
	if err != nil || !strings.Contains(string(result), `"replacements":2`) {
		t.Fatalf("result=%s err=%v", result, err)
	}
	if data, _ := os.ReadFile(filepath.Join(workspace, "a.txt")); string(data) != "x = 2\ny = 2\n" {
		t.Fatalf("edited=%q", data)
	}
}

func TestSearchByRegexAndGlob(t *testing.T) {
	workspace := t.TempDir()
	for path, body := range map[string]string{"q1.csv": "id,total\n7,120\n", "reports/2026/q2.csv": "id,total\n9,300\n", "reports/notes.md": "total 300\n", ".git/x.csv": "total\n"} {
		_ = os.MkdirAll(filepath.Join(workspace, filepath.Dir(path)), 0700)
		if err := os.WriteFile(filepath.Join(workspace, path), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	f, err := OpenFiles(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	search := f.Capabilities()[3].Call
	cases := map[string]string{
		`{"glob":"*.csv"}`:                                 `{"paths":["q1.csv","reports/2026/q2.csv"],"truncated":false}`,
		`{"glob":"reports/**/*.csv"}`:                      `{"paths":["reports/2026/q2.csv"],"truncated":false}`,
		`{"pattern":"^\\d+,3\\d\\d$","glob":"*.csv"}`:      `{"matches":[{"path":"reports/2026/q2.csv","line":2,"text":"9,300"}],"truncated":false}`,
		`{"pattern":"TOTAL","ignore_case":true,"limit":1}`: `{"matches":[{"path":"q1.csv","line":1,"text":"id,total"}],"truncated":true}`,
	}
	for input, want := range cases {
		result, err := search(context.Background(), json.RawMessage(input))
		if err != nil || string(result) != want {
			t.Fatalf("%s: result=%s err=%v", input, result, err)
		}
	}
	for _, bad := range []string{`{}`, `{"pattern":"("}`, `{"glob":"*","limit":500}`} {
		if _, err := search(context.Background(), json.RawMessage(bad)); err == nil {
			t.Fatalf("search admitted: %s", bad)
		}
	}
}

func TestUploadCapabilitiesAreReadOnly(t *testing.T) {
	uploads := t.TempDir()
	if err := os.WriteFile(filepath.Join(uploads, "invoice.txt"), []byte("approved"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := OpenFiles(uploads)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	capabilities := f.UploadCapabilities()
	if len(capabilities) != 2 || capabilities[0].Name != "upload_read" || capabilities[1].Name != "upload_search" {
		t.Fatalf("unexpected upload tool catalog: %+v", capabilities)
	}
	for _, capability := range capabilities {
		if capability.Effect != "read" || capability.Origin != "uploads" {
			t.Fatalf("upload mutation admitted: %+v", capability)
		}
	}
	result, err := capabilities[0].Call(context.Background(), json.RawMessage(`{"path":"invoice.txt"}`))
	if err != nil || !strings.Contains(string(result), "approved") {
		t.Fatalf("upload read failed: %s %v", result, err)
	}
}

func TestSkillsReadFrontmatterAndBody(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "daily-lead-union"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "daily-lead-union", "SKILL.md"), []byte("---\nname: daily-lead-union\ndescription: Build the daily report\n---\nLong instructions remain in the file."), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := OpenFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	catalog, err := f.Skills()
	if err != nil || len(catalog) != 1 || catalog[0].Name != "daily-lead-union" || catalog[0].Description != "Build the daily report" ||
		catalog[0].Content != "" || catalog[0].Load == nil {
		t.Fatalf("catalog=%+v err=%v", catalog, err)
	}
	if body, err := catalog[0].Load(); err != nil || body != "Long instructions remain in the file." {
		t.Fatalf("body=%q err=%v", body, err)
	}
}

func TestUploadReadsRunThroughAgent(t *testing.T) {
	uploads := t.TempDir()
	if err := os.WriteFile(filepath.Join(uploads, "invoice.txt"), []byte("Invoice approved"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := OpenFiles(uploads)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	answers := []string{
		`{"javascriptCode":"final('Find the invoice',{})"}`,
		`{"javascriptCode":"const matches=upload_search({glob:'invoice*'}); const file=upload_read({path:matches.paths[0]}); final('Found the invoice',{matches,file});"}`,
		`{"answer":"Invoice approved"}`,
	}
	calls := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls >= len(answers) {
			t.Errorf("unexpected model call")
			http.Error(w, "unexpected", 400)
			return
		}
		response := answers[calls]
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ax.Object("choices", ax.Array(ax.Object("message", ax.Object("role", "assistant", "content", response), "finish_reason", "stop"))))
	}))
	defer model.Close()
	client := ax.NewOpenAICompatibleClient(ax.Object("base_url", model.URL, "api_key", "synthetic", "model", "fixture"))
	spec := agent.Spec{Version: 1, RunID: "upload", InputID: "input", Prompt: "Read the uploaded invoice"}
	var tools []string
	result, err := agent.RunWithTools(context.Background(), spec, client, agent.Tools{Capabilities: f.UploadCapabilities()}, finishedTools(&tools))
	if err != nil || result.Status != "completed" || result.Answer != "Invoice approved" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if strings.Join(tools, ",") != "upload_search,upload_read" {
		t.Fatalf("finished tools=%v", tools)
	}
}

func TestFileCapabilitiesRunThroughAgent(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := OpenFiles(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	answers := []string{
		`{"javascriptCode":"final('Update the note',{})"}`,
		`{"javascriptCode":"const r=file_read({path:'note.txt'}); const e=file_edit({path:'note.txt',old_string:r.content,new_string:'done'}); const w=file_write({path:'result.txt',content:'created'}); final('Updated the note',{r,e,w});"}`,
		`{"answer":"Updated the note"}`,
	}
	calls := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls >= len(answers) {
			t.Errorf("unexpected model call")
			http.Error(w, "unexpected", 400)
			return
		}
		response := answers[calls]
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ax.Object("choices", ax.Array(ax.Object("message", ax.Object("role", "assistant", "content", response), "finish_reason", "stop"))))
	}))
	defer model.Close()
	client := ax.NewOpenAICompatibleClient(ax.Object("base_url", model.URL, "api_key", "synthetic", "model", "fixture"))
	spec := agent.Spec{Version: 1, RunID: "files", InputID: "input", Prompt: "Update the note"}
	var tools []string
	result, err := agent.RunWithTools(context.Background(), spec, client, agent.Tools{Capabilities: f.Capabilities()}, finishedTools(&tools))
	if err != nil || result.Status != "completed" || result.Kind != "answer" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if strings.Join(tools, ",") != "file_read,file_edit,file_write" {
		t.Fatalf("finished tools=%v", tools)
	}
	data, err := os.ReadFile(filepath.Join(workspace, "note.txt"))
	if err != nil || string(data) != "done" {
		t.Fatalf("edited file=%q err=%v", data, err)
	}
	data, err = os.ReadFile(filepath.Join(workspace, "result.txt"))
	if err != nil || string(data) != "created" {
		t.Fatalf("created file=%q err=%v", data, err)
	}
}

func finishedTools(names *[]string) func(agent.Event) error {
	return func(e agent.Event) error {
		if e.Type == "tool.finished" && e.Error == "" {
			*names = append(*names, e.Name)
		}
		return nil
	}
}

func TestReadFollowsHermesLimits(t *testing.T) {
	root := t.TempDir()
	var lines []string
	for i := 1; i <= 2500; i++ {
		lines = append(lines, "line "+strconv.Itoa(i))
	}
	lines[0] = strings.Repeat("w", 3000)
	if err := os.WriteFile(filepath.Join(root, "big.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := OpenFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out struct {
		Content    string `json:"content"`
		StartLine  int    `json:"start_line"`
		EndLine    int    `json:"end_line"`
		TotalLines int    `json:"total_lines"`
		Truncated  bool   `json:"truncated"`
	}
	raw, err := f.read(context.Background(), json.RawMessage(`{"path":"big.txt"}`))
	if err != nil || json.Unmarshal(raw, &out) != nil {
		t.Fatal(err)
	}
	if out.EndLine != 2000 || out.TotalLines != 2500 || !out.Truncated || !strings.Contains(out.Content, "[line truncated]") {
		t.Fatalf("start=%d end=%d total=%d truncated=%v", out.StartLine, out.EndLine, out.TotalLines, out.Truncated)
	}
	raw, err = f.read(context.Background(), json.RawMessage(`{"path":"big.txt","offset":2400,"limit":50}`))
	if err != nil || json.Unmarshal(raw, &out) != nil || out.StartLine != 2400 || out.EndLine != 2449 || !strings.HasPrefix(out.Content, "line 2400\n") {
		t.Fatalf("paged read=%+v err=%v", out, err)
	}
}

// Every bundled OpenNeko skill must fit the Ax skills catalog.
func TestBuiltinSkillsFitTheCatalog(t *testing.T) {
	f, err := OpenFiles(filepath.Join(mustAbs(t, "../../../../packages/llm/assets/builtin-skills")))
	if err != nil {
		t.Skip("built-in skills are outside this checkout")
	}
	defer f.Close()
	catalog, err := f.Skills()
	if err != nil || len(catalog) == 0 {
		t.Fatalf("catalog=%d err=%v", len(catalog), err)
	}
	for _, skill := range catalog {
		if utf8.RuneCountInString(skill.Description) > 1024 || skill.Description == "" {
			t.Fatalf("%s description has %d characters", skill.Name, utf8.RuneCountInString(skill.Description))
		}
	}
}

func mustAbs(t *testing.T, path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}
