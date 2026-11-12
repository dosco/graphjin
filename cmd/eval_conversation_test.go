package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/dosco/graphjin/serv/v3"
)

// embeddedEvalSuites are the suites this binary carries and runs on an
// instance it configures itself.
func embeddedEvalSuites(t *testing.T) map[string]*gjeval.Suite {
	t.Helper()
	public, err := loadPublicEvalSuite()
	if err != nil {
		t.Fatal(err)
	}
	contoso, err := loadDeepContosoEvalSuite()
	if err != nil {
		t.Fatal(err)
	}
	return map[string]*gjeval.Suite{"public": public, DeepContosoBenchmarkSlug: contoso}
}

// An eval instance exposes the data agent alone, and the server refuses a
// request with history there: HTTP 400, before any agent runs. For a week the
// public suite still carried seven tasks that sent history, so 21 of 339 slots
// came back refused on every run and were scored as the model failing. While
// the instance is configured that way, no embedded suite may send earlier
// turns.
func TestEmbeddedSuitesSendNoHistoryToAnInstanceThatRefusesIt(t *testing.T) {
	var config serv.Config
	config.Agent.Lead.Enabled = true
	configureEvalInstance(&config, gjeval.EnvSpec{})
	if config.Agent.Lead.Enabled {
		t.Skip("the eval instance now runs the lead, which holds a conversation")
	}
	for name, suite := range embeddedEvalSuites(t) {
		for _, task := range gjeval.ConversationTasks(suite.Tasks) {
			t.Errorf("%s suite: %s sends %d earlier turn(s), which the eval instance refuses with HTTP 400",
				name, task.Slug, len(task.Turns))
		}
	}
}

// The same contract against a booted eval instance, end to end: what the
// instance reports about itself, what its agent endpoint really does with
// history, and what the run-time check concludes all have to agree. If they
// drift apart, an episode can be rejected unanswered and graded anyway.
func TestEvalInstanceRefusesHistoryAndSaysSo(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded demo integration")
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

	spec := gjeval.PublicBenchmark()
	instance, err := (evalEnvironment{}).Start(context.Background(), gjeval.EnvSpec{
		Target: gjeval.TargetDemo, ConfigPath: project, Seed: spec.Seed,
		Writable: true, Reactive: true, Resettable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close() //nolint:errcheck
	client := &http.Client{Timeout: 30 * time.Second}

	get, err := http.NewRequest(http.MethodGet, instance.BaseURL()+"/api/v1/agent/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range instance.Headers() {
		get.Header.Set(key, value)
	}
	response, err := client.Do(get)
	if err != nil {
		t.Fatal(err)
	}
	var status gjeval.AgentStatus
	err = json.NewDecoder(response.Body).Decode(&status)
	response.Body.Close() //nolint:errcheck
	if err != nil {
		t.Fatal(err)
	}
	if status.LeadEnabled == nil {
		t.Fatal("the eval instance does not say whether it holds a conversation")
	}

	conversation := gjeval.Task{
		Slug: "follow-up", Prompt: "What is that account's current MRR?",
		Turns: []gjeval.TurnSpec{{Role: "user", Content: "Which account is Meridian Robotics?"}},
	}
	checkErr := gjeval.AssertConversationsRunnable(context.Background(), client, instance, []gjeval.Task{conversation})
	if !status.RefusesConversations() {
		if checkErr != nil {
			t.Fatalf("the instance holds a conversation, yet the check refused one: %v", checkErr)
		}
		return
	}
	if checkErr == nil {
		t.Fatal("the instance refuses conversations, yet the check let one through")
	}

	// The status has to describe what the endpoint does. A request with history
	// is turned away before any model is called, so this costs nothing.
	body := `{"instruction":"What is that account's current MRR?","history":[{"role":"user","content":"Which account is Meridian Robotics?"}]}`
	post, err := http.NewRequest(http.MethodPost, instance.BaseURL()+"/api/v1/agent", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	post.Header.Set("Content-Type", "application/json")
	for key, value := range instance.Headers() {
		post.Header.Set(key, value)
	}
	refused, err := client.Do(post)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(refused.Body)
	refused.Body.Close() //nolint:errcheck
	if refused.StatusCode != http.StatusBadRequest || !strings.Contains(string(raw), "agent.lead.enabled") {
		t.Fatalf("the instance says it refuses history, but answered HTTP %d: %s", refused.StatusCode, raw)
	}

	// Which is why nothing the binary ships may send any, and why the suites it
	// ships pass the check the runner applies before every run.
	for name, suite := range embeddedEvalSuites(t) {
		if err := gjeval.AssertConversationsRunnable(context.Background(), client, instance, suite.Tasks); err != nil {
			t.Errorf("%s suite would be rejected by the eval instance: %v", name, err)
		}
	}
}
