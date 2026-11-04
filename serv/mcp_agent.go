package serv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/serv/v3/internal/mcpcompat/mcp"
	"go.opentelemetry.io/otel/trace"
)

const mcpToolAskGraphJinAgent = "ask_graphjin_agent"

func (ms *mcpServer) registerAgentTools() {
	if ms == nil || ms.service == nil || ms.service.conf == nil || !ms.service.conf.agentEnabled() {
		return
	}
	ms.srv.AddTool(mcp.NewTool(
		mcpToolAskGraphJinAgent,
		mcp.WithDescription("Ask GraphJin's server-side Ax agent to do catalog-first discovery, safe saved-query execution, and return a typed answer/result. Use this when the caller wants GraphJin to orchestrate discovery instead of manually chaining MCP tools. Send a _meta.progressToken to receive notifications/progress events per agent action; pass prior turns in history to enable follow-up questions. Blocked responses include a structured refusal (code, because, unblock steps, policy_final/retryable): run the unblock steps and retry only when retryable. Responses may also carry notices: watch_events_unseen lists watch_ids to review, task_open_unlinked lists open task_ids that were not attached to the run, task_context_loaded confirms retained task context, and annotations_unshared lists owner-only annotation drafts awaiting review. The agent always uses GraphJin-owned provider credentials. Caller identity and permissions are unchanged."),
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
		mcp.WithString("task_id",
			mcp.Description("Optional retained gj_task id. It loads owner-scoped declared context and journals this run; it never grants access or satisfies discovery guards."),
		),
		mcp.WithNumber("max_steps",
			mcp.Description("Optional agent step cap for this request. Capped by agent.max_steps."),
			mcp.Min(1),
		),
		mcp.WithBoolean("return_trace",
			mcp.Description("Include Ax action/trace data in the response for debugging."),
		),
		mcp.WithArray("history",
			mcp.Description("Prior conversation turns, most recent last: {role:'user'|'assistant', content, status?}. A request with history runs the lead agent, which resolves follow-ups into self-contained questions; without it one question runs directly."),
			mcp.Items(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"role":    map[string]any{"type": "string", "enum": []string{"user", "assistant"}},
					"content": map[string]any{"type": "string"},
					"status":  map[string]any{"type": "string"},
				},
				"required": []string{"role", "content"},
			}),
		),
		mcp.WithString("thread_id",
			mcp.Description("Persistent thread to continue. The thread's stored messages replace history, and this turn is recorded in it so any GraphJin replica or the console can show it."),
		),
		mcp.WithOutputSchema[gjagent.Response](),
	), ms.handleAskGraphJinAgent)
}

func (ms *mcpServer) handleAskGraphJinAgent(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if ms == nil || ms.service == nil || ms.service.conf == nil || !ms.service.conf.agentEnabled() {
		return mcp.NewToolResultError("GraphJin agent is disabled"), nil
	}
	start := time.Now()
	agentConf := agentConfigFromService(ms.service.conf)
	ctx = ms.effectiveContext(ctx)
	ctx = withWatchCreatedRecorder(ms.service.applyIdentityContext(ctx))
	args := req.GetArguments()
	agentReq := agentRequestFromArgs(args)
	if strings.TrimSpace(agentReq.Instruction) == "" {
		return mcp.NewToolResultError(gjagent.ErrMissingInstruction.Error()), nil
	}
	// Capabilities is server-derived and json:"-"; it is never taken from tool args.
	agentReq.Capabilities = ms.service.agentCapabilityProfile(ctx)
	taskWarm, err := ms.service.resolveAgentTaskContext(ctx, &agentReq)
	if err != nil {
		info, _ := agentPublicFailure(agentConf, err)
		return mcp.NewToolResultError(info.Message), nil
	}

	// Clients that send a progress token get one notifications/progress event per
	// executed agent action (best-effort; a dropped notification never fails the run).
	if ms.srv != nil && req.Params.Meta != nil && req.Params.Meta.ProgressToken != nil {
		token := req.Params.Meta.ProgressToken
		notifyCtx := ctx
		agentReq.Observer = func(event gjagent.ActionEvent) {
			_ = ms.srv.SendNotificationToClient(notifyCtx, "notifications/progress", map[string]any{
				"progressToken": token,
				"progress":      float64(event.Index),
				"message":       agentProgressMessage(event, configuredAgentSecret(agentConf)),
			})
		}
	}

	var resp gjagent.Response
	runner, err := newGraphJinAgentRunner(ms.service, agentConf)
	if err != nil {
		if errors.Is(err, gjagent.ErrMissingAPIKey) {
			recordAgentRuntimeEvent(ms.service, ctx, agentReq, resp, taskWarm, time.Since(start), err)
			ms.service.appendTaskTrailEntry(ctx, agentReq, resp, time.Since(start), err)
			return modelCredentialsRequiredToolResult(agentConf), nil
		}
		info, publicErr := agentPublicFailure(agentConf, err)
		resp = gjagent.Response{Status: gjagent.StatusError, Errors: []gjagent.ErrorInfo{info}}
		recordAgentRuntimeEvent(ms.service, ctx, agentReq, resp, taskWarm, time.Since(start), publicErr)
		ms.service.appendTaskTrailEntry(ctx, agentReq, resp, time.Since(start), publicErr)
		return ms.toolResultJSON(mcpToolAskGraphJinAgent, args, resp)
	}
	threadID := strings.TrimSpace(stringArg(args, "thread_id"))
	// A conversation — a thread, history, a task's trail — runs the lead; a
	// single self-contained question runs the worker alone.
	if threadID != "" || len(agentReq.History) != 0 {
		lead, err := ms.service.threadAgentRunner(agentConf, runner)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		runner = lead
	}
	if threadID != "" {
		return ms.askAgentInThread(ctx, args, threadID, agentReq, taskWarm, runner, start)
	}
	resp, err = runner.Run(ctx, agentReq)
	runErr := err
	if err == nil {
		ms.service.appendWatchNotices(ctx, &resp)
		ms.service.appendTaskNotices(ctx, agentReq, &resp)
		ms.service.appendAnnotationNotices(ctx, &resp)
		appendTaskContextNotice(taskWarm, &resp)
	}
	resp = sanitizeAgentResponse(agentConf, resp)
	if err != nil {
		_, err = agentPublicFailure(agentConf, err)
	}
	recordAgentUsageObservability(ms.service, trace.SpanFromContext(ctx), "mcp", agentConf, resp, time.Since(start))
	recordAgentRuntimeEvent(ms.service, ctx, agentReq, resp, taskWarm, time.Since(start), err)
	ms.service.appendTaskTrailEntry(ctx, agentReq, resp, time.Since(start), err)
	if err != nil {
		if errors.Is(runErr, gjagent.ErrMissingAPIKey) {
			return modelCredentialsRequiredToolResult(agentConf), nil
		}
		info := gjagent.PublicErrorInfo(runErr, agentConf.Provider, agentConf.Model, configuredAgentSecret(agentConf))
		resp = gjagent.Response{Status: gjagent.StatusError, Errors: []gjagent.ErrorInfo{info}}
		return ms.toolResultJSON(mcpToolAskGraphJinAgent, args, resp)
	}
	return ms.toolResultJSON(mcpToolAskGraphJinAgent, args, resp)
}

// askAgentInThread runs one ask_graphjin_agent call as a turn of a persistent
// thread, the MCP twin of POST /api/v1/agent with a thread_id.
func (ms *mcpServer) askAgentInThread(ctx context.Context, args map[string]any, threadID string, agentReq gjagent.Request,
	taskWarm taskWarmStart, runner graphjinAgentRunner, start time.Time,
) (*mcp.CallToolResult, error) {
	s := ms.service
	if !s.threadsEnabled() {
		return mcp.NewToolResultError("thread_id requires threads.enabled"), nil
	}
	run, err := s.prepareThreadRun(ctx, threadID, &agentReq)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	settle := func(ctx context.Context, resp gjagent.Response, err error) (gjagent.Response, error) {
		return s.settleAgentResponse(ctx, agentReq, taskWarm, resp, err)
	}
	resp, err := run.execute(ctx, runner, agentReq, settle, nil)
	agentConf := agentConfigFromService(s.conf)
	recordAgentUsageObservability(s, trace.SpanFromContext(ctx), "mcp", agentConf, resp, time.Since(start))
	recordAgentRuntimeEvent(s, ctx, agentReq, resp, taskWarm, time.Since(start), err)
	s.appendTaskTrailEntry(ctx, agentReq, resp, time.Since(start), err)
	resp.Notices = append(resp.Notices, gjagent.ResponseNotice{
		Kind:    "thread_turn_recorded",
		Message: fmt.Sprintf("Recorded in thread %s as run %s; pass the same thread_id to continue.", threadID, run.runID),
	})
	return ms.toolResultJSON(mcpToolAskGraphJinAgent, args, resp)
}

func modelCredentialsRequiredToolResult(conf gjagent.Config) *mcp.CallToolResult {
	apiKeyEnv := strings.TrimSpace(conf.APIKeyEnv)
	if apiKeyEnv == "" {
		apiKeyEnv = defaultAgentAPIKeyEnv
	}
	payload := map[string]any{
		"status":      gjagent.StatusBlocked,
		"code":        modelCredentialsRequiredCode,
		"message":     "GraphJin-owned model credentials are required for agent execution.",
		"api_key_env": apiKeyEnv,
	}
	data, err := mcpMarshalJSON(payload, false)
	if err != nil {
		return mcp.NewToolResultError(modelCredentialsRequiredCode)
	}
	result := mcpToolResultJSONBytes(data)
	result.IsError = true
	return result
}

func agentRequestFromArgs(args map[string]any) gjagent.Request {
	req := gjagent.Request{
		Instruction: stringArg(args, "instruction"),
		Namespace:   stringArg(args, "namespace"),
		TaskID:      stringArg(args, "task_id"),
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
func agentProgressMessage(event gjagent.ActionEvent, secrets ...string) string {
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
	return gjagent.SanitizeText(msg, secrets...)
}
