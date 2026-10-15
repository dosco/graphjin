package localtool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func runShell(t *testing.T, s *Shell, ctx context.Context, input string) (shellResult, error) {
	t.Helper()
	raw, err := s.Capability().Call(ctx, json.RawMessage(input))
	var result shellResult
	if err == nil {
		if decodeErr := json.Unmarshal(raw, &result); decodeErr != nil {
			t.Fatal(decodeErr)
		}
	}
	return result, err
}

func TestShellRunsInTheWorkspace(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenShell(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runShell(t, s, context.Background(), `{"command":"pwd; echo hello"}`)
	resolved, _ := filepath.EvalSymlinks(dir)
	if err != nil || result.ExitCode != 0 || !strings.Contains(result.Output, "hello") || !strings.Contains(result.Output, resolved) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	result, err = runShell(t, s, context.Background(), `{"command":"echo oops >&2; exit 3"}`)
	if err != nil || result.ExitCode != 3 || !strings.Contains(result.Output, "oops") {
		t.Fatalf("a non-zero exit was not a normal result: %+v %v", result, err)
	}
}

func TestShellTimeoutKillsChildProcesses(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenShell(dir, nil)
	start := time.Now()
	result, err := runShell(t, s, context.Background(), `{"command":"sleep 30 & echo $! > child.pid; wait","timeout_seconds":1}`)
	if err != nil || !result.TimedOut || time.Since(start) > 10*time.Second {
		t.Fatalf("result=%+v err=%v elapsed=%s", result, err, time.Since(start))
	}
	data, err := os.ReadFile(filepath.Join(dir, "child.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	time.Sleep(100 * time.Millisecond)
	if syscall.Kill(pid, 0) == nil {
		t.Fatalf("child process %d survived the timeout", pid)
	}
}

func TestShellStripsCredentials(t *testing.T) {
	t.Setenv("HARNESS_SECRET_KEY", "do-not-leak")
	t.Setenv("HARNESS_VISIBLE", "fine")
	s, _ := OpenShell(t.TempDir(), []string{"HARNESS_SECRET_KEY"})
	result, err := runShell(t, s, context.Background(), `{"command":"echo \"[$HARNESS_SECRET_KEY][$HARNESS_VISIBLE]\""}`)
	if err != nil || !strings.Contains(result.Output, "[][fine]") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestShellOutputIsCapped(t *testing.T) {
	s, _ := OpenShell(t.TempDir(), nil)
	result, err := runShell(t, s, context.Background(), `{"command":"echo START; head -c 300000 /dev/zero | tr '\\0' 'a'; echo; echo END"}`)
	if err != nil || !result.Truncated || len([]rune(result.Output)) > maxShellOutput+100 ||
		!strings.HasPrefix(result.Output, "START") || !strings.Contains(result.Output, "END") || !strings.Contains(result.Output, "output truncated") {
		t.Fatalf("truncated=%v length=%d err=%v", result.Truncated, len(result.Output), err)
	}
}

func TestShellStopsOnCancel(t *testing.T) {
	s, _ := OpenShell(t.TempDir(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	if _, err := runShell(t, s, ctx, `{"command":"sleep 30"}`); err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("cancellation did not stop the command: err=%v elapsed=%s", err, time.Since(start))
	}
}

func TestShellRejectsBadInput(t *testing.T) {
	s, _ := OpenShell(t.TempDir(), nil)
	for _, input := range []string{`{"command":""}`, `{"command":"ls","timeout_seconds":601}`} {
		if _, err := runShell(t, s, context.Background(), input); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	if _, err := OpenShell("relative/dir", nil); err == nil {
		t.Fatal("accepted a relative workspace")
	}
}

func TestShellHostLimits(t *testing.T) {
	s, _ := OpenShell(t.TempDir(), nil)
	if err := s.SetLimits(0, 50_000); err == nil {
		t.Fatal("zero timeout was accepted")
	}
	if err := s.SetLimits(30, 2_000); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.Capability().Description, "2000 characters") || !strings.Contains(s.Capability().Description, "30 seconds") {
		t.Fatalf("description does not show host limits: %s", s.Capability().Description)
	}
	raw, err := s.Capability().Call(context.Background(), json.RawMessage(`{"command":"yes x | head -c 10000"}`))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Output    string `json:"output"`
		Truncated bool   `json:"truncated"`
	}
	if json.Unmarshal(raw, &out) != nil || !out.Truncated || len([]rune(out.Output)) > 2_000+40 {
		t.Fatalf("host output cap not applied: truncated=%v len=%d", out.Truncated, len(out.Output))
	}
}

func TestShellSetsSkillDirFromTheScriptPath(t *testing.T) {
	t.Setenv("SKILL_DIR", "")
	skills := t.TempDir()
	for _, path := range []string{"stock-check/scripts/check.py", "lead-report/scripts/report.py", "lead-report/scripts/common.py", "stock-check/scripts/common.py"} {
		if err := os.MkdirAll(filepath.Join(skills, filepath.Dir(path)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skills, path), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	s, _ := OpenShell(t.TempDir(), nil)
	s.SetSkillsRoot(skills)
	for command, want := range map[string]string{
		`echo \"[$SKILL_DIR]\" \"$SKILL_DIR/scripts/check.py\"`:                          "[" + filepath.Join(skills, "stock-check") + "]",
		`echo \"[${SKILL_DIR}]\" ${SKILL_DIR}/scripts/report.py`:                         "[" + filepath.Join(skills, "lead-report") + "]",
		`echo \"[$SKILL_DIR]\" $SKILL_DIR/scripts/common.py`:                             "[]",
		`echo \"[$SKILL_DIR]\" $SKILL_DIR/scripts/check.py $SKILL_DIR/scripts/report.py`: "[]",
		`echo \"[$SKILL_DIR]\" $SKILL_DIR/../../etc/passwd`:                              "[]",
	} {
		result, err := runShell(t, s, context.Background(), `{"command":"`+command+`"}`)
		if err != nil || !strings.HasPrefix(result.Output, want) {
			t.Fatalf("%s: result=%+v err=%v want %s", command, result, err, want)
		}
	}
}
