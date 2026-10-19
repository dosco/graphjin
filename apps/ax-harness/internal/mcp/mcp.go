// Package mcp exposes the tools of one trusted MCP server as run capabilities.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"

	"github.com/google/jsonschema-go/jsonschema"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/open-neko/openneko/apps/ax-harness/internal/agent"
)

const (
	maxPages       = 32
	maxTools       = 512
	maxResultBytes = 8 << 20 // below the MCP SDK 16 MiB frame limit
	maxDescription = 1000
	maxSchemaBytes = 16384
)

type resourceLink struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	MIMEType    string `json:"mime_type,omitempty"`
	Size        *int64 `json:"size,omitempty"`
}

// Connect starts cmd as a stdio MCP server and returns every tool it lists.
// Each capability name is prefix + the tool name. effects overrides the effect
// of a tool by its MCP name; other tools are "read" unless their annotations
// say they change state, which makes them "durable".
func Connect(ctx context.Context, cmd *exec.Cmd, prefix string, effects map[string]string) ([]agent.Capability, func() error, error) {
	client := protocol.NewClient(&protocol.Implementation{Name: "ax-harness", Version: "1"}, nil)
	session, err := client.Connect(ctx, &protocol.CommandTransport{Command: cmd}, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("MCP connection failed: %w", err)
	}
	capabilities, err := Capabilities(ctx, session, prefix, effects)
	if err != nil {
		_ = session.Close()
		return nil, nil, err
	}
	return capabilities, session.Close, nil
}

// ConnectStream is Connect for an MCP server reached over stream, which it
// closes with the session.
func ConnectStream(ctx context.Context, stream io.ReadWriteCloser, prefix string, effects map[string]string) ([]agent.Capability, func() error, error) {
	client := protocol.NewClient(&protocol.Implementation{Name: "ax-harness", Version: "1"}, nil)
	session, err := client.Connect(ctx, &protocol.IOTransport{Reader: stream, Writer: stream}, nil)
	if err != nil {
		_ = stream.Close()
		return nil, nil, fmt.Errorf("MCP connection failed: %w", err)
	}
	capabilities, err := Capabilities(ctx, session, prefix, effects)
	if err != nil {
		_ = session.Close()
		return nil, nil, err
	}
	return capabilities, session.Close, nil
}

// DelegatedUsageKey is the _meta key under which a tool reports the model
// tokens another agent spent answering it, as {"total_tokens": n}.
const DelegatedUsageKey = "openneko/delegated_usage"

func delegatedUsage(meta protocol.Meta) any {
	if meta == nil {
		return nil
	}
	return meta[DelegatedUsageKey]
}

// Capabilities lists the tools of an open session. A tool whose prefixed name
// or input schema the agent cannot admit is left out.
func Capabilities(ctx context.Context, session *protocol.ClientSession, prefix string, effects map[string]string) ([]agent.Capability, error) {
	if session == nil {
		return nil, fmt.Errorf("invalid MCP session")
	}
	var tools []*protocol.Tool
	seen, cursors := map[string]bool{}, map[string]bool{}
	cursor := ""
	for page := 0; ; page++ {
		result, err := session.ListTools(ctx, &protocol.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		if result == nil {
			return nil, fmt.Errorf("empty MCP discovery")
		}
		for _, tool := range result.Tools {
			if tool == nil || tool.Name == "" || seen[tool.Name] || len(seen) >= maxTools {
				return nil, fmt.Errorf("invalid MCP discovery")
			}
			seen[tool.Name] = true
			tools = append(tools, tool)
		}
		if result.NextCursor == "" {
			break
		}
		if cursors[result.NextCursor] || page == maxPages-1 {
			return nil, fmt.Errorf("MCP discovery pagination failed")
		}
		cursors[result.NextCursor] = true
		cursor = result.NextCursor
	}
	capabilities := make([]agent.Capability, 0, len(tools))
	for _, tool := range tools {
		name := prefix + tool.Name
		schema, ok := inputSchema(tool.InputSchema)
		if !agent.ValidToolName(name) || !ok {
			continue
		}
		description := tool.Description
		if description == "" {
			description = tool.Name
		}
		if len(description) > maxDescription {
			description = description[:maxDescription]
		}
		effect := effects[tool.Name]
		if effect == "" {
			effect = "read"
			if a := tool.Annotations; a != nil && !a.ReadOnlyHint {
				effect = "durable"
			}
		}
		remote := tool.Name
		capabilities = append(capabilities, agent.Capability{Name: name, Version: "1", Origin: "mcp", Effect: effect,
			Description: description, InputSchema: schema,
			Call: func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
				return call(ctx, session, remote, input)
			}})
	}
	return capabilities, nil
}

func inputSchema(value any) (json.RawMessage, bool) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > maxSchemaBytes {
		return nil, false
	}
	var schema jsonschema.Schema
	if json.Unmarshal(raw, &schema) != nil || schema.Type != "object" {
		return nil, false
	}
	if _, err := schema.Resolve(nil); err != nil {
		return nil, false
	}
	return raw, true
}

func call(ctx context.Context, session *protocol.ClientSession, name string, input json.RawMessage) (json.RawMessage, error) {
	var args map[string]any
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, err
	}
	result, err := session.CallTool(ctx, &protocol.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("empty MCP result")
	}
	texts := make([]string, 0, len(result.Content))
	var resources []resourceLink
	for _, content := range result.Content {
		switch block := content.(type) {
		case *protocol.TextContent:
			texts = append(texts, block.Text)
		case *protocol.ResourceLink:
			if block.URI == "" || block.Name == "" {
				return nil, fmt.Errorf("invalid MCP resource link")
			}
			resources = append(resources, resourceLink{
				URI: block.URI, Name: block.Name, Title: block.Title,
				Description: block.Description, MIMEType: block.MIMEType, Size: block.Size,
			})
		default:
			return nil, fmt.Errorf("unsupported MCP content")
		}
	}
	encoded, err := json.Marshal(struct {
		Content    []string       `json:"content"`
		Resources  []resourceLink `json:"resource_links,omitempty"`
		Structured any            `json:"structured_content,omitempty"`
		IsError    bool           `json:"is_error"`
		Delegated  any            `json:"delegated_usage,omitempty"`
	}{texts, resources, result.StructuredContent, result.IsError, delegatedUsage(result.Meta)})
	if err != nil || len(encoded) > maxResultBytes {
		return nil, fmt.Errorf("MCP result exceeds limit")
	}
	return encoded, nil
}
