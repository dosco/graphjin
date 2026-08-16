package eval

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

func concurrencyReadTask(t *testing.T, n string) Task {
	t.Helper()
	task := Task{
		Slug: "read-" + n, Category: CategoryAggregate, Difficulty: DifficultyT1,
		Prompt:     "READ " + n + ": What is total MRR?",
		Provenance: Provenance{Source: "imported"}, ExpectedStatus: gjagent.StatusAnswered,
		Oracle:   &OracleSpec{Query: "query { accounts { sum_mrr } }", Extract: "accounts.0.sum_mrr"},
		Answer:   AnswerRule{Kind: "number"},
		Method:   MethodRule{RequireQueryMatch: []string{"sum_mrr"}},
		Behavior: BehaviorRule{RequiredActions: []string{"execute_graphql"}},
	}
	if err := task.Normalize(); err != nil {
		t.Fatal(err)
	}
	return task
}

func concurrencyMutationTask(t *testing.T, n string) Task {
	t.Helper()
	task := Task{
		Slug: "mutate-" + n, Category: CategoryAction, Difficulty: DifficultyT4,
		Prompt:     "MUTATE " + n + ": Record payment PAY-" + n + ".",
		Provenance: Provenance{GeneratorVersion: GeneratorVersion, Source: "curated"},
		Method:     MethodRule{RequireQueryMatch: []string{`mutation.*payments`}},
		Behavior:   BehaviorRule{RequiredActions: []string{"execute_graphql:mutation"}},
		Mutation: &MutationSpec{
			ResetStrategy: "sqlite-copy", ExpectedValue: "1",
			PostState:  OracleSpec{Query: `query { payments { count_id } }`, Extract: "payments.0.count_id"},
			Collateral: []OracleSpec{{Query: `query { accounts { count_id } }`, Extract: "accounts.0.count_id"}},
		},
		ExpectedStatus: gjagent.StatusAnswered,
	}
	if err := task.Normalize(); err != nil {
		t.Fatal(err)
	}
	return task
}

// concurrencyProbe is the doer plus instance instrumentation that observes
// database occupancy the way a real demo instance would experience it.
type concurrencyProbe struct {
	readers    atomic.Int64
	mutators   atomic.Int64
	maxReaders atomic.Int64
	violations atomic.Int64
}

func (p *concurrencyProbe) enterReader() {
	if p.mutators.Load() > 0 {
		p.violations.Add(1)
	}
	now := p.readers.Add(1)
	for {
		max := p.maxReaders.Load()
		if now <= max || p.maxReaders.CompareAndSwap(max, now) {
			break
		}
	}
}

func (p *concurrencyProbe) enterMutator() {
	if p.mutators.Add(1) > 1 || p.readers.Load() > 0 {
		p.violations.Add(1)
	}
}

func concurrencySuite(t *testing.T, reads, mutations int) Suite {
	t.Helper()
	tasks := make([]Task, 0, reads+mutations)
	for i := 0; i < reads; i++ {
		tasks = append(tasks, concurrencyReadTask(t, string(rune('a'+i))))
	}
	for i := 0; i < mutations; i++ {
		tasks = append(tasks, concurrencyMutationTask(t, string(rune('a'+i))))
	}
	suite := Suite{Name: "concurrency", Generator: GeneratorMeta{Version: GeneratorVersion, Scale: len(tasks)}, Tasks: tasks}
	if err := suite.Normalize(); err != nil {
		t.Fatal(err)
	}
	return suite
}

func concurrencyDoer(t *testing.T, probe *concurrencyProbe, hold time.Duration) HTTPDoer {
	t.Helper()
	return doerFunc(func(request *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			return nil, err
		}
		if strings.HasSuffix(request.URL.Path, "/graphql") {
			query := valueString(payload["query"])
			switch {
			case strings.Contains(query, "sum_mrr"):
				return jsonResponse(200, `{"data":{"accounts":[{"sum_mrr":42}]}}`), nil
			case strings.Contains(query, "payments"):
				return jsonResponse(200, `{"data":{"payments":[{"count_id":1}]}}`), nil
			default:
				return jsonResponse(200, `{"data":{"accounts":[{"count_id":7}]}}`), nil
			}
		}
		instruction := valueString(payload["instruction"])
		mutate := strings.Contains(instruction, "MUTATE")
		if mutate {
			probe.enterMutator()
		} else {
			probe.enterReader()
		}
		time.Sleep(hold)
		var response gjagent.Response
		if mutate {
			response = responseWithAnswer(gjagent.StatusAnswered, "Payment recorded.")
			response.Actions = []map[string]any{{
				"tool": "execute_graphql", "status": "ok",
				"args":    map[string]any{"query": `mutation { payments(insert: {reference: "PAY"}) { id } }`},
				"summary": map[string]any{"error_count": 0},
			}}
			probe.mutators.Add(-1)
		} else {
			response = responseWithAnswer(gjagent.StatusAnswered, "The total is 42.")
			response.Actions = []map[string]any{{
				"tool": "execute_graphql", "status": "ok",
				"args":    map[string]any{"query": "query { accounts { sum_mrr } }"},
				"summary": map[string]any{"error_count": 0},
			}}
			probe.readers.Add(-1)
		}
		response.Usage = map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15, "llm_calls": 2}
		data, _ := json.Marshal(response)
		return jsonResponse(200, string(data)), nil
	})
}

// TestConcurrentSlotsIsolateMutationsFromReaders is the write-isolation
// proof: at concurrency 4, read episodes genuinely overlap each other while a
// mutation episode never shares the instance with anything — its reset
// brackets would corrupt a concurrent reader's world in ways that score as
// model failures.
func TestConcurrentSlotsIsolateMutationsFromReaders(t *testing.T) {
	probe := &concurrencyProbe{}
	suite := concurrencySuite(t, 6, 2)
	instance := &ResettableStaticInstance{
		StaticInstance: &StaticInstance{URL: "http://graphjin.test", TargetLabel: "test"},
		ResetFunc: func(context.Context) error {
			if probe.readers.Load() > 0 {
				probe.violations.Add(1)
			}
			return nil
		},
	}
	report, err := (Runner{Client: concurrencyDoer(t, probe, 25 * time.Millisecond)}).Run(
		context.Background(), suite, instance,
		RunOptions{Repeats: 2, Seed: 23, Store: NewStore(t.TempDir()), Concurrency: 4},
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.RunStatus != RunStatusComplete {
		t.Fatalf("run status = %s", report.RunStatus)
	}
	if got := probe.violations.Load(); got != 0 {
		t.Fatalf("isolation violated %d times", got)
	}
	if got := probe.maxReaders.Load(); got < 2 {
		t.Fatalf("expected genuine reader overlap, max concurrent readers = %d", got)
	}
	if report.Metrics.EpisodeCount != 16 {
		t.Fatalf("episodes = %d, want 16", report.Metrics.EpisodeCount)
	}
	if report.Provenance.Concurrency != 4 {
		t.Fatalf("provenance concurrency = %d, want 4", report.Provenance.Concurrency)
	}
}

// TestConcurrentRunMatchesSerialResults pins that concurrency changes wall
// clock and nothing else: identical suite, identical scripted responses,
// identical verdicts, metrics, and usage.
func TestConcurrentRunMatchesSerialResults(t *testing.T) {
	suite := concurrencySuite(t, 5, 1)
	run := func(concurrency int) *Report {
		probe := &concurrencyProbe{}
		instance := &ResettableStaticInstance{
			StaticInstance: &StaticInstance{URL: "http://graphjin.test", TargetLabel: "test"},
			ResetFunc:      func(context.Context) error { return nil },
		}
		report, err := (Runner{Client: concurrencyDoer(t, probe, time.Millisecond), Now: func() time.Time { return time.Unix(100, 0) }}).Run(
			context.Background(), suite, instance,
			RunOptions{Repeats: 3, Seed: 23, Store: NewStore(t.TempDir()), Concurrency: concurrency},
		)
		if err != nil {
			t.Fatal(err)
		}
		return report
	}
	serial := run(1)
	concurrent := run(4)

	if serial.Metrics.Recall != concurrent.Metrics.Recall ||
		serial.Metrics.EpisodeCount != concurrent.Metrics.EpisodeCount ||
		serial.Metrics.TaskCount != concurrent.Metrics.TaskCount {
		t.Fatalf("headline metrics diverged: serial %+v vs concurrent %+v", serial.Metrics, concurrent.Metrics)
	}
	serialTasks, _ := json.Marshal(serial.Tasks)
	concurrentTasks, _ := json.Marshal(concurrent.Tasks)
	if string(serialTasks) != string(concurrentTasks) {
		t.Fatalf("task verdicts diverged:\nserial:     %s\nconcurrent: %s", serialTasks, concurrentTasks)
	}
	if serial.ProviderUsage.TotalTokens != concurrent.ProviderUsage.TotalTokens ||
		serial.ProviderUsage.LLMCalls != concurrent.ProviderUsage.LLMCalls {
		t.Fatalf("usage diverged: serial %+v vs concurrent %+v", serial.ProviderUsage, concurrent.ProviderUsage)
	}
	if serial.Provenance.Concurrency != 0 {
		t.Fatalf("serial run must not stamp concurrency, got %d", serial.Provenance.Concurrency)
	}
}

// TestConcurrentRunStopsOnEnvironmentFailure pins the failure contract: the
// first environment failure cancels the pool, in-flight slots drain, and the
// run finishes incomplete exactly as the serial loop would.
func TestConcurrentRunStopsOnEnvironmentFailure(t *testing.T) {
	suite := concurrencySuite(t, 6, 0)
	poison := suite.Tasks[3].Prompt
	probe := &concurrencyProbe{}
	inner := concurrencyDoer(t, probe, 5*time.Millisecond)
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		var payload map[string]any
		body := request.Body
		if err := json.NewDecoder(body).Decode(&payload); err != nil {
			return nil, err
		}
		if valueString(payload["instruction"]) == poison {
			return nil, errors.New("synthetic transport failure")
		}
		encoded, _ := json.Marshal(payload)
		clone, _ := http.NewRequest(request.Method, request.URL.String(), strings.NewReader(string(encoded)))
		return inner.Do(clone)
	})
	instance := &ResettableStaticInstance{
		StaticInstance: &StaticInstance{URL: "http://graphjin.test", TargetLabel: "test"},
		ResetFunc:      func(context.Context) error { return nil },
	}
	done := make(chan struct{})
	var report *Report
	var err error
	go func() {
		defer close(done)
		report, err = (Runner{Client: doer, RetryDelay: time.Millisecond}).Run(
			context.Background(), suite, instance,
			RunOptions{Repeats: 2, Seed: 23, Store: NewStore(t.TempDir()), Concurrency: 4, MaxTransientAttempts: 1},
		)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("concurrent run deadlocked on environment failure")
	}
	if err != nil {
		t.Fatalf("environment failure must finish incomplete, not error: %v", err)
	}
	if report.RunStatus != RunStatusEnvironmentFailed {
		t.Fatalf("run status = %s, want %s", report.RunStatus, RunStatusEnvironmentFailed)
	}
}
