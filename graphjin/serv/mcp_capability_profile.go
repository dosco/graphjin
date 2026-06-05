package serv

import (
	"context"
	"sort"
	"strings"

	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/mcp"
)

type MCPCapabilityProfile struct {
	Mode                  string                 `json:"mode,omitempty"`
	SourcesMode           bool                   `json:"sources_mode"`
	RoleClass             string                 `json:"role_class"`
	Authenticated         bool                   `json:"authenticated"`
	CatalogRevision       string                 `json:"catalog_revision,omitempty"`
	AvailableTools        []string               `json:"available_tools,omitempty"`
	AvailableRoots        []MCPRootProfile       `json:"available_roots,omitempty"`
	BlockedRoots          []MCPRootProfile       `json:"blocked_roots,omitempty"`
	VisibleCapabilities   []MCPCatalogCapability `json:"visible_capabilities,omitempty"`
	RecommendedEntrypoint string                 `json:"recommended_entrypoint,omitempty"`
	SafetyNotes           []string               `json:"safety_notes,omitempty"`
}

type MCPRootProfile struct {
	Root       string `json:"root"`
	AccessMode string `json:"access_mode,omitempty"`
	Available  bool   `json:"available"`
	Reason     string `json:"reason,omitempty"`
}

type MCPCatalogCapability struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Name           string `json:"name,omitempty"`
	Summary        string `json:"summary,omitempty"`
	Enabled        bool   `json:"enabled,omitempty"`
	CapabilityKind string `json:"capability_kind,omitempty"`
}

var mcpGraphJinRoots = []string{
	"gj_catalog",
	"gj_artifacts",
	"gj_workflow",
	"gj_workflow_execution",
	"gj_runtime",
	"gj_security",
	"gj_config",
}

var mcpToolRequiredRoots = map[string][]string{
	"graphql_help":             {"gj_catalog"},
	"query_catalog":            {"gj_catalog"},
	"get_catalog_card":         {"gj_catalog"},
	"get_catalog_entrypoints":  {"gj_catalog"},
	"get_catalog_capabilities": {"gj_catalog"},
	"get_current_config":       {"gj_config"},
	"update_current_config":    {"gj_config"},
	"execute_workflow":         {"gj_workflow_execution"},
	"save_workflow":            {"gj_workflow"},
	"list_workflows":           {"gj_workflow"},
}

func (ms *mcpServer) effectiveContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if ms == nil || ms.ctx == nil {
		return ctx
	}
	for _, key := range []any{core.UserIDKey, core.UserRoleKey, core.IdentityVarsKey, core.IdentityRolesKey} {
		if ctx.Value(key) == nil {
			if value := ms.ctx.Value(key); value != nil {
				ctx = context.WithValue(ctx, key, value)
			}
		}
	}
	return ctx
}

func (s *graphjinService) mcpStartupCapabilityProfile(ctx context.Context) *MCPCapabilityProfile {
	ms := &mcpServer{service: s, ctx: ctx}
	return ms.callerCapabilityProfile(ctx, false)
}

func (ms *mcpServer) callerCapabilityProfile(ctx context.Context, includeVisibleCapabilities bool) *MCPCapabilityProfile {
	ctx = ms.effectiveContext(ctx)
	conf := ms.config()
	role := runtimeRoleClass(ctx)
	profile := &MCPCapabilityProfile{
		Mode:          effectiveMode(conf),
		SourcesMode:   conf != nil && conf.Core.IsSourcesUsed(),
		RoleClass:     role,
		Authenticated: mcpContextAuthenticated(ctx, role),
		SafetyNotes: []string{
			"Capability profile is caller-scoped and redacted.",
			"Do not use client-supplied variables as identity fields.",
		},
	}
	if snap, err := ms.catalogSnapshot(); err == nil && snap != nil {
		profile.CatalogRevision = snap.Revision
	}
	for _, tool := range mcpToolList(conf) {
		if ms.toolVisibleForContext(ctx, tool) {
			profile.AvailableTools = append(profile.AvailableTools, tool)
		}
	}
	sort.Strings(profile.AvailableTools)
	if profile.SourcesMode {
		profile.AvailableRoots, profile.BlockedRoots = ms.graphJinRootProfiles(ctx)
		if !ms.rootVisibleForContext(ctx, "gj_catalog") {
			profile.SafetyNotes = append(profile.SafetyNotes, "gj_catalog is not available to this caller; request authenticated/admin access instead of inventing schema.")
		}
	}
	profile.RecommendedEntrypoint = ms.recommendedMCPEntrypoint(ctx)
	if includeVisibleCapabilities && ms.rootVisibleForContext(ctx, "gj_catalog") {
		profile.VisibleCapabilities = ms.visibleCatalogCapabilities(ctx)
	}
	return profile
}

func (ms *mcpServer) config() *Config {
	if ms == nil || ms.service == nil {
		return nil
	}
	return ms.service.conf
}

func mcpContextAuthenticated(ctx context.Context, role string) bool {
	if strings.TrimSpace(role) != "" && !strings.EqualFold(role, "anon") && !strings.EqualFold(role, "unknown") {
		return true
	}
	if ctx != nil && ctx.Value(core.UserIDKey) != nil {
		return true
	}
	return false
}

func (ms *mcpServer) graphJinRootProfiles(ctx context.Context) ([]MCPRootProfile, []MCPRootProfile) {
	conf := ms.config()
	if conf == nil || !conf.Core.IsSourcesUsed() {
		return nil, nil
	}
	source, ok := conf.Core.GraphJinSource()
	if !ok {
		return nil, nil
	}
	access := conf.Core.EffectiveSourceAccess(source)
	var available []MCPRootProfile
	var blocked []MCPRootProfile
	for _, root := range mcpGraphJinRoots {
		mode := strings.TrimSpace(access.Roots[root])
		item := MCPRootProfile{
			Root:       root,
			AccessMode: mode,
			Available:  ms.rootVisibleForContext(ctx, root),
		}
		if item.Available {
			item.Reason = "allowed for caller role"
			available = append(available, item)
		} else {
			item.Reason = "blocked by source-mode root access"
			blocked = append(blocked, item)
		}
	}
	return available, blocked
}

func (ms *mcpServer) rootVisibleForContext(ctx context.Context, root string) bool {
	conf := ms.config()
	if conf == nil || !conf.Core.IsSourcesUsed() {
		return true
	}
	if strings.TrimSpace(root) == "" {
		return true
	}
	if _, ok := conf.Core.GraphJinSource(); !ok {
		return false
	}
	ctx = ms.effectiveContext(ctx)
	return ms.service.sourceModeRootAccessAllowed(root, runtimeRoleClass(ctx))
}

func (ms *mcpServer) toolVisibleForContext(ctx context.Context, tool string) bool {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return false
	}
	for _, root := range mcpToolRequiredRoots[tool] {
		if !ms.rootVisibleForContext(ctx, root) {
			return false
		}
	}
	return true
}

func (ms *mcpServer) toolAvailableForContext(ctx context.Context, tool string) bool {
	if !ms.toolVisibleForContext(ctx, tool) {
		return false
	}
	if ms.srv != nil {
		_, ok := ms.srv.ListTools()[tool]
		return ok
	}
	for _, name := range mcpToolList(ms.config()) {
		if name == tool {
			return true
		}
	}
	return false
}

func (ms *mcpServer) recommendedMCPEntrypoint(ctx context.Context) string {
	switch {
	case ms.toolAvailableForContext(ctx, "query_catalog"):
		return `query_catalog(search: "<user instruction>")`
	case ms.toolAvailableForContext(ctx, "graphql_help"):
		return `graphql_help(for: "discovery")`
	case ms.toolAvailableForContext(ctx, "execute_saved_query"):
		return "execute_saved_query"
	default:
		return "request authenticated/admin MCP access"
	}
}

func (ms *mcpServer) visibleCatalogCapabilities(ctx context.Context) []MCPCatalogCapability {
	rows, err := ms.queryCatalogGraphQL(ctx, catalogGraphQLQuery{
		Where: map[string]any{"kind": map[string]any{"in": []any{"capability", "system_capability", "entrypoint", "config_recipe"}}},
		OrderBy: map[string]string{
			"kind": "asc",
			"name": "asc",
		},
		Limit: 100,
	})
	if err != nil {
		return nil
	}
	out := make([]MCPCatalogCapability, 0, len(rows))
	for _, row := range rows {
		out = append(out, MCPCatalogCapability{
			ID:             row.ID,
			Kind:           row.Kind,
			Name:           row.Name,
			Summary:        row.Summary,
			Enabled:        row.Enabled,
			CapabilityKind: row.CapabilityKind,
		})
	}
	return out
}

func (ms *mcpServer) catalogResultNeedsCapabilityProfile(q catalogGraphQLQuery, rows []CatalogItem) bool {
	if q.ID == "" {
		return true
	}
	if len(rows) == 0 {
		return false
	}
	switch rows[0].Kind {
	case "help", "config_recipe", "capability", "system_capability":
		return true
	default:
		return false
	}
}

func (ms *mcpServer) applyCallerToolMetadata(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
	ctx = ms.effectiveContext(ctx)
	out := make([]mcp.Tool, 0, len(tools))
	for _, tool := range tools {
		if !ms.toolVisibleForContext(ctx, tool.Name) {
			continue
		}
		mergeGraphJinToolMeta(&tool, ms.toolGraphJinMeta(tool.Name))
		tool.Annotations = ms.toolAnnotations(tool.Name)
		out = append(out, tool)
	}
	return out
}

func mergeGraphJinToolMeta(tool *mcp.Tool, graphjin map[string]any) {
	if tool == nil {
		return
	}
	if tool.Meta == nil {
		tool.Meta = mcp.NewMetaFromMap(map[string]any{})
	}
	if tool.Meta.AdditionalFields == nil {
		tool.Meta.AdditionalFields = make(map[string]any)
	}
	tool.Meta.AdditionalFields["graphjin"] = graphjin
}

func (ms *mcpServer) toolGraphJinMeta(tool string) map[string]any {
	meta := map[string]any{
		"category":         mcpToolCategory(tool),
		"capability_ids":   mcpToolCapabilityIDs(tool),
		"requires_roots":   mcpToolRequiredRoots[tool],
		"discovery_first":  mcpToolDiscoveryFirst(tool),
		"available_to_now": true,
	}
	if mcpToolConfigPreviewRequired(tool) {
		meta["config_preview_required"] = true
	}
	return meta
}

func mcpToolCategory(tool string) string {
	switch tool {
	case "graphql_help", "query_catalog", "get_catalog_card", "get_catalog_entrypoints", "get_catalog_capabilities":
		return "discovery"
	case "validate_where_clause":
		return "validation"
	case "execute_graphql", "execute_saved_query", "execute_workflow":
		return "execution"
	case "get_current_config", "update_current_config":
		return "config"
	case "reload_schema", "preview_schema_changes", "apply_schema_changes":
		return "schema"
	case "list_workflows", "save_workflow":
		return "workflow"
	default:
		return "legacy"
	}
}

func mcpToolCapabilityIDs(tool string) []string {
	switch tool {
	case "graphql_help", "query_catalog", "get_catalog_card", "get_catalog_entrypoints", "get_catalog_capabilities":
		return []string{"system_capability.gj_catalog.query"}
	case "get_current_config":
		return []string{"system_capability.gj_config.query"}
	case "update_current_config":
		return []string{"system_capability.gj_config.update"}
	case "execute_workflow":
		return []string{"system_capability.gj_workflow_execution.insert"}
	case "save_workflow", "list_workflows":
		return []string{"system_capability.gj_workflow.manage"}
	case "execute_graphql":
		return []string{"capability.raw_graphql_query"}
	case "execute_saved_query":
		return []string{"capability.saved_query.execute"}
	default:
		return nil
	}
}

func mcpToolDiscoveryFirst(tool string) bool {
	switch tool {
	case "execute_graphql", "execute_saved_query", "execute_workflow", "update_current_config", "apply_schema_changes", "apply_database_setup", "save_workflow":
		return true
	default:
		return false
	}
}

func mcpToolConfigPreviewRequired(tool string) bool {
	return tool == "update_current_config"
}

func (ms *mcpServer) toolAnnotations(tool string) mcp.ToolAnnotation {
	readOnly, destructive, idempotent, openWorld := true, false, true, false
	switch tool {
	case "execute_graphql", "update_current_config", "apply_schema_changes", "apply_database_setup", "save_workflow":
		readOnly, destructive, idempotent, openWorld = false, true, false, true
	case "execute_saved_query", "execute_workflow":
		readOnly, destructive, idempotent, openWorld = false, false, false, true
	case "reload_schema":
		readOnly, destructive, idempotent, openWorld = false, false, false, false
	case "preview_schema_changes":
		readOnly, destructive, idempotent, openWorld = false, false, true, false
	}
	return mcp.ToolAnnotation{
		ReadOnlyHint:    mcpBoolPtr(readOnly),
		DestructiveHint: mcpBoolPtr(destructive),
		IdempotentHint:  mcpBoolPtr(idempotent),
		OpenWorldHint:   mcpBoolPtr(openWorld),
	}
}

func mcpBoolPtr(v bool) *bool {
	return &v
}
