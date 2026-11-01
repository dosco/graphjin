package command

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNativeProviderRoutesParse(t *testing.T) {
	for _, provider := range []string{"anthropic", "google-gemini", "openai"} {
		raw, _ := json.Marshal(routeConfig{Context: "main", Executor: "main", Responder: "main",
			Routes: []modelRoute{{Key: "main", Provider: provider, Model: "model-x", APIKeyEnv: "HARNESS_MAIN_KEY"}}})
		t.Setenv("HARNESS_MAIN_KEY", "synthetic")
		t.Setenv("HARNESS_MODEL_ROUTES", string(raw))
		client, err := loadModelClient(func(name string) string {
			return map[string]string{"HARNESS_MODEL_ROUTES": string(raw), "HARNESS_MAIN_KEY": "synthetic"}[name]
		})
		if err != nil || client == nil {
			t.Fatalf("%s route without a URL was rejected: %v", provider, err)
		}
	}
	raw, _ := json.Marshal(routeConfig{Context: "main", Executor: "main", Responder: "main",
		Routes: []modelRoute{{Key: "main", Provider: "no-such-provider", Model: "model-x", APIKeyEnv: "HARNESS_MAIN_KEY"}}})
	if _, err := loadModelClient(func(name string) string {
		return map[string]string{"HARNESS_MODEL_ROUTES": string(raw), "HARNESS_MAIN_KEY": "synthetic"}[name]
	}); err == nil {
		t.Fatal("an unknown provider was accepted")
	}
	raw, _ = json.Marshal(routeConfig{Context: "main", Executor: "main", Responder: "main",
		Routes: []modelRoute{{Key: "main", Model: "model-x", APIKeyEnv: "HARNESS_MAIN_KEY"}}})
	if _, err := parseRouteConfig(string(raw)); err == nil {
		t.Fatal("an OpenAI-compatible route without a URL was accepted")
	}
}

func anthropicReply(text string) map[string]any {
	return map[string]any{"id": "m", "type": "message", "role": "assistant", "model": "claude-served",
		"content": []any{map[string]any{"type": "text", "text": text}}, "stop_reason": "end_turn",
		"usage": map[string]any{"input_tokens": 3, "output_tokens": 2}}
}

func TestAnthropicRouteAnswersThroughItsURL(t *testing.T) {
	replies := []string{`{"javascriptCode":"final('Answer',{})"}`, `{"javascriptCode":"final('Answer',{})"}`, `Answer: REF-42`}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		index := int(calls.Add(1)) - 1
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "synthetic" || index >= len(replies) {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(anthropicReply(replies[index]))
	}))
	defer server.Close()
	t.Setenv("HARNESS_MODEL_PROVIDER", "anthropic")
	t.Setenv("HARNESS_MODEL_URL", server.URL)
	t.Setenv("HARNESS_MODEL", "claude-x")
	t.Setenv("HARNESS_MODEL_API_KEY", "synthetic")
	var out bytes.Buffer
	code, err := run(context.Background(), strings.NewReader(request), &out)
	if code != 0 || err != nil || calls.Load() != 3 {
		t.Fatalf("code=%d err=%v calls=%d out=%s", code, err, calls.Load(), out.String())
	}
	es := events(t, &out)
	if es[len(es)-1].Result.Answer != "REF-42" {
		t.Fatalf("answer=%+v", es[len(es)-1].Result)
	}
	for _, e := range es {
		if e.Type == "model.request.finished" && (e.Provider != "anthropic" || e.ObservedModel != "claude-served") {
			t.Fatalf("model receipt lacks identity: %+v", e)
		}
	}
}

func TestNativeClientDoesNotRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"overloaded_error","message":"busy"}}`)
	}))
	defer server.Close()
	t.Setenv("HARNESS_MODEL_PROVIDER", "anthropic")
	t.Setenv("HARNESS_MODEL_URL", server.URL)
	t.Setenv("HARNESS_MODEL", "claude-x")
	t.Setenv("HARNESS_MODEL_API_KEY", "synthetic")
	var out bytes.Buffer
	code, err := run(context.Background(), strings.NewReader(request), &out)
	if code != 1 || err != nil || calls.Load() != 1 {
		t.Fatalf("code=%d err=%v calls=%d", code, err, calls.Load())
	}
}

func TestKeyEnvNamesListsEveryCredential(t *testing.T) {
	raw, _ := json.Marshal(routeConfig{Context: "a", Executor: "a", Responder: "a", Routes: []modelRoute{
		{Key: "a", Model: "m", URL: "https://a.example/v1", APIKeyEnv: "HARNESS_A_KEY"},
		{Key: "b", Provider: "anthropic", Model: "m", APIKeyEnv: "HARNESS_B_KEY"}}})
	names := strings.Join(KeyEnvNames(func(name string) string {
		if name == "HARNESS_MODEL_ROUTES" {
			return string(raw)
		}
		return ""
	}), ",")
	if names != "HARNESS_MODEL_API_KEY,HARNESS_MODEL_ROUTES,HARNESS_A_KEY,HARNESS_B_KEY" {
		t.Fatalf("names=%s", names)
	}
}

func openAIChatReply(text string, stream bool) string {
	if stream {
		chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": text}}}})
		done, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}})
		return "data: " + string(chunk) + "\n\ndata: " + string(done) + "\n\ndata: [DONE]\n\n"
	}
	body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": text}, "finish_reason": "stop"}}})
	return string(body)
}

// Each OpenAI-chat provider keeps its own Ax profile: its auth header and its
// path. With a URL, Azure uses Ax's default API version; without one, Ax
// builds the endpoint from resource_name, deployment_name and api_version.
func TestNativeChatProfilesReachTheirEndpoint(t *testing.T) {
	cases := []struct {
		provider, path, query, header, value string
		options                              string
	}{
		{"azure-openai", "/openai/deployments/sales/chat/completions", "api-version=2024-02-15-preview", "api-key", "synthetic", ""},
		{"groq", "/openai/v1/chat/completions", "", "Authorization", "Bearer synthetic", ""},
		{"mistral", "/v1/chat/completions", "", "Authorization", "Bearer synthetic", ""},
	}
	for _, c := range cases {
		t.Run(c.provider, func(t *testing.T) {
			replies := []string{`{"javascriptCode":"final('Answer',{})"}`, `{"javascriptCode":"final('Answer',{})"}`, `Answer: REF-7`}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				index := int(calls.Add(1)) - 1
				if r.URL.Path != c.path || r.URL.RawQuery != c.query || r.Header.Get(c.header) != c.value || index >= len(replies) {
					t.Errorf("request %s?%s %s=%q", r.URL.Path, r.URL.RawQuery, c.header, r.Header.Get(c.header))
					http.Error(w, "unexpected request", http.StatusBadRequest)
					return
				}
				stream := strings.Contains(string(body), `"stream":true`)
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
				} else {
					w.Header().Set("Content-Type", "application/json")
				}
				_, _ = io.WriteString(w, openAIChatReply(replies[index], stream))
			}))
			defer server.Close()
			base := server.URL + strings.TrimSuffix(c.path, "/chat/completions")
			t.Setenv("HARNESS_MODEL_PROVIDER", c.provider)
			t.Setenv("HARNESS_MODEL_URL", base)
			t.Setenv("HARNESS_MODEL", "sales")
			t.Setenv("HARNESS_MODEL_API_KEY", "synthetic")
			t.Setenv("HARNESS_MODEL_OPTIONS", c.options)
			var out bytes.Buffer
			code, err := run(context.Background(), strings.NewReader(request), &out)
			es := events(t, &out)
			if code != 0 || err != nil || es[len(es)-1].Result.Answer != "REF-7" {
				t.Fatalf("code=%d err=%v out=%s", code, err, out.String())
			}
		})
	}
}

func TestModelOptionsCannotCarryTheKeyOrEndpoint(t *testing.T) {
	for _, raw := range []string{`{"api_key":"x"}`, `{"base_url":"https://evil.example"}`, `{"model":"other"}`, `{"bad key":"x"}`, `not json`} {
		_, err := loadModelClient(func(name string) string {
			return map[string]string{"HARNESS_MODEL_PROVIDER": "groq", "HARNESS_MODEL": "m", "HARNESS_MODEL_API_KEY": "k", "HARNESS_MODEL_OPTIONS": raw}[name]
		})
		if err == nil {
			t.Fatalf("options %s accepted", raw)
		}
	}
}

func TestAzureRouteBuildsItsEndpointFromOptions(t *testing.T) {
	client, err := loadModelClient(func(name string) string {
		return map[string]string{"HARNESS_MODEL_PROVIDER": "azure-openai", "HARNESS_MODEL": "sales", "HARNESS_MODEL_API_KEY": "k",
			"HARNESS_MODEL_OPTIONS": `{"resource_name":"acme","deployment_name":"sales","api_version":"2024-10-21"}`}[name]
	})
	if err != nil || client == nil {
		t.Fatalf("azure route from options rejected: %v", err)
	}
	if _, err := loadModelClient(func(name string) string {
		return map[string]string{"HARNESS_MODEL_PROVIDER": "azure-openai", "HARNESS_MODEL": "sales", "HARNESS_MODEL_API_KEY": "k"}[name]
	}); err == nil {
		t.Fatal("azure route without an endpoint accepted")
	}
}
