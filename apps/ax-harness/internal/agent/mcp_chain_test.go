package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

// A read result from the OpenNeko MCP bridge must be usable as the exact
// version precondition of a later durable tool in the same Ax code turn.
func TestMCPReadVersionFeedsDurableTool(t *testing.T) {
	var calls, saves atomic.Int32
	answers := []string{
		`{"javascriptCode":"final('List and revise the workflow', {})"}`,
		`{"javascriptCode":"const listed=mcp_neko_workflow_builder_list_workflows({limit:20}); function unwrap(v){if(typeof v==='string')return unwrap(JSON.parse(v)); if(v&&v.content&&v.content[0])return unwrap(v.content[0]); if(v&&v.text)return unwrap(v.text); return v;} const item=(unwrap(listed).workflows||[]).find(w=>w.name==='Harness review workflow'); if(!item||!item.versionToken) throw Error('workflow version missing'); const receipt=workflow_save({name:item.name,steps:[{id:'review',description:'Updated'}],expectedVersion:item.versionToken}); final('Report the updated workflow',{receipt});"}`,
		`{"answer":"Updated the workflow."}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(calls.Add(1)) - 1
		if i >= len(answers) {
			http.Error(w, "unexpected model call", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ax.Object("choices", ax.Array(ax.Object("message", ax.Object("role", "assistant", "content", answers[i]), "finish_reason", "stop"))))
	}))
	defer server.Close()
	client := ax.NewOpenAICompatibleClient(ax.Object("base_url", server.URL, "api_key", "synthetic", "model", "fixture"))
	list := Capability{Name: "mcp_neko_workflow_builder_list_workflows", Version: "1", Origin: "openneko", Effect: "read", Description: "List workflows.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer"}}}`),
		Call: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"content":["{\"workflows\":[{\"name\":\"Harness review workflow\",\"versionToken\":\"123\"}]}"],"is_error":false}`), nil
		}}
	save := Capability{Name: "workflow_save", Version: "1", Origin: "openneko", Effect: "durable", Description: "Save workflow.",
		InputSchema: json.RawMessage(`{"type":"object","required":["name","steps","expectedVersion"],"properties":{"name":{"type":"string"},"steps":{"type":"array"},"expectedVersion":{"type":"string"}}}`),
		Call: func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
			var args struct {
				ExpectedVersion string `json:"expectedVersion"`
			}
			if err := json.Unmarshal(raw, &args); err != nil || args.ExpectedVersion != "123" {
				t.Errorf("lost workflow revision: %s (%v)", raw, err)
			}
			saves.Add(1)
			return json.RawMessage(`{"ok":true,"workflowId":"fixture"}`), nil
		}}
	result, err := RunWithTools(context.Background(), Spec{Version: 1, RunID: "workflow-chain", InputID: "input", Prompt: "Edit the workflow"},
		client, Tools{Capabilities: []Capability{list, save}}, func(Event) error { return nil })
	if err != nil || result.Status != "completed" || saves.Load() != 1 || calls.Load() != 3 {
		t.Fatalf("result=%+v err=%v saves=%d modelCalls=%d", result, err, saves.Load(), calls.Load())
	}
}
