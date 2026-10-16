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

// anthropicServer answers actor calls with Messages JSON and streams the last
// reply as Messages SSE with a thinking block.
func anthropicServer(t *testing.T, replies []string, bodies *[]string, mu *sync.Mutex) *httptest.Server {
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
		if strings.Contains(string(body), `"stream":true`) {
			w.Header().Set("Content-Type", "text/event-stream")
			send := func(event, data string) { _, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data) }
			send("message_start", `{"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"claude-served","content":[],"usage":{"input_tokens":3,"output_tokens":0}}}`)
			send("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`)
			send("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Checking the data."}}`)
			send("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig"}}`)
			send("content_block_stop", `{"type":"content_block_stop","index":0}`)
			send("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`)
			text, _ := json.Marshal(replies[index])
			send("content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":%s}}`, text))
			send("content_block_stop", `{"type":"content_block_stop","index":1}`)
			send("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}`)
			send("message_stop", `{"type":"message_stop"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "m", "type": "message", "role": "assistant", "model": "claude-served",
			"content": []any{map[string]any{"type": "text", "text": replies[index]}}, "stop_reason": "end_turn",
			"usage": map[string]any{"input_tokens": 3, "output_tokens": 2}})
	}))
	t.Cleanup(server.Close)
	return server
}

func anthropicRouted(server *httptest.Server) *RoutedClient {
	client := ax.NewAI("anthropic", ax.Object("base_url", server.URL, "api_key", "synthetic", "model", "claude-x", "retry", ax.Object("max_retries", 0)))
	return &RoutedClient{AIClient: client, ModelNames: map[string]string{"anthropic": "claude-x"}, Providers: map[string]string{"anthropic": "anthropic"}, DefaultProvider: "anthropic"}
}

func TestAnthropicRunThinksCachesAndStreamsThoughts(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	server := anthropicServer(t, []string{`{"javascriptCode":"final('Check the data',{})"}`, `{"javascriptCode":"final('Answer',{})"}`, `Answer: done`}, &bodies, &mu)
	var thoughts, steps []string
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "anthropic", InputID: "i", Prompt: "p", ReasoningEffort: "low", StreamResponses: true},
		anthropicRouted(server), Tools{}, func(e Event) error {
			var data struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(e.Data, &data)
			switch e.Type {
			case "thought.delta":
				thoughts = append(thoughts, data.Text)
			case "actor.step":
				steps = append(steps, data.Text)
			}
			if (e.Type == "thought.delta" || e.Type == "actor.step") && e.Sequence != 0 {
				t.Errorf("live event advanced the sequence: %+v", e)
			}
			return nil
		})
	if err != nil || result.Status != "completed" || result.Answer != "done" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	mu.Lock()
	defer mu.Unlock()
	for i, body := range bodies {
		if !strings.Contains(body, `"thinking":{"type":"enabled"`) || !strings.Contains(body, `"cache_control":{"type":"ephemeral"}`) {
			t.Fatalf("request %d lacks thinking or cache_control: %s", i, body)
		}
	}
	if strings.Join(thoughts, "") != "Checking the data." || len(steps) == 0 || steps[0] != "Check the data" {
		t.Fatalf("thoughts=%q steps=%q", thoughts, steps)
	}
}

func TestSkillsCatalogLoadsAndReportsUse(t *testing.T) {
	model := &scripted{replies: []string{
		`{"javascriptCode":"await discover({skills:['daily-lead']}); console.log('loaded')"}`,
		`{"javascriptCode":"final('Follow the daily lead guide',{})"}`,
		`{"javascriptCode":"await used('daily-lead','followed the guide'); console.log('noted')"}`,
		`{"javascriptCode":"final('Done',{})"}`,
		`Answer: done`,
	}}
	server := model.server(t)
	var used, loaded []string
	skills := []Skill{{Name: "daily-lead", Description: "Build the daily lead report", Content: "Step 1: read the leads table."}}
	for i := 0; i < 1000; i++ {
		skills = append(skills, Skill{Name: fmt.Sprintf("other-%04d", i), Description: "Unrelated guide", Load: func() (string, error) {
			t.Error("a guide that did not match was read")
			return "", nil
		}})
	}
	var queries []string
	index := Capability{Name: SkillSearchTool, Version: "1", Origin: "mcp", Effect: "read", Description: "Find skills by meaning.",
		InputSchema: json.RawMessage(`{"type":"object"}`), Call: func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
			var input struct {
				Query string `json:"query"`
			}
			_ = json.Unmarshal(raw, &input)
			queries = append(queries, input.Query)
			return json.RawMessage(`{"content":["{\"skills\":[{\"name\":\"daily-lead\",\"score\":0.7}]}"]}`), nil
		}}
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "skills", InputID: "i", Prompt: "Make the daily lead report", SkillQuery: "Make the daily lead report"}, openAIClient(server),
		Tools{Capabilities: []Capability{index}, Skills: skills},
		func(e Event) error {
			switch e.Type {
			case "skill.used":
				used = append(used, e.Name)
			case "skill.loaded":
				loaded = append(loaded, e.Name)
			}
			return nil
		})
	if err != nil || result.Status != "completed" {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, model.calls())
	}
	if strings.Contains(model.body(0), "other-0001") {
		t.Fatal("the skills catalog was listed in the prompt")
	}
	if strings.Contains(model.body(0), SkillSearchTool) {
		t.Fatal("the skill index was offered to the model as a tool")
	}
	if len(queries) != 2 || queries[0] != "Make the daily lead report" || queries[1] != "daily-lead" {
		t.Fatalf("index queries = %q", queries)
	}
	if strings.Contains(model.body(1), "Guide available for this request") {
		t.Fatal("the loaded guide did not replace its stub")
	}
	if !strings.Contains(model.body(1), "Step 1: read the leads table.") {
		t.Fatal("the loaded skill guide did not reach the next request")
	}
	if len(used) != 1 || used[0] != "daily-lead" || len(loaded) != 1 || loaded[0] != "daily-lead" {
		t.Fatalf("used=%q loaded=%q", used, loaded)
	}
}

func TestShortlistedGuideStubReachesTheExecutor(t *testing.T) {
	model := &scripted{replies: []string{
		`{"javascriptCode":"final('Make the report',{})"}`,
		`{"javascriptCode":"final('Done',{})"}`,
		`Answer: done`,
	}}
	server := model.server(t)
	index := Capability{Name: SkillSearchTool, Version: "1", Origin: "mcp", Effect: "read", Description: "Find skills by meaning.",
		InputSchema: json.RawMessage(`{"type":"object"}`), Call: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"content":["{\"skills\":[{\"name\":\"daily-lead\",\"score\":0.7}]}"]}`), nil
		}}
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "stub", InputID: "i", Prompt: "Make the daily lead report", SkillQuery: "Make the daily lead report"}, openAIClient(server),
		Tools{Capabilities: []Capability{index}, Skills: []Skill{{Name: "daily-lead", Description: "Build the daily lead report", Content: "Step 1: read the leads table."}}},
		func(Event) error { return nil })
	if err != nil || result.Status != "completed" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !strings.Contains(model.body(1), "Guide available for this request: Build the daily lead report") || strings.Contains(model.body(1), "Step 1: read the leads table.") {
		t.Fatal("the executor did not see the stub, or saw the unloaded guide")
	}
}

func TestRerankedShortlistReachesTheExecutor(t *testing.T) {
	model := &scripted{replies: []string{
		`Fitting: ["aw-territory"]`,
		`{"javascriptCode":"final('Rank territories',{})"}`,
		`{"javascriptCode":"final('Done',{})"}`,
		`Answer: done`,
	}}
	server := model.server(t)
	index := Capability{Name: SkillSearchTool, Version: "1", Origin: "mcp", Effect: "read", Description: "Find skills by meaning.",
		InputSchema: json.RawMessage(`{"type":"object"}`), Call: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"content":["{\"skills\":[{\"name\":\"aw-sales\",\"score\":0.5},{\"name\":\"aw-territory\",\"score\":0.45}]}"]}`), nil
		}}
	var shortlist string
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "rerank", InputID: "i", Prompt: "Which territory grew fastest?", SkillQuery: "Which territory grew fastest?"}, openAIClient(server),
		Tools{Capabilities: []Capability{index}, Skills: []Skill{
			{Name: "aw-sales", Description: "Sales by product", Content: "x"},
			{Name: "aw-territory", Description: "Revenue growth by territory", Content: "y"},
		}},
		func(e Event) error {
			if e.Type == "skill.shortlist" {
				shortlist = string(e.Data)
			}
			return nil
		})
	if err != nil || result.Status != "completed" || result.Usage == nil || result.Usage.Requests != 4 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if shortlist != `["aw-territory"]` || strings.Contains(model.body(1), "aw-sales") || !strings.Contains(model.body(1), "Revenue growth by territory") {
		t.Fatalf("shortlist=%s executor saw the dropped guide or missed the kept one", shortlist)
	}
}
