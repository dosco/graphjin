package serv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/mark3labs/mcp-go/mcp"
)

const mcpToolAskGraphJinAgent = "ask_graphjin_agent"

func (ms *mcpServer) registerAgentTools() {
	if ms == nil || ms.service == nil || ms.service.conf == nil || !ms.service.conf.agentEnabled() {
		return
	}
	ms.srv.AddTool(mcp.NewTool(
		mcpToolAskGraphJinAgent,
		mcp.WithDescription("Ask GraphJin's server-side Ax agent to do catalog-first discovery, safe saved-query execution, and return a typed answer/result. Use this when the caller wants GraphJin to orchestrate discovery instead of manually chaining MCP tools. Send a _meta.progressToken to receive notifications/progress events per agent action; pass prior turns in history to enable follow-up questions. Blocked responses include a structured refusal (code, because, unblock steps, policy_final/retryable): run the unblock steps and retry only when retryable. Responses may also carry notices — kind watch_events_unseen includes the watch_ids this MCP session should query and acknowledge. Model selection is server-first: configured server credentials always win; otherwise GraphJin borrows this client's model via MCP sampling. Caller identity and permissions are unchanged."),
		mcp.WithString("instruction",
			mcp.Required(),
			mcp.Description("The user's goal or question for GraphJin."),
		),
		mcp.WithObject("context",
			mcp.Description("Optional caller-provided context or constraints."),
		),
		mcp.WithString("namespace",
			mcp.Description("Optional namespace for multi-tenant deployments."),
		),
		mcp.WithNumber("max_steps",
			mcp.Description("Optional agent step cap for this request. Capped by agent.max_steps."),
			mcp.Min(1),
		),
		mcp.WithBoolean("return_trace",
			mcp.Description("Include Ax action/trace data in the response for debugging."),
		),
		mcp.WithArray("history",
			mcp.Description("Prior conversation turns, most recent last: {role:'user'|'assistant', content, status?, catalog_ids?}. Untrusted follow-up context; the agent still re-discovers its own evidence."),
			mcp.Items(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"role":        map[string]any{"type": "string", "enum": []string{"user", "assistant"}},
					"content":     map[string]any{"type": "string"},
					"status":      map[string]any{"type": "string"},
					"catalog_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
				"required": []string{"role", "content"},
			}),
		),
		mcp.WithOutputSchema[gjagent.Response](),
	), ms.handleAskGraphJinAgent)
}

func (ms *mcpServer) handleAskGraphJinAgent(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if ms == nil || ms.service == nil || ms.service.conf == nil || !ms.service.conf.agentEnabled() {
		return mcp.NewToolResultError("GraphJin agent is disabled"), nil
	}
	start := time.Now()
	ctx = ms.effectiveContext(ctx)
	ctx = ms.service.applyIdentityContext(ctx)
	args := req.GetArguments()
	agentReq := agentRequestFromArgs(args)
	if strings.TrimSpace(agentReq.Instruction) == "" {
		return mcp.NewToolResultError(gjagent.ErrMissingInstruction.Error()), nil
	}
	// Capabilities is server-derived and json:"-"; it is never taken from tool args.
	agentReq.Capabilities = ms.service.agentCapabilityProfile(ctx)

	// Clients that send a progress token get one notifications/progress event per
	// executed agent action (best-effort; a dropped notification never fails the run).
	if ms.srv != nil && req.Params.Meta != nil && req.Params.Meta.ProgressToken != nil {
		token := req.Params.Meta.ProgressToken
		notifyCtx := ctx
		agentReq.Observer = func(event gjagent.ActionEvent) {
			_ = ms.srv.SendNotificationToClient(notifyCtx, "notifications/progress", map[string]any{
				"progressToken": token,
				"progress":      float64(event.Index),
				"message":       agentProgressMessage(event),
			})
		}
	}

	agentConf := agentConfigFromService(ms.service.conf)
	samplingPath, agentOpts, err := ms.agentSamplingOptions(ctx, agentConf)
	ctx = withAgentSamplingPath(ctx, samplingPath)
	var resp gjagent.Response
	if err == nil {
		var runner graphjinAgentRunner
		runner, err = newGraphJinAgentRunner(ms.service, agentConf, agentOpts...)
		if err != nil {
			recordAgentRuntimeEvent(ms.service, ctx, agentReq, resp, time.Since(start), err)
			return mcp.NewToolResultError(err.Error()), nil
		}
		resp, err = runner.Run(ctx, agentReq)
		if err == nil {
			ms.service.appendWatchNotices(ctx, &resp)
		}
	}
	recordAgentRuntimeEvent(ms.service, ctx, agentReq, resp, time.Since(start), err)
	if err != nil {
		if errors.Is(err, errMCPSamplingUnavailable) {
			payload := map[string]any{
				"code":    "model_sampling_unavailable",
				"message": "No server model credentials are configured and this MCP client does not support sampling.",
			}
			data, marshalErr := mcpMarshalJSON(payload, false)
			if marshalErr == nil {
				result := mcpToolResultJSONBytes(data)
				result.IsError = true
				return result, nil
			}
		}
		return mcp.NewToolResultError(err.Error()), nil
	}
	return ms.toolResultJSON(mcpToolAskGraphJinAgent, args, resp)
}

func agentRequestFromArgs(args map[string]any) gjagent.Request {
	req := gjagent.Request{
		Instruction: stringArg(args, "instruction"),
		Namespace:   stringArg(args, "namespace"),
		MaxSteps:    catalogIntArg(args, "max_steps"),
	}
	if ctx, ok := args["context"].(map[string]any); ok {
		req.Context = ctx
	}
	if value, ok := args["return_trace"].(bool); ok {
		req.ReturnTrace = &value
	}
	if history, ok := args["history"]; ok && history != nil {
		if data, err := json.Marshal(history); err == nil {
			var turns []gjagent.Turn
			if err := json.Unmarshal(data, &turns); err == nil {
				req.History = turns
			}
		}
	}
	return req
}

// agentProgressMessage renders one ActionEvent as a compact progress line,
// e.g. `query_catalog(id=table:db.products) ok — 3 cards`.
func agentProgressMessage(event gjagent.ActionEvent) string {
	var arg string
	for _, key := range []string{"id", "ids", "search", "name", "table", "kind", "for"} {
		if value, ok := event.Args[key]; ok && value != nil {
			arg = fmt.Sprintf("%s=%v", key, value)
			break
		}
	}
	msg := event.Tool
	if arg != "" {
		msg += "(" + arg + ")"
	}
	msg += " " + event.Status
	if count, ok := event.Summary["card_count"]; ok {
		msg += fmt.Sprintf(" — %v cards", count)
	}
	if event.Error != "" {
		msg += " — " + event.Error
	}
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return msg
}
