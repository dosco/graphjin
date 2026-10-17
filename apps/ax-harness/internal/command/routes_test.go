package command

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/open-neko/openneko/apps/ax-harness/internal/agent"
)

func TestHostRoutesSelectAxStages(t *testing.T) {
	t.Setenv("HARNESS_CHEAP_KEY", "cheap-secret")
	t.Setenv("HARNESS_WORK_KEY", "work-secret")
	var requested []string
	serve := func(routeKey, expectedModel, expectedKey string, responses []string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+expectedKey {
				t.Errorf("wrong credential for %s", expectedModel)
			}
			var body struct {
				Model string `json:"model"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Model != expectedModel {
				t.Errorf("route sent %q to %s", body.Model, expectedModel)
			}
			requested = append(requested, routeKey)
			if len(responses) == 0 {
				http.Error(w, "too many calls", 400)
				return
			}
			content := responses[0]
			responses = responses[1:]
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}, "finish_reason": "stop"}},
				"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}})
		}))
	}
	cheap := serve("cheap", "fixture", "cheap-secret", []string{`{"javascriptCode":"final('Find reference', {})"}`})
	defer cheap.Close()
	work := serve("work", "fixture", "work-secret", []string{`{"javascriptCode":"final('Report reference', {reference:'REF-42'});"}`, `Answer: REF-42`})
	defer work.Close()
	config := func(responder string) string {
		b, _ := json.Marshal(routeConfig{Context: "cheap", Executor: "work", Responder: responder, Routes: []modelRoute{
			{Key: "cheap", Model: "fixture", URL: cheap.URL, APIKeyEnv: "HARNESS_CHEAP_KEY"},
			{Key: "work", Model: "fixture", URL: work.URL, APIKeyEnv: "HARNESS_WORK_KEY"},
		}})
		return string(b)
	}
	t.Setenv("HARNESS_MODEL_ROUTES", config("work"))
	var out bytes.Buffer
	code, err := run(context.Background(), strings.NewReader(request), &out)
	if code != 0 || err != nil {
		t.Fatalf("code=%d err=%v output=%s", code, err, out.String())
	}
	if got := strings.Join(requested, ","); got != "cheap,work,work" {
		t.Fatalf("stage routes=%s", got)
	}
	rawEvents := out.String()
	copyOfEvents := bytes.NewBufferString(rawEvents)
	var modelEvents []string
	for _, event := range events(t, copyOfEvents) {
		if event.Type == "model.request.started" {
			modelEvents = append(modelEvents, event.Origin+":"+event.Name)
		}
	}
	if got := strings.Join(modelEvents, ","); got != "cheap:fixture,work:fixture,work:fixture" {
		t.Fatalf("model receipts=%s", got)
	}
	for _, secret := range []string{"cheap-secret", "work-secret"} {
		if strings.Contains(rawEvents, secret) {
			t.Fatal("credential leaked in events")
		}
	}
}

func TestExecutorErrorEscalatesOnlyLaterApprovedModelCalls(t *testing.T) {
	var selected []string
	serve := func(alias, key string, responses []string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+key {
				t.Errorf("wrong route credential for %s", alias)
			}
			selected = append(selected, alias)
			if len(responses) == 0 {
				http.Error(w, "unexpected model call", http.StatusBadRequest)
				return
			}
			content := responses[0]
			responses = responses[1:]
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}, "finish_reason": "stop"}},
				"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}})
		}))
	}
	contextServer := serve("context", "context-secret", []string{`{"javascriptCode":"final('Plan',{});"}`, `Answer: Done`})
	defer contextServer.Close()
	baseline := serve("base", "base-secret", []string{`{"javascriptCode":"throw new Error('retry this actor step');"}`})
	defer baseline.Close()
	strong := serve("strong", "strong-secret", []string{`{"javascriptCode":"final('Done',{});"}`})
	defer strong.Close()
	t.Setenv("HARNESS_CONTEXT_KEY", "context-secret")
	t.Setenv("HARNESS_BASE_KEY", "base-secret")
	t.Setenv("HARNESS_STRONG_KEY", "strong-secret")
	raw, _ := json.Marshal(routeConfig{Context: "context", Executor: "base", ExecutorEscalation: "strong", ExecutorAfterErrors: 1,
		Responder: "context", Routes: []modelRoute{
			{Key: "context", Model: "gpt-6-luna", URL: contextServer.URL, APIKeyEnv: "HARNESS_CONTEXT_KEY"},
			{Key: "base", Model: "gpt-6-sol", URL: baseline.URL, APIKeyEnv: "HARNESS_BASE_KEY"},
			{Key: "strong", Model: "gpt-6-astra", URL: strong.URL, APIKeyEnv: "HARNESS_STRONG_KEY"},
		}})
	t.Setenv("HARNESS_MODEL_ROUTES", string(raw))
	var out bytes.Buffer
	code, err := run(context.Background(), strings.NewReader(`{"version":1,"run_id":"run-1","input_id":"input-1","prompt":"Complete the task","max_cost_micros":1000000}`), &out)
	if code != 0 || err != nil {
		t.Fatalf("code=%d err=%v events=%s", code, err, out.String())
	}
	if got := strings.Join(selected, ","); got != "context,base,strong,context" {
		t.Fatalf("actual route order=%s", got)
	}
	var started []string
	var failed int
	var terminal *agent.Result
	for _, event := range events(t, &out) {
		switch event.Type {
		case "model.request.started":
			started = append(started, event.Origin+":"+event.Name)
		case "executor.step.failed":
			failed++
			if event.CallID != 2 || event.Error != "actor_code_error" {
				t.Fatalf("invalid error-turn receipt: %+v", event)
			}
		case "run.finished":
			terminal = event.Result
		}
	}
	if got := strings.Join(started, ","); got != "context:gpt-6-luna,base:gpt-6-sol,strong:gpt-6-astra,context:gpt-6-luna" {
		t.Fatalf("model admission routes=%s", got)
	}
	if failed != 1 || terminal == nil || terminal.Status != "completed" || terminal.Cost == nil || terminal.Cost.ChargedMicros != 428 {
		t.Fatalf("failed=%d terminal=%+v", failed, terminal)
	}
}

func TestExecutorEscalationDoesNotBypassModelAdmission(t *testing.T) {
	var baselineCalls, strongCalls int
	serve := func(responses []string, calls *int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			(*calls)++
			if len(responses) == 0 {
				http.Error(w, "unexpected request", http.StatusBadRequest)
				return
			}
			content := responses[0]
			responses = responses[1:]
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}, "finish_reason": "stop"}}})
		}))
	}
	contextCalls := 0
	contextServer := serve([]string{`{"javascriptCode":"final('Plan',{});"}`, `Answer: Stopped before the work finished.`}, &contextCalls)
	defer contextServer.Close()
	baseServer := serve([]string{`{"javascriptCode":"throw new Error('executor failed');"}`}, &baselineCalls)
	defer baseServer.Close()
	strongServer := serve([]string{`{"javascriptCode":"final('Done',{});"}`}, &strongCalls)
	defer strongServer.Close()
	t.Setenv("HARNESS_CONTEXT_KEY", "synthetic")
	t.Setenv("HARNESS_BASE_KEY", "synthetic")
	t.Setenv("HARNESS_STRONG_KEY", "synthetic")
	raw, _ := json.Marshal(routeConfig{Context: "context", Executor: "base", ExecutorEscalation: "strong", ExecutorAfterErrors: 1,
		Responder: "context", Routes: []modelRoute{
			{Key: "context", Model: "fixture", URL: contextServer.URL, APIKeyEnv: "HARNESS_CONTEXT_KEY"},
			{Key: "base", Model: "fixture", URL: baseServer.URL, APIKeyEnv: "HARNESS_BASE_KEY"},
			{Key: "strong", Model: "fixture", URL: strongServer.URL, APIKeyEnv: "HARNESS_STRONG_KEY"},
		}})
	t.Setenv("HARNESS_MODEL_ROUTES", string(raw))
	var out bytes.Buffer
	// Two agent calls and the one reserved summary call.
	code, err := run(context.Background(), strings.NewReader(`{"version":1,"run_id":"run-1","input_id":"input-1","prompt":"Complete","max_model_calls":3}`), &out)
	if code != 0 || err != nil || contextCalls != 2 || baselineCalls != 1 || strongCalls != 0 {
		t.Fatalf("model cap bypassed: code=%d err=%v routes=%d,%d,%d events=%s", code, err, contextCalls, baselineCalls, strongCalls, out.String())
	}
	var failed, terminal int
	for _, e := range events(t, &out) {
		if e.Type == "executor.step.failed" {
			failed++
		}
		if e.Type == "run.finished" && e.Result != nil && e.Result.Kind == "summary" && e.Result.Code == "model_calls_exhausted" {
			terminal++
		}
	}
	if failed != 1 || terminal != 1 {
		t.Fatalf("error and budget denial were not preserved: failed=%d terminal=%d", failed, terminal)
	}
}

func TestExecutorEscalationPolicyIsScoped(t *testing.T) {
	base := routeConfig{Context: "context", Executor: "base", Responder: "context", ExecutorEscalation: "strong", ExecutorAfterErrors: 1,
		Routes: []modelRoute{
			{Key: "context", Model: "fixture", URL: "https://context.example/v1", APIKeyEnv: "HARNESS_CONTEXT_KEY"},
			{Key: "base", Model: "fixture", URL: "https://base.example/v1", APIKeyEnv: "HARNESS_BASE_KEY"},
			{Key: "strong", Model: "fixture", URL: "https://strong.example/v1", APIKeyEnv: "HARNESS_STRONG_KEY"},
		}}
	validate := func(config routeConfig) error {
		raw, _ := json.Marshal(config)
		_, err := parseRouteConfig(string(raw))
		return err
	}
	if err := validate(base); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}
	for _, config := range []routeConfig{
		func() routeConfig { c := base; c.ExecutorEscalation = "missing"; return c }(),
		func() routeConfig { c := base; c.ExecutorAfterErrors = 0; return c }(),
		func() routeConfig { c := base; c.Responder = "base"; return c }(),
	} {
		if err := validate(config); err == nil {
			t.Fatalf("unsafe route policy was accepted: %+v", config)
		}
	}
}

func TestTransientProviderFallbackChargesEachRouteWithoutReplayingTools(t *testing.T) {
	var requested []string
	primaryCalls := 0
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, "primary")
		primaryCalls++
		if primaryCalls == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"temporary outage"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": `Answer: Done`}, "finish_reason": "stop"}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}})
	}))
	defer primary.Close()
	serve := func(alias, answer string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requested = append(requested, alias)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": answer}, "finish_reason": "stop"}},
				"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}})
		}))
	}
	secondary := serve("secondary", `{"javascriptCode":"final('Plan',{});"}`)
	defer secondary.Close()
	executor := serve("executor", `{"javascriptCode":"final('Done',{});"}`)
	defer executor.Close()
	t.Setenv("HARNESS_PRIMARY_KEY", "synthetic")
	t.Setenv("HARNESS_SECONDARY_KEY", "synthetic")
	t.Setenv("HARNESS_EXECUTOR_KEY", "synthetic")
	raw, _ := json.Marshal(routeConfig{Context: "primary", Executor: "executor", Responder: "primary",
		Fallbacks: []modelFallback{{From: "primary", To: "secondary"}}, Routes: []modelRoute{
			{Key: "primary", Model: "gpt-6-luna", URL: primary.URL, APIKeyEnv: "HARNESS_PRIMARY_KEY"},
			{Key: "secondary", Model: "gpt-6-sol", URL: secondary.URL, APIKeyEnv: "HARNESS_SECONDARY_KEY"},
			{Key: "executor", Model: "gpt-5.6-terra", URL: executor.URL, APIKeyEnv: "HARNESS_EXECUTOR_KEY"},
		}})
	t.Setenv("HARNESS_MODEL_ROUTES", string(raw))
	var out bytes.Buffer
	code, err := run(context.Background(), strings.NewReader(`{"version":1,"run_id":"run-1","input_id":"input-1","prompt":"Finish","max_cost_micros":200000}`), &out)
	if code != 0 || err != nil || strings.Join(requested, ",") != "primary,secondary,executor,primary" {
		t.Fatalf("fallback failed: code=%d err=%v routes=%v output=%s", code, err, requested, out.String())
	}
	var starts, costs []string
	var decisions int
	var terminal *agent.Result
	for _, e := range events(t, &out) {
		if e.Type == "model.request.started" {
			starts = append(starts, e.Origin)
		}
		if e.Type == "model.request.finished" && e.Usage != nil {
			costs = append(costs, fmt.Sprintf("%s:%d:%s", e.Origin, e.Usage.CostMicros, e.Usage.PricedBy))
		}
		if e.Type == "model.route.fallback" {
			decisions++
			if e.CallID != 1 || e.Name != "primary" || e.Origin != "secondary" || e.Error != "transient_provider_failure" {
				t.Fatalf("fallback receipt=%+v", e)
			}
		}
		if e.Type == "run.finished" {
			terminal = e.Result
		}
	}
	if strings.Join(starts, ",") != "primary,secondary,executor,primary" || decisions != 1 || terminal == nil ||
		terminal.Cost == nil || terminal.Cost.ChargedMicros != 3226 || terminal.Usage == nil || terminal.Usage.Coverage != "partial" {
		t.Fatalf("admission/cost receipts: starts=%v decisions=%d result=%+v", starts, decisions, terminal)
	}
	// The failed primary call keeps its 4,096-token reservation at luna's top rate.
	if got := strings.Join(costs, ","); got != "secondary:70:catalog,executor:80:catalog,primary:4:catalog" {
		t.Fatalf("per-route catalog costs=%s", got)
	}
	if terminal.Usage.CostMicros != 154 || terminal.Usage.PricedBy != "catalog" || terminal.Usage.CatalogVersion != terminal.Cost.CatalogVersion {
		t.Fatalf("run usage cost=%+v", terminal.Usage)
	}
}

func TestProviderDenialCannotFallback(t *testing.T) {
	primaryCalls, secondaryCalls := 0, 0
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		primaryCalls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"denied"}}`))
	}))
	defer primary.Close()
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondaryCalls++
		http.Error(w, "unexpected fallback", http.StatusInternalServerError)
	}))
	defer secondary.Close()
	t.Setenv("HARNESS_PRIMARY_KEY", "synthetic")
	t.Setenv("HARNESS_SECONDARY_KEY", "synthetic")
	raw, _ := json.Marshal(routeConfig{Context: "primary", Executor: "primary", Responder: "primary",
		Fallbacks: []modelFallback{{From: "primary", To: "secondary"}}, Routes: []modelRoute{
			{Key: "primary", Model: "fixture", URL: primary.URL, APIKeyEnv: "HARNESS_PRIMARY_KEY"},
			{Key: "secondary", Model: "fixture", URL: secondary.URL, APIKeyEnv: "HARNESS_SECONDARY_KEY"},
		}})
	t.Setenv("HARNESS_MODEL_ROUTES", string(raw))
	var out bytes.Buffer
	code, err := run(context.Background(), strings.NewReader(request), &out)
	if code != 1 || err != nil || primaryCalls != 1 || secondaryCalls != 0 || strings.Contains(out.String(), "model.route.fallback") {
		t.Fatalf("denial rerouted: code=%d err=%v primary=%d secondary=%d output=%s", code, err, primaryCalls, secondaryCalls, out.String())
	}
}

func TestProviderFallbackCannotBypassModelCallCeiling(t *testing.T) {
	primaryCalls, secondaryCalls := 0, 0
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		primaryCalls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer primary.Close()
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondaryCalls++
		http.Error(w, "unexpected fallback dispatch", http.StatusInternalServerError)
	}))
	defer secondary.Close()
	t.Setenv("HARNESS_PRIMARY_KEY", "synthetic")
	t.Setenv("HARNESS_SECONDARY_KEY", "synthetic")
	raw, _ := json.Marshal(routeConfig{Context: "primary", Executor: "primary", Responder: "primary",
		Fallbacks: []modelFallback{{From: "primary", To: "secondary"}}, Routes: []modelRoute{
			{Key: "primary", Model: "fixture", URL: primary.URL, APIKeyEnv: "HARNESS_PRIMARY_KEY"},
			{Key: "secondary", Model: "fixture", URL: secondary.URL, APIKeyEnv: "HARNESS_SECONDARY_KEY"},
		}})
	t.Setenv("HARNESS_MODEL_ROUTES", string(raw))
	var out bytes.Buffer
	code, err := run(context.Background(), strings.NewReader(`{"version":1,"run_id":"run-1","input_id":"input-1","prompt":"Answer","max_model_calls":1}`), &out)
	if code != 1 || err != nil || primaryCalls != 1 || secondaryCalls != 0 {
		t.Fatalf("model ceiling bypassed: code=%d err=%v primary=%d secondary=%d output=%s", code, err, primaryCalls, secondaryCalls, out.String())
	}
	var starts, fallback, denied int
	for _, event := range events(t, &out) {
		if event.Type == "model.request.started" {
			starts++
		}
		if event.Type == "model.route.fallback" {
			fallback++
		}
		if event.Type == "run.finished" && event.Result != nil && event.Result.Code == "model_budget_exceeded" {
			denied++
		}
	}
	if starts != 1 || fallback != 1 || denied != 1 {
		t.Fatalf("incorrect fallback admission receipts: starts=%d fallback=%d denied=%d", starts, fallback, denied)
	}
}

func TestFallbackPolicyAllowsOnlyApprovedStageRoutes(t *testing.T) {
	base := routeConfig{Context: "primary", Executor: "primary", Responder: "primary",
		Fallbacks: []modelFallback{{From: "primary", To: "spare"}}, Routes: []modelRoute{
			{Key: "primary", Model: "fixture", URL: "https://primary.example/v1", APIKeyEnv: "HARNESS_PRIMARY_KEY"},
			{Key: "spare", Model: "fixture", URL: "https://spare.example/v1", APIKeyEnv: "HARNESS_SPARE_KEY"},
			{Key: "unused", Model: "fixture", URL: "https://unused.example/v1", APIKeyEnv: "HARNESS_UNUSED_KEY"},
		}}
	validate := func(config routeConfig) error {
		raw, _ := json.Marshal(config)
		_, err := parseRouteConfig(string(raw))
		return err
	}
	if err := validate(base); err != nil {
		t.Fatalf("valid fallback rejected: %v", err)
	}
	for _, pair := range []modelFallback{
		{From: "unused", To: "spare"}, {From: "primary", To: "missing"}, {From: "primary", To: "primary"},
	} {
		invalid := base
		invalid.Fallbacks = []modelFallback{pair}
		if err := validate(invalid); err == nil {
			t.Fatalf("unsafe fallback accepted: %+v", pair)
		}
	}
}

func TestRejectCallerRoutingAndUnapprovedStage(t *testing.T) {
	t.Setenv("HARNESS_MODEL_ROUTES", `{"context":"small","executor":"work","responder":"work","routes":[{"key":"small","model":"fixture","url":"http://127.0.0.1:1","api_key_env":"HARNESS_CHEAP_KEY"}]}`)
	t.Setenv("HARNESS_CHEAP_KEY", "synthetic")
	var out bytes.Buffer
	code, err := run(context.Background(), strings.NewReader(request), &out)
	if code != 2 || err == nil || out.Len() != 0 {
		t.Fatalf("unapproved route admitted: code=%d err=%v", code, err)
	}
	code, err = run(context.Background(), strings.NewReader(`{"version":1,"run_id":"run-1","input_id":"input-1","prompt":"Hi","host_routing_digest":"other"}`), &out)
	if code != 2 || err == nil || out.Len() != 0 {
		t.Fatalf("caller route admitted: code=%d err=%v", code, err)
	}
}

func TestCostBudgetNeedsCatalogPriceForEveryRoute(t *testing.T) {
	t.Setenv("HARNESS_CHEAP_KEY", "synthetic")
	t.Setenv("HARNESS_WORK_KEY", "synthetic")
	raw, _ := json.Marshal(routeConfig{Context: "cheap", Executor: "work", Responder: "work", Routes: []modelRoute{
		{Key: "cheap", Model: "gpt-6-luna", URL: "http://127.0.0.1:1/v1", APIKeyEnv: "HARNESS_CHEAP_KEY"},
		{Key: "work", Model: "uncatalogued-model", URL: "http://127.0.0.1:1/v1", APIKeyEnv: "HARNESS_WORK_KEY"},
	}})
	t.Setenv("HARNESS_MODEL_ROUTES", string(raw))
	client, err := loadModelClient(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	routed := client.(*agent.RoutedClient)
	if _, ok := routed.Prices["cheap"]; !ok || len(routed.Prices) != 1 {
		t.Fatalf("catalog prices=%+v", routed.Prices)
	}
	var out bytes.Buffer
	code, err := run(context.Background(), strings.NewReader(`{"version":1,"run_id":"run-1","input_id":"input-1","prompt":"Answer","max_cost_micros":50000}`), &out)
	if code != 1 || err == nil || !strings.Contains(err.Error(), "catalog price") || strings.Contains(out.String(), "model.request") {
		t.Fatalf("unpriced route admitted under a cost budget: code=%d err=%v", code, err)
	}
	t.Setenv("HARNESS_MODEL_ROUTES", "")
	t.Setenv("HARNESS_MODEL_PROVIDER", "anthropic")
	t.Setenv("HARNESS_MODEL", "claude-sonnet-5-5")
	t.Setenv("HARNESS_MODEL_API_KEY", "synthetic")
	if client, err = loadModelClient(os.Getenv); err != nil {
		t.Fatal(err)
	}
	if _, ok := client.(*agent.RoutedClient).Prices["anthropic"]; !ok {
		t.Fatal("the single-model route has no catalog price")
	}
}

func TestCostAdmissionStopsBeforeNextModelDispatch(t *testing.T) {
	t.Setenv("HARNESS_TEST_KEY", "synthetic")
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": `{"javascriptCode":"final('Answer',{})"}`}, "finish_reason": "stop"}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}})
	}))
	defer server.Close()
	raw, _ := json.Marshal(routeConfig{Context: "only", Executor: "only", Responder: "only", Routes: []modelRoute{
		{Key: "only", Model: "gpt-6-luna", URL: server.URL, APIKeyEnv: "HARNESS_TEST_KEY"},
	}})
	t.Setenv("HARNESS_MODEL_ROUTES", string(raw))
	// A 4,096-token reservation at luna's top rate is 3,072 micros; the observed call costs 4.
	input := `{"version":1,"run_id":"run-1","input_id":"input-1","prompt":"Answer","max_cost_micros":3075}`
	var out bytes.Buffer
	code, err := run(context.Background(), strings.NewReader(input), &out)
	if code != 1 || err != nil || requests != 1 {
		t.Fatalf("cost gate: code=%d err=%v requests=%d output=%s", code, err, requests, out.String())
	}
	var started, finished int
	for _, event := range events(t, &out) {
		if event.Type == "model.request.started" {
			started++
			if event.CostMicros == nil || *event.CostMicros != 3072 {
				t.Fatalf("reservation: %+v", event)
			}
		}
		if event.Type == "model.request.finished" {
			finished++
			if event.CostMicros == nil || *event.CostMicros != 4 || event.Usage == nil || event.Usage.CostMicros != 4 {
				t.Fatalf("observed charge: %+v", event)
			}
		}
		if event.Type == "run.finished" && (event.Result == nil || event.Result.Code != "cost_budget_exceeded" ||
			event.Result.Cost == nil || event.Result.Cost.ChargedMicros != 4 || event.Result.Cost.CatalogVersion != event.Result.Usage.CatalogVersion) {
			t.Fatalf("terminal cost: %+v", event.Result)
		}
	}
	if started != 1 || finished != 1 {
		t.Fatalf("model starts=%d finishes=%d", started, finished)
	}
}

func TestMissingUsageRetainsCostReservation(t *testing.T) {
	t.Setenv("HARNESS_TEST_KEY", "synthetic")
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": `{"javascriptCode":"final('Answer',{})"}`}, "finish_reason": "stop"}}})
	}))
	defer server.Close()
	raw, _ := json.Marshal(routeConfig{Context: "only", Executor: "only", Responder: "only", Routes: []modelRoute{
		{Key: "only", Model: "gpt-6-luna", URL: server.URL, APIKeyEnv: "HARNESS_TEST_KEY"},
	}})
	t.Setenv("HARNESS_MODEL_ROUTES", string(raw))
	var out bytes.Buffer
	code, err := run(context.Background(), strings.NewReader(`{"version":1,"run_id":"run-1","input_id":"input-1","prompt":"Answer","max_cost_micros":3075}`), &out)
	if code != 1 || err != nil || requests != 1 {
		t.Fatalf("missing usage: code=%d err=%v requests=%d output=%s", code, err, requests, out.String())
	}
	var terminal *agent.Result
	for _, event := range events(t, &out) {
		if event.Type == "run.finished" {
			terminal = event.Result
		}
	}
	if terminal == nil || terminal.Code != "cost_budget_exceeded" || terminal.Cost == nil || terminal.Cost.ChargedMicros != 3072 ||
		terminal.Usage == nil || terminal.Usage.Coverage != "unavailable" {
		t.Fatalf("reservation or coverage lost: %+v", terminal)
	}
}
