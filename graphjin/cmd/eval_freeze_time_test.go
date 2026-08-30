package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// promptCapturingClient answers every model call with a terminal refusal and
// keeps what it was asked, so a test can assert on the rendered prompt without
// provider traffic.
type promptCapturingClient struct {
	mu       sync.Mutex
	captured []string
}

func (c *promptCapturingClient) Chat(_ context.Context, request map[string]ax.Value, _ map[string]ax.Value) (ax.Value, error) {
	if encoded, err := json.Marshal(request); err == nil {
		c.mu.Lock()
		c.captured = append(c.captured, string(encoded))
		c.mu.Unlock()
	}
	content, _ := json.Marshal(map[string]string{
		"javascriptCode": `await final({status:"blocked",answer:"not configured"});`,
	})
	return map[string]ax.Value{
		"results": []ax.Value{map[string]ax.Value{"content": string(content)}},
		"model_usage": map[string]ax.Value{"tokens": map[string]ax.Value{
			"prompt": 10, "completion": 5,
		}},
	}, nil
}

func (*promptCapturingClient) Embed(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.Value, error) {
	return nil, nil
}

func (*promptCapturingClient) Stream(context.Context, map[string]ax.Value, map[string]ax.Value) ([]ax.Value, error) {
	return nil, nil
}

func (c *promptCapturingClient) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.captured, "\n")
}

func askEvalAgent(t *testing.T, instance gjeval.Instance, instruction string) (string, error) {
	t.Helper()
	base := strings.TrimRight(strings.TrimSpace(instance.BaseURL()), "/")
	for _, suffix := range []string{"/api/v1/agent/status", "/api/v1/agent", "/api/v1/graphql"} {
		base = strings.TrimSuffix(base, suffix)
	}
	body, err := json.Marshal(map[string]any{"instruction": instruction})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequest(http.MethodPost, base+"/api/v1/agent", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range instance.Headers() {
		request.Header.Set(key, value)
	}
	client := &http.Client{Timeout: 120 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close() //nolint:errcheck
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return "", err
	}
	answer, _ := payload["answer"].(string)
	if response.StatusCode >= 500 {
		return answer, fmt.Errorf("agent returned %d", response.StatusCode)
	}
	return answer, nil
}

// askEvalAgentFull returns the whole agent response envelope.
func askEvalAgentFull(t *testing.T, instance gjeval.Instance, instruction string) (map[string]any, error) {
	t.Helper()
	base := strings.TrimRight(strings.TrimSpace(instance.BaseURL()), "/")
	for _, suffix := range []string{"/api/v1/agent/status", "/api/v1/agent", "/api/v1/graphql"} {
		base = strings.TrimSuffix(base, suffix)
	}
	trace := true
	body, err := json.Marshal(gjagent.Request{Instruction: instruction, ReturnTrace: &trace})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequest(http.MethodPost, base+"/api/v1/agent", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range instance.Headers() {
		request.Header.Set(key, value)
	}
	client := &http.Client{Timeout: 120 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close() //nolint:errcheck
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// The agent is told what "today" is, and a run that crosses midnight otherwise
// asks two different questions of the same frozen rows. This asserts the frozen
// clock actually reaches the prompt the model is rendered, through the same
// embedded boot a real evaluation uses — the option is easy to add and easy to
// leave unwired, and an unwired one fails silently as drift.
func TestFrozenClockReachesTheAgentPrompt(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	project := t.TempDir()
	if err := extractDefaultDemo(project); err != nil {
		t.Fatal(err)
	}
	originalPath, originalConf, originalDB, originalOpened := cpath, conf, db, dbOpened
	defer func() {
		cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened
	}()
	t.Setenv("GO_ENV", "dev")

	const frozen = "2026-08-01T12:30:00Z"
	const frozenDay = "2026-08-01"

	capture := &promptCapturingClient{}
	environment := evalEnvironment{ClientFactory: func(gjagent.Config) (ax.AIClient, error) { return capture, nil }}
	instance, err := environment.Start(context.Background(), gjeval.EnvSpec{
		Target: gjeval.TargetDemo, ConfigPath: project, Seed: 23, FreezeTime: frozen,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close() //nolint:errcheck

	if _, err := askEvalAgent(t, instance, "How many accounts are there?"); err != nil {
		t.Fatal(err)
	}
	rendered := capture.text()
	if rendered == "" {
		t.Fatal("the agent never rendered a prompt")
	}
	if !strings.Contains(rendered, frozenDay) {
		t.Fatalf("the frozen day %s never reached the prompt", frozenDay)
	}
}
