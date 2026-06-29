package serv

import (
	"context"
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
		mcp.WithDescription("Ask GraphJin's server-side Ax agent to do catalog-first discovery, safe saved-query execution, and return a typed answer/result. Use this when the caller wants GraphJin to orchestrate discovery instead of manually chaining MCP tools."),
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
		mcp.WithString("mode",
			mcp.Description("Agent execution scope. Defaults to safe."),
			mcp.Enum(gjagent.ModeSafe, gjagent.ModeDiscoveryOnly, gjagent.ModeRawAllowed),
		),
		mcp.WithNumber("max_steps",
			mcp.Description("Optional agent step cap for this request. Capped by agent.max_steps."),
			mcp.Min(1),
		),
		mcp.WithBoolean("return_trace",
			mcp.Description("Include Ax action/trace data in the response for debugging."),
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
	// Capabilities is server-derived and json:"-"; it is never taken from tool args.
	agentReq.Capabilities = ms.service.agentCapabilityProfile(ctx)

	runner, err := newGraphJinAgentRunner(ms.service, agentConfigFromService(ms.service.conf))
	var resp gjagent.Response
	if err == nil {
		resp, err = runner.Run(ctx, agentReq)
	}
	recordAgentRuntimeEvent(ms.service, ctx, agentReq, resp, time.Since(start), err)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return ms.toolResultJSON(mcpToolAskGraphJinAgent, args, resp)
}

func agentRequestFromArgs(args map[string]any) gjagent.Request {
	req := gjagent.Request{
		Instruction: stringArg(args, "instruction"),
		Namespace:   stringArg(args, "namespace"),
		Mode:        stringArg(args, "mode"),
		MaxSteps:    catalogIntArg(args, "max_steps"),
	}
	if ctx, ok := args["context"].(map[string]any); ok {
		req.Context = ctx
	}
	if value, ok := args["return_trace"].(bool); ok {
		req.ReturnTrace = &value
	}
	return req
}
