package agent

import (
	"context"
	"encoding/json"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

type codeFixture struct {
	ax.AIClient
	code  string
	calls int
}

func (c *codeFixture) Chat(context.Context, map[string]ax.Value, map[string]ax.Value) (ax.Value, error) {
	c.calls++
	content := `{"answer":"done"}`
	switch c.calls {
	case 1:
		code, _ := json.Marshal(c.code)
		content = `{"javascriptCode":` + string(code) + `}`
	case 2:
		content = `{"javascriptCode":"await final('done', {})"}`
	}
	return ax.Object("results", []ax.Value{ax.Object("index", 0, "content", content, "finish_reason", "stop")}), nil
}

func TestActorCodeKeepsLettersBeforeLineBreaks(t *testing.T) {
	for _, query := range []string{
		"{\n  product {\n    productnumber\n    reorderpoint\n  }\n}",
		"x\nlayer\nnumber\n",
		"a r\nb r",
	} {
		var got string
		gemini := ax.NewAI("google-gemini", ax.Object("api_key", "synthetic", "model", "fixture", "base_url", "http://127.0.0.1:1")).(ax.AxAIService)
		client := &RoutedClient{AIClient: &codeFixture{AIClient: gemini, code: "const query = `" + query + "`;\nawait execute_graphql({ query });"}, DefaultProvider: "google-gemini"}
		tool := Capability{Name: "execute_graphql", Version: "1", Origin: "fixture", Effect: "read", Description: "Run GraphQL.",
			InputSchema: json.RawMessage(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"}}}`),
			Call: func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
				var input struct{ Query string }
				_ = json.Unmarshal(raw, &input)
				got = input.Query
				return json.RawMessage(`{"data":{}}`), nil
			}}
		_, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "line-ends", InputID: "input", Prompt: "Read products", MaxOperations: 3, MaxModelCalls: 6},
			client, Tools{Capabilities: []Capability{tool}}, func(Event) error { return nil })
		if err != nil || got != query {
			t.Fatalf("query = %q, want %q (err %v)", got, query, err)
		}
	}
}

func TestLineEndGuardAcrossChunks(t *testing.T) {
	whole := `{"javascriptCode":"a = ` + "`" + `productnumber\nlayer\\n\"r\"\n` + "`" + `"}`
	want := `{"javascriptCode":"a = ` + "`" + `productnumber\r\nlayer\\n\"r\"\n` + "`" + `"}`
	for cut := 1; cut < len(whole); cut++ {
		values := []ax.Value{
			ax.Object("results", []ax.Value{ax.Object("index", 0, "content", whole[:cut])}),
			ax.Object("results", []ax.Value{ax.Object("index", 0, "content", whole[cut:])}),
		}
		guardResponses(values)
		got := ""
		for _, value := range values {
			got += items(value.(map[string]ax.Value)["results"])[0].(map[string]ax.Value)["content"].(string)
		}
		if got != want {
			t.Fatalf("cut %d: %s", cut, got)
		}
	}
}
