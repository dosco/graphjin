package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerFragmentTools registers the fragment discovery tools
func (ms *mcpServer) registerFragmentTools() {
	// list_fragments - List all available fragments
	ms.srv.AddTool(mcp.NewTool(
		"list_fragments",
		mcp.WithDescription("List all available GraphQL fragments. Fragments are reusable field selections stored in /queries/fragments/."),
		mcp.WithString("namespace",
			mcp.Description("Optional namespace filter"),
		),
	), ms.handleListFragments)

	// get_fragment - Get full details of a fragment
	ms.srv.AddTool(mcp.NewTool(
		"get_fragment",
		mcp.WithDescription("Get full details of a fragment including its definition and the type it's defined on. Use this to see how to use a fragment in your queries."),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Name of the fragment"),
		),
	), ms.handleGetFragment)

	// search_fragments - Search fragments by name
	ms.srv.AddTool(mcp.NewTool(
		"search_fragments",
		mcp.WithDescription("Search for fragments by name. Uses fuzzy matching to find relevant fragments."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Search term to match against fragment names"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of results to return (default: 10)"),
		),
	), ms.handleSearchFragments)
}

// handleListFragments returns all available fragments
func (ms *mcpServer) handleListFragments(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check if search is enabled
	if !ms.service.conf.MCP.EnableSearch {
		return mcp.NewToolResultError("fragment listing is not enabled. Enable enable_search in config."), nil
	}

	args := req.GetArguments()
	namespace, _ := args["namespace"].(string)

	fragments, err := ms.service.gj.ListFragments()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list fragments: %v", err)), nil
	}

	// Filter by namespace if provided
	if namespace != "" {
		filtered := make([]core.FragmentInfo, 0)
		for _, f := range fragments {
			if f.Namespace == namespace {
				filtered = append(filtered, f)
			}
		}
		fragments = filtered
	}

	result := struct {
		Fragments []core.FragmentInfo `json:"fragments"`
		Count     int                 `json:"count"`
		Usage     string              `json:"usage"`
	}{
		Fragments: fragments,
		Count:     len(fragments),
		Usage:     `To use a fragment, add: #import "./fragments/<name>" at the top of your query, then use ...FragmentName in your selection set`,
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// handleGetFragment returns details of a specific fragment
func (ms *mcpServer) handleGetFragment(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	name, _ := args["name"].(string)

	if name == "" {
		return mcp.NewToolResultError("fragment name is required"), nil
	}

	details, err := ms.service.gj.GetFragment(name)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get fragment: %v", err)), nil
	}

	// Add usage example
	result := struct {
		*core.FragmentDetails
		ImportDirective string `json:"import_directive"`
		UsageExample    string `json:"usage_example"`
	}{
		FragmentDetails: details,
		ImportDirective: fmt.Sprintf(`#import "./fragments/%s"`, name),
		UsageExample:    fmt.Sprintf("query { %s { ...%s } }", details.On, details.Name),
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// handleSearchFragments searches fragments by name
func (ms *mcpServer) handleSearchFragments(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check if search is enabled
	if !ms.service.conf.MCP.EnableSearch {
		return mcp.NewToolResultError("fragment search is not enabled. Enable enable_search in config."), nil
	}

	args := req.GetArguments()
	searchQuery, _ := args["query"].(string)
	limitFloat, _ := args["limit"].(float64)

	if searchQuery == "" {
		return mcp.NewToolResultError("search query is required"), nil
	}

	limit := int(limitFloat)
	if limit <= 0 {
		limit = 10
	}

	fragments, err := ms.service.gj.ListFragments()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list fragments: %v", err)), nil
	}

	// Simple fuzzy search - reuse fuzzyScore from mcp_search.go
	searchTerm := strings.ToLower(searchQuery)
	type scoredFragment struct {
		Fragment core.FragmentInfo
		Score    int
	}

	scored := make([]scoredFragment, 0)
	for _, f := range fragments {
		name := strings.ToLower(f.Name)
		score := fuzzyScore(searchTerm, name)
		if score > 0 {
			scored = append(scored, scoredFragment{Fragment: f, Score: score})
		}
	}

	// Sort by score (higher is better)
	for i := 0; i < len(scored); i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].Score > scored[i].Score {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	// Limit results
	if len(scored) > limit {
		scored = scored[:limit]
	}

	// Extract just the fragment info
	results := make([]core.FragmentInfo, len(scored))
	for i, s := range scored {
		results[i] = s.Fragment
	}

	result := struct {
		Fragments []core.FragmentInfo `json:"fragments"`
		Count     int                 `json:"count"`
	}{
		Fragments: results,
		Count:     len(results),
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}
