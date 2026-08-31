package serv

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	mcpserver "github.com/dosco/graphjin/serv/v3/internal/mcpcompat/server"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Recording what an agent did over MCP.
//
// An evaluation harness grades what was done, not only what was said. For
// GraphJin's own agent that account comes back with the answer; for an external
// agent — a lab's own scaffold connecting over MCP — there is nobody to ask but
// the server. Without it, an external agent could be graded on its answer alone,
// which is precisely the shortcut every method rule exists to close.
//
// This is deliberately at the protocol boundary rather than inside the tool
// implementations: it sees the calls that failed as well as the ones that
// worked, and a tool that returns an error rather than an error result cannot
// slip past it.
func installMCPToolRecorder(srv *mcpserver.MCPServer, record func(MCPToolEvent)) {
	if srv == nil || record == nil {
		return
	}
	srv.SDK().AddReceivingMiddleware(func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			call, ok := req.(*sdk.CallToolRequest)
			if method != "tools/call" || !ok {
				return next(ctx, method, req)
			}
			// The namespace prefix is stripped here as well as by the server's own
			// middleware, so this records the same tool name either way round and
			// stays correct however the two are ordered.
			name := call.Params.Name
			if index := strings.LastIndex(name, ":"); index >= 0 {
				name = name[index+1:]
			}
			arguments := cloneToolArguments(call.Params.Arguments)
			session := ""
			if call.Session != nil {
				session = call.Session.ID()
			}

			started := time.Now()
			result, err := next(ctx, method, req)
			event := MCPToolEvent{
				SessionID: session, Tool: name, Arguments: arguments,
				DurationMS: time.Since(started).Milliseconds(),
				IsError:    err != nil,
			}
			if toolResult, ok := result.(*sdk.CallToolResult); ok && toolResult != nil && toolResult.IsError {
				event.IsError = true
			}
			record(event)
			return result, err
		}
	})
}

// cloneToolArguments decodes the call's arguments into something a recorder can
// hold onto.
//
// At this seam the arguments are still the raw JSON the client sent — decoding
// into the tool's own argument type happens further in. Type-asserting for a
// map here silently yields nothing, which would record every call with its
// arguments missing: tool names with no queries, and a method rule with nothing
// to match against.
func cloneToolArguments(arguments any) map[string]any {
	switch source := arguments.(type) {
	case json.RawMessage:
		if len(source) == 0 {
			return nil
		}
		var decoded map[string]any
		if err := json.Unmarshal(source, &decoded); err != nil {
			return nil
		}
		return decoded
	case map[string]any:
		cloned := make(map[string]any, len(source))
		for key, value := range source {
			cloned[key] = value
		}
		return cloned
	default:
		return nil
	}
}
