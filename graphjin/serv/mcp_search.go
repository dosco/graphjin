package serv

import (
	"context"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerQueryDiscoveryTools registers the saved query discovery tools
func (ms *mcpServer) registerQueryDiscoveryTools() {
	// list_saved_queries - List all saved queries from the allow-list
	ms.srv.AddTool(mcp.NewTool(
		"list_saved_queries",
		mcp.WithDescription("List all saved queries from the allow-list. "+
			"Check this BEFORE writing new queries - a saved query may already exist for your need. "+
			"Saved queries are pre-validated and safer to execute."),
		mcp.WithString("namespace",
			mcp.Description("Optional namespace filter"),
		),
	), ms.handleListSavedQueries)

	// search_saved_queries - Search saved queries by name
	ms.srv.AddTool(mcp.NewTool(
		"search_saved_queries",
		mcp.WithDescription("Search for saved queries by name using fuzzy matching. "+
			"Use this to find queries related to a specific feature or table."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Search term to match against query names"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of results to return (default: 10)"),
		),
	), ms.handleSearchSavedQueries)

	// get_saved_query - Get full details of a saved query
	ms.srv.AddTool(mcp.NewTool(
		"get_saved_query",
		mcp.WithDescription("Get full details of a saved query including the query text and variable schema. "+
			"Use this to understand what variables are needed before calling execute_saved_query."),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Name of the saved query"),
		),
	), ms.handleGetSavedQuery)
}

// handleListSavedQueries returns all saved queries
func (ms *mcpServer) handleListSavedQueries(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check if search is enabled
	if !ms.service.conf.MCP.EnableSearch {
		return mcp.NewToolResultError("query search/listing is not enabled. Enable enable_search in config."), nil
	}

	args := req.GetArguments()
	namespace, _ := args["namespace"].(string)

	queries, err := ms.service.gj.ListSavedQueries()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list queries: %v", err)), nil
	}

	// Filter by namespace if provided
	if namespace != "" {
		filtered := make([]core.SavedQueryInfo, 0)
		for _, q := range queries {
			if q.Namespace == namespace {
				filtered = append(filtered, q)
			}
		}
		queries = filtered
	}

	result := struct {
		Queries []core.SavedQueryInfo `json:"queries"`
		Count   int                   `json:"count"`
	}{
		Queries: queries,
		Count:   len(queries),
	}

	data, err := mcpMarshalJSON(result, true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// handleSearchSavedQueries searches queries by name
func (ms *mcpServer) handleSearchSavedQueries(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Check if search is enabled
	if !ms.service.conf.MCP.EnableSearch {
		return mcp.NewToolResultError("query search is not enabled. Enable enable_search in config."), nil
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

	queries, err := ms.service.gj.ListSavedQueries()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list queries: %v", err)), nil
	}

	// Simple fuzzy search - match if search term is contained in name
	searchTerm := strings.ToLower(searchQuery)
	type scoredQuery struct {
		Query core.SavedQueryInfo
		Score int
	}

	scored := make([]scoredQuery, 0)
	for _, q := range queries {
		name := strings.ToLower(q.Name)
		score := fuzzyScore(searchTerm, name)
		if score > 0 {
			scored = append(scored, scoredQuery{Query: q, Score: score})
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

	// Extract just the query info
	results := make([]core.SavedQueryInfo, len(scored))
	for i, s := range scored {
		results[i] = s.Query
	}

	result := struct {
		Queries []core.SavedQueryInfo `json:"queries"`
		Count   int                   `json:"count"`
	}{
		Queries: results,
		Count:   len(results),
	}

	data, err := mcpMarshalJSON(result, true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// handleGetSavedQuery returns details of a specific saved query
func (ms *mcpServer) handleGetSavedQuery(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	name, _ := args["name"].(string)

	if name == "" {
		return mcp.NewToolResultError("query name is required"), nil
	}

	details, err := ms.service.gj.GetSavedQuery(name)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get query: %v", err)), nil
	}

	data, err := mcpMarshalJSON(details, true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// fuzzyScore returns a score for how well the search term matches the target
// Higher score = better match
func fuzzyScore(search, target string) int {
	// Exact match
	if search == target {
		return 100
	}

	// Starts with
	if strings.HasPrefix(target, search) {
		return 90
	}

	// Contains
	if strings.Contains(target, search) {
		return 70
	}

	// Word boundary match
	words := strings.FieldsFunc(target, func(r rune) bool {
		return r == '_' || r == '-' || r == '.'
	})
	for _, word := range words {
		if strings.HasPrefix(word, search) {
			return 60
		}
	}

	// Character-by-character fuzzy match
	searchIdx := 0
	matches := 0
	for i := 0; i < len(target) && searchIdx < len(search); i++ {
		if target[i] == search[searchIdx] {
			matches++
			searchIdx++
		}
	}

	if searchIdx == len(search) {
		// All characters found in order
		return 50 * matches / len(target)
	}

	return 0
}
