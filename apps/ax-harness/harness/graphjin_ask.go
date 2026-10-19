package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/open-neko/openneko/apps/ax-harness/internal/agent"
)

// GraphJinAskToolName is the bridge's name for GraphJin's agent, kept so the
// prompts that name the tool hold for either path.
const GraphJinAskToolName = "mcp_neko_graphjin_agent_ask"

// GraphJinAsk asks GraphJin's agent one data question for this turn's user.
// onStep receives each step the agent takes. It returns GraphJin's agent
// response and the model tokens the agent spent.
type GraphJinAsk func(ctx context.Context, instruction string, maxSteps int, onStep func(string)) (response json.RawMessage, tokens int64, err error)

func graphJinAskCapability(ask GraphJinAsk) agent.Capability {
	return agent.Capability{
		Name: GraphJinAskToolName, Version: "1", Origin: "graphjin", Effect: "read",
		Description: "Delegate one read-only operational data question to GraphJin's catalog-first server agent. " +
			"Include the complete business question, time window, aggregation, comparison baseline, and desired dimension in one instruction.",
		InputSchema: json.RawMessage(`{"type":"object","required":["instruction"],"properties":{"instruction":{"type":"string","minLength":1,"maxLength":8000},"maxSteps":{"type":"integer","minimum":1,"maximum":12}},"additionalProperties":false}`),
		Call: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
			var input struct {
				Instruction string `json:"instruction"`
				MaxSteps    int    `json:"maxSteps"`
			}
			if err := json.Unmarshal(raw, &input); err != nil || strings.TrimSpace(input.Instruction) == "" || len(input.Instruction) > 8000 {
				return nil, fmt.Errorf("invalid data question")
			}
			response, tokens, err := ask(ctx, strings.TrimSpace(input.Instruction), input.MaxSteps, func(step string) {
				agent.ToolProgress(ctx, step)
			})
			// The bridge tool's result shape: its text is the data agent result.
			result := map[string]any{}
			if err != nil {
				result["error"] = err.Error()
			} else {
				result["response"] = response
			}
			text, _ := json.Marshal(result)
			out := map[string]any{"content": []string{string(text)}, "is_error": err != nil}
			if tokens > 0 {
				out["delegated_usage"] = map[string]int64{"total_tokens": tokens}
			}
			return json.Marshal(out)
		},
	}
}
