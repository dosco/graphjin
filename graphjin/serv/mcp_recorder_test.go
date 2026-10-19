package serv

import (
	"context"
	"errors"
	"testing"

	mcp "github.com/dosco/graphjin/serv/v3/internal/mcpcompat/mcp"
	mcpserver "github.com/dosco/graphjin/serv/v3/internal/mcpcompat/server"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// callRecordedTool drives one tool call through the SDK server the recorder is
// installed on, so the middleware runs exactly as it does in a live session.
func callRecordedTool(t *testing.T, tool mcp.Tool, handler mcpserver.ToolHandler,
	arguments map[string]any) []MCPToolEvent {
	t.Helper()
	srv := mcpserver.NewMCPServer("test", "0.0.0")
	var events []MCPToolEvent
	installMCPToolRecorder(srv, func(event MCPToolEvent) { events = append(events, event) })
	srv.AddTool(tool, handler)

	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := srv.SDK().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close() //nolint:errcheck
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close() //nolint:errcheck

	// The result is deliberately ignored: what is being tested is what the
	// recorder saw, including for calls that failed.
	_, _ = clientSession.CallTool(ctx, &sdk.CallToolParams{Name: tool.Name, Arguments: arguments})
	return events
}

func recorderTestTool() mcp.Tool {
	return mcp.Tool{
		Name:        "execute_graphql",
		Description: "run a query",
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: map[string]any{"query": map[string]any{"type": "string"}},
		},
	}
}

// The recorder is what makes grading an agent this harness does not host a
// measurement rather than a self-report.
func TestMCPToolRecorderCapturesCallsAndArguments(t *testing.T) {
	events := callRecordedTool(t, recorderTestTool(),
		func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText(`{"data":{"accounts":[{"count_id":4}]}}`), nil
		},
		map[string]any{"query": "query { accounts { count_id } }"})

	if len(events) != 1 {
		t.Fatalf("expected one recorded call, got %d", len(events))
	}
	event := events[0]
	if event.Tool != "execute_graphql" {
		t.Fatalf("tool = %q", event.Tool)
	}
	// The query is what every method rule is written against, so losing the
	// arguments would leave nothing to grade the method on.
	if event.Arguments["query"] != "query { accounts { count_id } }" {
		t.Fatalf("arguments = %+v", event.Arguments)
	}
	if event.IsError {
		t.Fatal("a successful call was recorded as an error")
	}
}

// A call that failed must be recorded as failed, both ways it can fail. An
// absent error count reads as success downstream, so a run of failures would
// otherwise grade as competent work.
func TestMCPToolRecorderNoticesBothKindsOfFailure(t *testing.T) {
	fromResult := callRecordedTool(t, recorderTestTool(),
		func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultError("unknown column"), nil
		}, map[string]any{"query": "query { accounts { nope } }"})
	if len(fromResult) != 1 || !fromResult[0].IsError {
		t.Fatalf("an error result was not recorded as one: %+v", fromResult)
	}

	fromError := callRecordedTool(t, recorderTestTool(),
		func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return nil, errors.New("the database is gone")
		}, map[string]any{"query": "query { accounts { count_id } }"})
	if len(fromError) != 1 || !fromError[0].IsError {
		t.Fatalf("a returned error was not recorded as one: %+v", fromError)
	}
}

// Recording is off unless somebody asked for it, and a nil recorder must be a
// no-op rather than a panic on every tool call in production.
func TestMCPToolRecorderIsOptional(t *testing.T) {
	srv := mcpserver.NewMCPServer("test", "0.0.0")
	installMCPToolRecorder(srv, nil)
	installMCPToolRecorder(nil, func(MCPToolEvent) {})
}
