package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The Python step client is the surface a trainer actually drives, and its
// value is entirely in matching the server's contract. A test that stubbed the
// server would only prove the client agrees with an idea of the server.
//
// So this boots the real one, serves it over a real listener, and drives it
// with the real client: reset, answer whatever is asked, read the reward. If
// the wire shape drifts on either side, this is where it shows.
func TestPythonStepClientDrivesARealEpisode(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	server, steps, stop := startStepTestServer(t)
	defer stop()

	mux := http.NewServeMux()
	steps.register(mux)
	mux.HandleFunc("/tasks", server.handleTasks)
	listener := httptest.NewServer(mux)
	defer listener.Close()
	go steps.reapUntil(contextForTest(t))

	script := `
import json, sys
sys.path.insert(0, %q)
from graphjin_env import StepEnvironment, run_episode, group_advantages

PROGRAM = (
    'const detail = await query_catalog({id: "table:app:main.accounts"});\n'
    'const res = await execute_graphql({query: "query { accounts { count_id } }"});\n'
    'await final({status: "answered", answer: "There are " + res.data.accounts[0].count_id + " accounts.", data: res.data, evidence: [detail]});'
)
completion = json.dumps({"javascriptCode": PROGRAM})

seen = []
def complete(state):
    seen.append(state.stage)
    assert state.messages, "the rendered prompt did not reach the client"
    return completion, 100, 40

env = StepEnvironment(%q)
final_state = run_episode(env, "count-accounts", complete)
print(json.dumps({
    "done": final_state.done,
    "pass": final_state.passed,
    "reward": final_state.reward,
    "stages": seen,
    "advantages": group_advantages([final_state, final_state]),
}))
`
	source := fmt.Sprintf(script, trainingDir(t), listener.URL)
	path := filepath.Join(t.TempDir(), "drive.py")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, "python3", path).CombinedOutput()
	if err != nil {
		t.Fatalf("the python client could not drive an episode: %v\n%s", err, output)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var result struct {
		Done       bool      `json:"done"`
		Pass       bool      `json:"pass"`
		Reward     float64   `json:"reward"`
		Stages     []string  `json:"stages"`
		Advantages []float64 `json:"advantages"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &result); err != nil {
		t.Fatalf("unreadable client output: %v\n%s", err, output)
	}
	if !result.Done {
		t.Fatalf("the episode never finished: %s", output)
	}
	if !result.Pass || result.Reward <= 0 {
		t.Fatalf("a compliant program driven from python did not pass: %+v\n%s", result, output)
	}
	if len(result.Stages) == 0 {
		t.Fatal("the client answered nothing, so nothing was exercised")
	}
	// Two identical rewards must produce zero advantage — the property a GRPO
	// loop depends on, and the signal that a group carries nothing to learn.
	for _, advantage := range result.Advantages {
		if advantage != 0 {
			t.Fatalf("identical rewards must yield zero advantage, got %v", result.Advantages)
		}
	}
	t.Logf("python drove %d call(s), stages %v, reward %.3f", len(result.Stages), result.Stages, result.Reward)
}

func trainingDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("../training")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func contextForTest(t *testing.T) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx
}
