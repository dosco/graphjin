package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

const orgPrompt = "<role>Org-wide instructions, playbook and data notes.</role>"

var cacheTurnReplies = []string{
	`{"javascriptCode":"final('Read the data',{})"}`,
	`{"javascriptCode":"const a=alpha({}); console.log(a)"}`,
	`{"javascriptCode":"const b=beta({}); console.log(b)"}`,
	`{"javascriptCode":"final('Answer',{})"}`,
}

func cacheTools(reversed bool, match string) Tools {
	result := func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"ok":true}`), nil
	}
	capabilities := []Capability{
		{Name: "alpha", Version: "1", Origin: "mcp", Effect: "read", Description: "Read alpha.", InputSchema: json.RawMessage(`{"type":"object"}`), Call: result},
		{Name: "beta", Version: "1", Origin: "mcp", Effect: "durable", Description: "Write beta.", InputSchema: json.RawMessage(`{"type":"object"}`), Call: result},
		{Name: SkillSearchTool, Version: "1", Origin: "mcp", Effect: "read", Description: "Find skills by meaning.", InputSchema: json.RawMessage(`{"type":"object"}`),
			Call: func(context.Context, json.RawMessage) (json.RawMessage, error) {
				return json.RawMessage(`{"content":["{\"skills\":[{\"name\":\"` + match + `\",\"score\":0.7}]}"]}`), nil
			}},
	}
	skills := []Skill{{Name: "churn", Description: "Find churned accounts", Content: "c"}, {Name: "daily-lead", Description: "Build the daily lead report", Content: "d"}}
	if reversed {
		for i, j := 0, len(capabilities)-1; i < j; i, j = i+1, j-1 {
			capabilities[i], capabilities[j] = capabilities[j], capabilities[i]
		}
		skills[0], skills[1] = skills[1], skills[0]
	}
	return Tools{Capabilities: capabilities, Skills: skills}
}

var cacheMessages = []string{"Make the daily lead report", "Who churned last week?"}

// cacheSpec sends the org prompt in instructions when split is set, and the
// old combined prompt otherwise.
func cacheSpec(turn int, split bool) Spec {
	message := cacheMessages[turn]
	spec := Spec{Version: 1, RunID: fmt.Sprintf("run-%d", turn), InputID: fmt.Sprintf("input-%d", turn), Prompt: orgPrompt + "\n\nCurrent user message:\n" + message, SkillQuery: message}
	if split {
		spec.Instructions, spec.Prompt = orgPrompt, "Current user message:\n"+message
	}
	return spec
}

func cacheTurn(t *testing.T, client func(*httptest.Server) ax.AIClient, server func(*testing.T, []string, *[]string, *sync.Mutex) *httptest.Server, turn int, replies []string, split bool) ([]string, Result) {
	t.Helper()
	var mu sync.Mutex
	var bodies []string
	match := []string{"daily-lead", "churn"}[turn]
	result, err := RunWithTools(context.Background(), cacheSpec(turn, split), client(server(t, replies, &bodies, &mu)), cacheTools(turn == 1, match), func(Event) error { return nil })
	if err != nil || result.Status != "completed" {
		t.Fatalf("turn %d: result=%+v err=%v", turn, result, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != len(replies) {
		t.Fatalf("turn %d made %d model calls, want %d", turn, len(bodies), len(replies))
	}
	return append([]string(nil), bodies...), result
}

// chatParts returns the system prompt and the first user message of an
// OpenAI-style request.
func chatParts(t *testing.T, body string) (string, string) {
	t.Helper()
	var request struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &request); err != nil || len(request.Messages) < 2 || request.Messages[0].Role != "system" || request.Messages[1].Role != "user" {
		t.Fatalf("unexpected request shape: %s", body)
	}
	return request.Messages[0].Content, request.Messages[1].Content
}

func TestPromptPrefixIsByteStableAcrossStepsAndTurns(t *testing.T) {
	replies := append(append([]string(nil), cacheTurnReplies...), "Answer: done")
	turns := make([][]string, 2)
	for turn := range turns {
		message, match := []string{"Make the daily lead report", "Who churned last week?"}[turn], []string{"daily-lead", "churn"}[turn]
		model := &scripted{replies: replies}
		spec := Spec{Version: 1, RunID: fmt.Sprintf("run-%d", turn), InputID: fmt.Sprintf("input-%d", turn), Prompt: orgPrompt + "\n\nCurrent user message:\n" + message, SkillQuery: message}
		result, err := RunWithTools(context.Background(), spec, openAIClient(model.server(t)), cacheTools(turn == 1, match), func(Event) error { return nil })
		bodies := model.bodies
		if err != nil || result.Status != "completed" || len(bodies) != len(replies) {
			t.Fatalf("turn %d: result=%+v err=%v calls=%d", turn, result, err, len(bodies))
		}
		turns[turn] = bodies
	}
	prefix := "Question: " + orgPrompt + "\n\nCurrent user message:\n"
	for turn, bodies := range turns {
		executorSystem, _ := chatParts(t, bodies[1])
		for step := 1; step <= 3; step++ {
			system, user := chatParts(t, bodies[step])
			if system != executorSystem {
				t.Fatalf("turn %d executor step %d changed the system prompt", turn, step)
			}
			if !strings.HasPrefix(user, prefix) {
				t.Fatalf("turn %d executor step %d does not start with the org prompt: %q", turn, step, user[:min(len(user), 200)])
			}
		}
	}
	for _, step := range []int{0, 1, 4} {
		first, firstUser := chatParts(t, turns[0][step])
		second, secondUser := chatParts(t, turns[1][step])
		if first != second {
			t.Fatalf("stage of call %d has a different system prompt across turns", step)
		}
		if !strings.HasPrefix(firstUser, prefix) || !strings.HasPrefix(secondUser, prefix) {
			t.Fatalf("stage of call %d moved the org prompt out of the prefix", step)
		}
	}
	system, _ := chatParts(t, turns[0][1])
	if alpha, beta := strings.Index(system, "function alpha("), strings.Index(system, "function beta("); alpha < 0 || beta < alpha {
		t.Fatalf("tools are not in name order: alpha=%d beta=%d", alpha, beta)
	}
	for _, volatile := range []string{"run-0", "input-0", "daily-lead", "churn"} {
		if strings.Contains(system, volatile) {
			t.Fatalf("system prompt holds per-turn value %q", volatile)
		}
	}
}

func TestToolAndSkillOrderIsDeterministic(t *testing.T) {
	first, err := cacheTools(false, "x").admitted()
	if err != nil {
		t.Fatal(err)
	}
	second, _ := cacheTools(true, "x").admitted()
	firstSkills, _ := cacheTools(false, "x").skills()
	secondSkills, _ := cacheTools(true, "x").skills()
	describe := func(tools []admittedTool, skills []Skill) string {
		var out strings.Builder
		for _, tool := range tools {
			out.WriteString(tool.promptDescriptor(true))
		}
		for _, skill := range skills {
			out.WriteString(skill.Name + ",")
		}
		return out.String()
	}
	if describe(first, firstSkills) != describe(second, secondSkills) {
		t.Fatalf("input order changed the prompt:\n%s\n%s", describe(first, firstSkills), describe(second, secondSkills))
	}
}

func anthropicCacheServer(t *testing.T, replies []string, bodies *[]string, mu *sync.Mutex) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		index := len(*bodies)
		*bodies = append(*bodies, string(body))
		mu.Unlock()
		if index >= len(replies) {
			http.Error(w, "unexpected model call", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "m", "type": "message", "role": "assistant", "model": "claude-served",
			"content": []any{map[string]any{"type": "text", "text": replies[index]}}, "stop_reason": "end_turn",
			"usage": map[string]any{"input_tokens": 10, "cache_read_input_tokens": 900, "cache_creation_input_tokens": 50, "output_tokens": 2}})
	}))
	t.Cleanup(server.Close)
	return server
}

func TestAnthropicCacheBreakpointsAndCachedUsage(t *testing.T) {
	replies := append(append([]string(nil), cacheTurnReplies...), "Answer: done")
	routed := func(server *httptest.Server) ax.AIClient { return anthropicRouted(server) }
	turns := make([][]string, 2)
	var result Result
	for turn := range turns {
		turns[turn], result = cacheTurn(t, routed, anthropicCacheServer, turn, replies, false)
	}
	type block struct {
		Type         string          `json:"type"`
		Text         string          `json:"text"`
		CacheControl json.RawMessage `json:"cache_control"`
	}
	parse := func(body string) ([]block, []block) {
		var request struct {
			System   []block `json:"system"`
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal([]byte(body), &request); err != nil || len(request.System) == 0 || len(request.Messages) == 0 {
			t.Fatalf("unexpected request: %s", body)
		}
		var first []block
		_ = json.Unmarshal(request.Messages[0].Content, &first)
		return request.System, first
	}
	for turn, bodies := range turns {
		for step := 0; step < 4; step++ {
			system, user := parse(bodies[step])
			if string(system[len(system)-1].CacheControl) != `{"type":"ephemeral"}` {
				t.Fatalf("turn %d call %d: system has no cache breakpoint", turn, step)
			}
			if len(user) == 0 || string(user[0].CacheControl) != `{"type":"ephemeral"}` || !strings.HasPrefix(user[0].Text, "Question: "+orgPrompt) {
				t.Fatalf("turn %d call %d: the org prompt is not in a cached block: %+v", turn, step, user)
			}
		}
	}
	for _, step := range []int{0, 1} {
		first, _ := parse(turns[0][step])
		second, _ := parse(turns[1][step])
		if first[len(first)-1].Text != second[len(second)-1].Text {
			t.Fatalf("call %d: cached system block differs across turns", step)
		}
	}
	usage := result.Usage
	if usage == nil || usage.CacheReadTokens != 900*int64(len(replies)) || usage.CacheWriteTokens != 50*int64(len(replies)) || usage.InputTokens != 10*int64(len(replies)) {
		t.Fatalf("usage = %+v", usage)
	}
}

func geminiCacheServer(t *testing.T, replies []string, bodies *[]string, mu *sync.Mutex) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		index := len(*bodies)
		*bodies = append(*bodies, r.URL.Path+"\n"+string(body))
		mu.Unlock()
		if index >= len(replies) || !strings.HasSuffix(r.URL.Path, ":generateContent") {
			http.Error(w, "unexpected model call", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates":    []any{map[string]any{"content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": replies[index]}}}, "finishReason": "STOP"}},
			"usageMetadata": map[string]any{"promptTokenCount": 1000, "cachedContentTokenCount": 800, "candidatesTokenCount": 5, "totalTokenCount": 1005},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

func TestGeminiUsesImplicitCachingWithStablePrefix(t *testing.T) {
	replies := append(append([]string(nil), cacheTurnReplies...), `{"answer":"done"}`)
	routed := func(server *httptest.Server) ax.AIClient {
		client := ax.NewAI("google-gemini", ax.Object("base_url", server.URL, "api_key", "synthetic", "model", "gemini-3.8-flash", "retry", ax.Object("max_retries", 0)))
		return &RoutedClient{AIClient: client, ModelNames: map[string]string{"google-gemini": "gemini-3.8-flash"}, Providers: map[string]string{"google-gemini": "google-gemini"}, DefaultProvider: "google-gemini"}
	}
	type request struct {
		System struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"systemInstruction"`
		Contents []struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"contents"`
		CachedContent string `json:"cachedContent"`
	}
	parse := func(body string) request {
		_, payload, _ := strings.Cut(body, "\n")
		var out request
		if err := json.Unmarshal([]byte(payload), &out); err != nil || len(out.System.Parts) == 0 || len(out.Contents) == 0 {
			t.Fatalf("unexpected request: %s", body)
		}
		return out
	}
	turns := make([][]string, 2)
	var result Result
	for turn := range turns {
		turns[turn], result = cacheTurn(t, routed, geminiCacheServer, turn, replies, false)
	}
	for turn, bodies := range turns {
		executor := parse(bodies[1]).System.Parts[0].Text
		for step, body := range bodies {
			parsed := parse(body)
			if parsed.CachedContent != "" {
				t.Fatalf("turn %d call %d uses an explicit cache", turn, step)
			}
			if !strings.HasPrefix(parsed.Contents[0].Parts[0].Text, "Question: "+orgPrompt) {
				t.Fatalf("turn %d call %d: contents do not start with the org prompt", turn, step)
			}
			if step >= 1 && step <= 3 && parsed.System.Parts[0].Text != executor {
				t.Fatalf("turn %d executor step %d changed the system instruction", turn, step)
			}
		}
	}
	if parse(turns[0][1]).System.Parts[0].Text != parse(turns[1][1]).System.Parts[0].Text {
		t.Fatal("executor system instruction differs across turns")
	}
	usage := result.Usage
	if usage == nil || usage.CacheReadTokens != 800*int64(len(replies)) || usage.InputTokens != 200*int64(len(replies)) {
		t.Fatalf("usage = %+v", usage)
	}
}

func openAICacheServer(t *testing.T, replies []string, bodies *[]string, mu *sync.Mutex) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		index := len(*bodies)
		*bodies = append(*bodies, string(body))
		mu.Unlock()
		if index >= len(replies) {
			http.Error(w, "unexpected model call", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "served", "choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": replies[index]}, "finish_reason": "stop"}},
			"usage": map[string]any{"prompt_tokens": 1000, "completion_tokens": 2, "total_tokens": 1002, "prompt_tokens_details": map[string]any{"cached_tokens": 800}}})
	}))
	t.Cleanup(server.Close)
	return server
}

func TestInstructionsHaveTheirOwnAnthropicBreakpoint(t *testing.T) {
	replies := append(append([]string(nil), cacheTurnReplies...), "Answer: done")
	routed := func(server *httptest.Server) ax.AIClient { return anthropicRouted(server) }
	type block struct {
		Text         string          `json:"text"`
		CacheControl json.RawMessage `json:"cache_control"`
	}
	blocks := func(body string) ([]json.RawMessage, int) {
		var request struct {
			System   []block `json:"system"`
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal([]byte(body), &request); err != nil || len(request.System) == 0 {
			t.Fatalf("unexpected request: %s", body)
		}
		breakpoints := 0
		for _, part := range request.System {
			if part.CacheControl != nil {
				breakpoints++
			}
		}
		var out []json.RawMessage
		for _, message := range request.Messages {
			var parts []json.RawMessage
			if json.Unmarshal(message.Content, &parts) != nil {
				var text string
				_ = json.Unmarshal(message.Content, &text)
				part, _ := json.Marshal(block{Text: text})
				parts = []json.RawMessage{part}
			}
			for _, raw := range parts {
				var part block
				if json.Unmarshal(raw, &part) == nil && part.CacheControl != nil {
					breakpoints++
				}
				out = append(out, raw)
			}
		}
		return out, breakpoints
	}
	turns := make([][]string, 2)
	for turn := range turns {
		turns[turn], _ = cacheTurn(t, routed, anthropicCacheServer, turn, replies, true)
	}
	for step := range replies {
		first, _ := blocks(turns[0][step])
		second, _ := blocks(turns[1][step])
		if string(first[0]) != string(second[0]) {
			t.Fatalf("call %d: the org prompt block differs across turns:\n%s\n%s", step, first[0], second[0])
		}
		for turn, parts := range [][]json.RawMessage{first, second} {
			var head, next block
			_ = json.Unmarshal(parts[0], &head)
			_ = json.Unmarshal(parts[1], &next)
			if head.Text != instructionsLabel+orgPrompt || string(head.CacheControl) != `{"type":"ephemeral"}` {
				t.Fatalf("turn %d call %d: the org prompt is not a cached block of its own: %s", turn, step, parts[0])
			}
			if !strings.HasPrefix(next.Text, "Question: Current user message:\n"+cacheMessages[turn]) {
				t.Fatalf("turn %d call %d: the user message does not follow the org prompt: %q", turn, step, next.Text)
			}
			if _, breakpoints := blocks(turns[turn][step]); breakpoints > 4 {
				t.Fatalf("turn %d call %d has %d cache breakpoints", turn, step, breakpoints)
			}
		}
	}
}

func TestOpenAIBreakpointCoversInstructions(t *testing.T) {
	replies := append(append([]string(nil), cacheTurnReplies...), "Answer: done")
	routed := func(server *httptest.Server) ax.AIClient {
		client := ax.NewAI("openai", ax.Object("base_url", server.URL, "api_key", "synthetic", "model", "gpt-5.6", "retry", ax.Object("max_retries", 0)))
		return &RoutedClient{AIClient: client, ModelNames: map[string]string{"openai": "gpt-5.6"}, Providers: map[string]string{"openai": "openai"}, DefaultProvider: "openai"}
	}
	type request struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		Options json.RawMessage `json:"prompt_cache_options"`
	}
	parse := func(body string) request {
		var out request
		if err := json.Unmarshal([]byte(body), &out); err != nil || len(out.Messages) < 3 || out.Messages[1].Role != "user" {
			t.Fatalf("unexpected request: %s", body)
		}
		return out
	}
	turns := make([][]string, 2)
	var result Result
	for turn := range turns {
		turns[turn], result = cacheTurn(t, routed, openAICacheServer, turn, replies, true)
	}
	for step := range replies {
		first, second := parse(turns[0][step]), parse(turns[1][step])
		if string(first.Messages[1].Content) != string(second.Messages[1].Content) {
			t.Fatalf("call %d: the org prompt message differs across turns", step)
		}
		var parts []struct {
			Text       string          `json:"text"`
			Breakpoint json.RawMessage `json:"prompt_cache_breakpoint"`
		}
		if err := json.Unmarshal(first.Messages[1].Content, &parts); err != nil || len(parts) != 1 ||
			parts[0].Text != instructionsLabel+orgPrompt || string(parts[0].Breakpoint) != `{"mode":"explicit"}` {
			t.Fatalf("call %d: the org prompt has no breakpoint of its own: %s", step, first.Messages[1].Content)
		}
		if !strings.Contains(string(first.Options), `"explicit"`) {
			t.Fatalf("call %d: no prompt_cache_options: %s", step, turns[0][step])
		}
	}
	if usage := result.Usage; usage == nil || usage.CacheReadTokens != 800*int64(len(replies)) {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func TestInstructionsKeepAStablePrefixForImplicitCaches(t *testing.T) {
	gemini := func(server *httptest.Server) ax.AIClient {
		client := ax.NewAI("google-gemini", ax.Object("base_url", server.URL, "api_key", "synthetic", "model", "gemini-3.8-flash", "retry", ax.Object("max_retries", 0)))
		return &RoutedClient{AIClient: client, ModelNames: map[string]string{"google-gemini": "gemini-3.8-flash"}, Providers: map[string]string{"google-gemini": "google-gemini"}, DefaultProvider: "google-gemini"}
	}
	geminiParts := func(body string) (string, string) {
		_, payload, _ := strings.Cut(body, "\n")
		var request struct {
			System struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"systemInstruction"`
			Contents []struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"contents"`
			CachedContent string `json:"cachedContent"`
		}
		if err := json.Unmarshal([]byte(payload), &request); err != nil || len(request.Contents) == 0 || request.CachedContent != "" {
			t.Fatalf("unexpected request: %s", body)
		}
		return request.System.Parts[0].Text, request.Contents[0].Parts[0].Text
	}
	prefix := instructionsLabel + orgPrompt + "\n\nQuestion: Current user message:\n"
	for _, provider := range []struct {
		name   string
		client func(*httptest.Server) ax.AIClient
		server func(*testing.T, []string, *[]string, *sync.Mutex) *httptest.Server
		parts  func(string) (string, string)
		answer string
	}{
		{"gemini", gemini, geminiCacheServer, geminiParts, `{"answer":"done"}`},
		{"compatible", openAIClient, openAICacheServer, func(body string) (string, string) { return chatParts(t, body) }, "Answer: done"},
	} {
		replies := append(append([]string(nil), cacheTurnReplies...), provider.answer)
		turns := make([][]string, 2)
		for turn := range turns {
			turns[turn], _ = cacheTurn(t, provider.client, provider.server, turn, replies, true)
		}
		for step := range replies {
			firstSystem, firstUser := provider.parts(turns[0][step])
			secondSystem, secondUser := provider.parts(turns[1][step])
			if firstSystem != secondSystem {
				t.Fatalf("%s call %d: the system prompt differs across turns", provider.name, step)
			}
			if !strings.HasPrefix(firstUser, prefix+cacheMessages[0]) || !strings.HasPrefix(secondUser, prefix+cacheMessages[1]) {
				t.Fatalf("%s call %d: the prompt does not start with the org prompt: %q", provider.name, step, firstUser[:min(len(firstUser), 200)])
			}
		}
	}
}
