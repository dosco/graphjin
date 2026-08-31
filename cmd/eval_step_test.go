package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// startStepTestServer boots one real world whose model calls are parked for a
// trainer to answer, exactly as `env serve --step` does.
func startStepTestServer(t *testing.T) (*envServer, *stepServer, func()) {
	t.Helper()
	project := t.TempDir()
	if err := extractDefaultDemo(project); err != nil {
		t.Fatal(err)
	}
	originalPath, originalConf, originalDB, originalOpened := cpath, conf, db, dbOpened
	t.Setenv("GO_ENV", "dev")

	mailbox := newStepMailbox(nil)
	environment := evalEnvironment{
		ClientFactory: func(gjagent.Config) (ax.AIClient, error) { return mailbox, nil },
	}
	pool, err := newEvalInstancePool(context.Background(), func(int) evalEnvironment { return environment },
		gjeval.EnvSpec{
			Target: gjeval.TargetDemo, ConfigPath: project, Seed: 23, FreezeTime: "2026-08-01T12:00:00Z",
		}, 1)
	if err != nil {
		cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened
		t.Fatal(err)
	}
	server := &envServer{
		pool: pool, suite: envTestSuite(t), profile: gjeval.RewardProfileRL, side: "train",
		byID: map[string]gjeval.Task{}, bySlug: map[string]gjeval.Task{},
		mailboxes: map[gjeval.Instance]*stepMailbox{pool.instances[0]: mailbox},
	}
	if err := server.indexTasks(); err != nil {
		t.Fatal(err)
	}
	return server, newStepServer(server, 30*time.Second), func() {
		_ = pool.Close()
		cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened
	}
}

func postStepJSON(t *testing.T, handler http.HandlerFunc, path string, body any) (int, stepResponse) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(encoded)))
	var response stepResponse
	if rec.Body.Len() != 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &response)
	}
	return rec.Code, response
}

// A trainer supplies each completion itself and gets the same graded verdict a
// hosted episode would have produced. That equality is the whole point: a
// policy trained through this bridge is measured by the benchmark's contract.
func TestStepBridgeDrivesAnEpisodeToAGradedResult(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	server, steps, stop := startStepTestServer(t)
	defer stop()

	code, response := postStepJSON(t, steps.handleReset, "/step/reset",
		stepResetRequest{Slug: "count-accounts"})
	if code != http.StatusOK {
		t.Fatalf("reset status = %d: %+v", code, response)
	}
	if response.Done || response.Observation == nil {
		t.Fatalf("expected something for the model to answer: %+v", response)
	}
	// The observation has to carry enough to produce a usable completion: which
	// stage asked, the conversation, and the shape the answer must take.
	if response.Observation.Stage == "" || len(response.Observation.Messages) == 0 {
		t.Fatalf("observation is not usable: %+v", response.Observation)
	}
	if response.Observation.Messages[0].Content == "" {
		t.Fatal("the rendered prompt did not reach the trainer")
	}

	episodeID := response.EpisodeID
	// The agent must discover a specific catalog card before running raw
	// GraphQL. That guard is the environment behaving as it does for any policy,
	// so a completion supplied through the bridge has to satisfy it too.
	program := `const detail = await query_catalog({id: "table:app:main.accounts"});
		const res = await execute_graphql({query: "query { accounts { count_id } }"});
		await final({status: "answered", answer: "There are " + res.data.accounts[0].count_id + " accounts.", data: res.data, evidence: [detail]});`
	completion, err := json.Marshal(map[string]string{"javascriptCode": program})
	if err != nil {
		t.Fatal(err)
	}

	// Keep answering whatever is asked until the episode finishes.
	for turn := 0; turn < 8 && !response.Done; turn++ {
		code, response = postStepJSON(t, steps.handleStep, "/step", stepRequest{
			EpisodeID: episodeID, Completion: string(completion),
			PromptTokens: 100, CompletionTokens: 40,
		})
		if code != http.StatusOK {
			t.Fatalf("step status = %d: %+v", code, response)
		}
	}
	if !response.Done {
		t.Fatal("the episode never finished")
	}
	if response.Score == nil {
		t.Fatalf("a finished episode must come back graded: %+v", response)
	}
	if !response.Pass {
		t.Fatalf("the compliant program did not pass: answer=%q score=%+v", response.Answer, response.Score)
	}

	// The world has to come back, or a pool drains one episode at a time.
	instance, err := server.pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("the world was not released: %v", err)
	}
	_ = server.pool.Release(instance)
}

// A trainer that walks away must not take a world with it. Without the idle
// timeout, a crashed loop removes a world from the pool for the rest of the run
// and the whole thing quietly halves in throughput.
func TestStepBridgeReclaimsWorldsFromAbandonedEpisodes(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	server, _, stop := startStepTestServer(t)
	defer stop()
	// Long enough for the agent to ask its first question, short enough that
	// idling past it is quick to observe.
	steps := newStepServer(server, time.Second)

	code, response := postStepJSON(t, steps.handleReset, "/step/reset",
		stepResetRequest{Slug: "count-accounts"})
	if code != http.StatusOK || response.Observation == nil {
		t.Fatalf("reset status = %d: %+v", code, response)
	}
	time.Sleep(1200 * time.Millisecond)

	// Never answer. The reaper should end it and give the world back.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		steps.reap()
		if _, ok := steps.lookup(response.EpisodeID); !ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, ok := steps.lookup(response.EpisodeID); ok {
		t.Fatal("an idle episode was never reclaimed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	instance, err := server.pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("the world was not given back: %v", err)
	}
	_ = server.pool.Release(instance)

	// And a late completion is told the episode is gone rather than hanging.
	code, _ = postStepJSON(t, steps.handleStep, "/step",
		stepRequest{EpisodeID: response.EpisodeID, Completion: "{}"})
	if code != http.StatusGone {
		t.Fatalf("a completion for a reclaimed episode returned %d, want %d", code, http.StatusGone)
	}
}

// The mailbox hands out what the model was asked and hands back what the
// trainer answered, in the shape the pipeline expects from a provider.
func TestStepMailboxRoundTripsACompletion(t *testing.T) {
	mailbox := newStepMailbox(nil)
	values := map[string]ax.Value{"chat_prompt": []ax.Value{
		map[string]ax.Value{"role": "system", "content": "You (`executor`) write the code."},
		map[string]ax.Value{"role": "user", "content": "count the accounts"},
	}}

	done := make(chan ax.Value, 1)
	go func() {
		result, err := mailbox.Chat(context.Background(), values, nil)
		if err != nil {
			done <- nil
			return
		}
		done <- result
	}()

	parked := <-mailbox.requests
	if parked.Stage != gjagent.StageExecutor {
		t.Fatalf("stage = %q", parked.Stage)
	}
	messages := stepMessagesFromRequest(parked.Values)
	if len(messages) != 2 || messages[1].Content != "count the accounts" {
		t.Fatalf("messages = %+v", messages)
	}
	parked.Reply <- mailboxReply{Completion: `{"javascriptCode":"await final('done', {})"}`,
		PromptTokens: 12, CompletionTokens: 7}

	result := <-done
	shaped, ok := result.(map[string]ax.Value)
	if !ok {
		t.Fatalf("completion was not provider-shaped: %T", result)
	}
	results, _ := shaped["results"].([]ax.Value)
	if len(results) != 1 {
		t.Fatalf("results = %+v", shaped["results"])
	}
	content, _ := results[0].(map[string]ax.Value)["content"].(string)
	if content != `{"javascriptCode":"await final('done', {})"}` {
		t.Fatalf("content = %q", content)
	}
	// Token counts have to survive: efficiency is a term in the reward, and a
	// trainer whose usage vanished would score as though its policy were free.
	usage, _ := shaped["model_usage"].(map[string]ax.Value)
	tokens, _ := usage["tokens"].(map[string]ax.Value)
	if fmt.Sprint(tokens["prompt"]) != "12" || fmt.Sprint(tokens["completion"]) != "7" {
		t.Fatalf("usage = %+v", tokens)
	}
}

// An episode that ends while a call is parked must not leave the agent blocked
// forever holding a world.
func TestStepMailboxUnblocksWhenTheEpisodeEnds(t *testing.T) {
	mailbox := newStepMailbox(nil)
	values := map[string]ax.Value{"chat_prompt": []ax.Value{
		map[string]ax.Value{"role": "system", "content": "You (`executor`) write the code."},
	}}
	errs := make(chan error, 1)
	go func() {
		_, err := mailbox.Chat(context.Background(), values, nil)
		errs <- err
	}()
	parked := <-mailbox.requests
	close(parked.Reply)
	select {
	case err := <-errs:
		if err == nil {
			t.Fatal("a call whose episode ended must fail rather than return a completion")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the parked call never unblocked")
	}
}
