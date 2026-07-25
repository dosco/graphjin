package serv

import "strings"

func mcpServerInstructions(conf *Config, profiles ...*MCPCapabilityProfile) string {
	if conf != nil && conf.mcpDisabled() {
		return disabledServerInstructions
	}
	var profile *MCPCapabilityProfile
	if len(profiles) != 0 {
		profile = profiles[0]
	}
	return mcpCallerAwareInstructionBlock(profile) + catalogServerInstructions
}

func mcpCallerAwareInstructionBlock(profile *MCPCapabilityProfile) string {
	if profile == nil {
		return ""
	}
	var lines []string
	lines = append(lines, "## Caller-aware MCP surface")
	if profile.RecommendedEntrypoint != "" {
		lines = append(lines, "Recommended entrypoint: "+profile.RecommendedEntrypoint+".")
	}
	if len(profile.AvailableTools) != 0 {
		lines = append(lines, "Available now: "+strings.Join(profile.AvailableTools, ", ")+".")
	}
	if roots := mcpRootNames(profile.AvailableRoots); len(roots) != 0 {
		lines = append(lines, "Available roots: "+strings.Join(roots, ", ")+".")
	}
	if roots := mcpRootNames(profile.BlockedRoots); len(roots) != 0 {
		lines = append(lines, "Not available to this caller: "+strings.Join(roots, ", ")+".")
	}
	if !mcpRootListContains(profile.AvailableRoots, "gj_catalog") {
		lines = append(lines, "If gj_catalog is not available, stop and request authenticated/admin access instead of inventing schema.")
	}
	lines = append(lines, "Use live tools/list, graphql_help, or query_catalog responses as authoritative; disconnected proxy initialize text is only a temporary hint.")
	return strings.Join(lines, "\n") + "\n\n"
}

func mcpRootNames(roots []MCPRootProfile) []string {
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		if strings.TrimSpace(root.Root) != "" {
			out = append(out, root.Root)
		}
	}
	return out
}

func mcpRootListContains(roots []MCPRootProfile, name string) bool {
	for _, root := range roots {
		if strings.EqualFold(root.Root, name) {
			return true
		}
	}
	return false
}

const disabledServerInstructions = `GraphJin MCP is disabled by configuration.

No MCP discovery, prompt, resource, execution, workflow, config, schema, or catalog tools are available from this server. Do not recommend MCP tool calls unless MCP is enabled in configuration.
`

const catalogServerInstructions = `GraphJin is a GraphQL-to-SQL compiler. You query databases using GraphJin's own DSL (not standard GraphQL).

## Catalog-first operating loop

GraphJin MCP intentionally exposes a tiny bootstrap surface:

query_catalog(search: "<user instruction>") -> query_catalog(id) -> validate_where_clause -> execute_saved_query

When raw execution is explicitly enabled, execute_graphql is also available as an action tool. Prefer execute_saved_query when a matching saved query exists.

Discovery means selecting evidence-backed catalog items before acting. Do not write queries from memory.

1. For goal-driven work, first call query_catalog(search: "<user instruction>"). Use the user's own request text as the first search string.
2. When the user intent is unclear or catalog search returns no useful rows, call graphql_help(for: "discovery"). It returns curated catalog rows plus the exact gj_catalog GraphQL query it used.
3. Use returned recipe/help/topic rows with query_catalog(id: "...") for full guidance before acting.
4. Query gj_catalog to find relevant schema, relationship, workflow, language, config_recipe, config, policy, capability, query-pattern, mutation-pattern, or common-mistake items. Use search for intelligent ranked text search, and where for precise GraphJin-style filters.
5. Select details_json, evidence_json, examples_json, safety_json, and edges_json on the best matching gj_catalog item before choosing tables, columns, relationships, operators, or actions. In tool terms, inspect details, evidence, examples, safety notes, and nearby graph edges.
6. Resolve ambiguity by inspecting candidate items. If multiple tables or columns match, do not guess from names alone.
7. Use validate_where_clause before filters that depend on column types, operators, or real values.
8. Prefer execute_saved_query for approved allow-list queries. Use execute_graphql only when the server exposes it and raw execution is enabled.
9. In agentic mode, query gj_runtime before workflow, config, or schema actions, after GraphJin errors, and when results suggest stale schema, disconnected databases, degraded Redis, reload, discovery, or catalog refresh problems. Use gj_runtime for decision support, not audit history; if status is degraded, follow next_action before continuing.
10. Before config, workflow, schema, file, or code-source changes, inspect the relevant config_recipe or gj_security.query catalog row, then query gj_security for high/critical findings and effective policy.
11. Prefer GraphJin control-plane GraphQL mutations for workflow/config actions after discovery: gj_workflow_execution(insert) for workflow execution, gj_workflow(insert/update/delete) for workflow management, gj_artifacts(insert/update/delete) for saved queries/fragments/workflow artifacts, gj_watch(insert/update/delete) including flow_review_json and action_review_json, gj_watch_event(update) for the watch inbox, and gj_config(id: "current", update: ...) for config changes. Use validate_where_clause for filter checks; use catalog/config rows for schema refresh guidance; use errors[].extensions.graphjin_repair for query repair.
12. Prefer workflows for broad data questions after discovery. Workflows can page and aggregate safely.
13. Observe results, then return to catalog items or gj_runtime when the result, error, or follow-up question changes the facts you need.

Topic routing:

| Need | First call | Detailed row |
| :--- | :--- | :--- |
| user goal or operator request | query_catalog(search: "<user instruction>") | query_catalog(id: "<best_result_id>") |
| unknown start or old MCP tool mapping | graphql_help(for: "discovery") | query_catalog(id: "help:discovery") |
| MCP tools | graphql_help(for: "mcp_tools") | query_catalog(id: "help:mcp_tools") |
| schema overview | graphql_help(for: "schema") | query_catalog(id: "help:schema") |
| tables | graphql_help(for: "tables") | query_catalog(id: "help:tables") |
| columns and field safety | graphql_help(for: "columns") | query_catalog(id: "help:columns") |
| relationships and join paths | graphql_help(for: "relationships") | query_catalog(id: "help:relationships") |
| query DSL | graphql_help(for: "query") | query_catalog(id: "help:query") |
| filters | graphql_help(for: "filters") | query_catalog(id: "help:filters") |
| mutations | graphql_help(for: "mutations") | query_catalog(id: "help:mutations") |
| saved queries | graphql_help(for: "saved_queries") | query_catalog(id: "help:saved_queries") |
| fragments | graphql_help(for: "fragments") | query_catalog(id: "help:fragments") |
| workflows | graphql_help(for: "workflows") | query_catalog(id: "help:workflows") |
| workflow runtime | graphql_help(for: "workflow_runtime") | query_catalog(id: "help:workflow_runtime") |
| config | query_catalog(search: "<user instruction>") | query_catalog(id: "help:config") |
| security | query_catalog(search: "<user instruction>") | query_catalog(id: "help:security") |
| runtime status and recent structured events | graphql_help(for: "runtime") | query_catalog(id: "help:runtime") |
| saved artifacts (queries, fragments, workflows) | graphql_help(for: "artifacts") | query_catalog(id: "help:artifacts") |
| watches and their event inbox | graphql_help(for: "watches") | query_catalog(id: "help:watches") |
| blocked agent responses / refusals | graphql_help(for: "refusals") | query_catalog(id: "help:refusals") |
| automatic agent model selection | graphql_help(for: "sampling") | query_catalog(id: "help:sampling") |
| code/source changes | graphql_help(for: "code") | query_catalog(id: "help:code") |
| errors | graphql_help(for: "errors") | query_catalog(id: "help:errors") |

Legacy discovery MCP tools are gone from MCP. Their prompt knowledge now lives in mcpServerInstructions, graphql_help, help:* catalog rows, examples_json, evidence_json, safety_json, and errors[].extensions.graphjin_repair. Use graphql_help(for: "mcp_tools") for the old-to-new map.

## Discovery recipes

- Goal-driven discovery starts with query_catalog(search: "<user instruction>"); catalog discovery then uses query_catalog(id) for the best result. The canonical GraphQL root is still gj_catalog.
- Help routing uses graphql_help(for: "mcp_tools" | "query" | "filters" | "schema" | "workflows" | "config" | "security" | "runtime" | "code" | "errors" | ...). The response includes graphql_query so you can reuse or modify the exact gj_catalog query shape.
- Compatibility catalog query shape: query_catalog(search: "workflow", where: { kind: { eq: "workflow" } }).
- Canonical catalog query shape: gj_catalog(search: "join orders customers", where: { kind: { eq: "relationship" } }, order_by: { search_rank: desc }) { id kind name summary details_json edges_json }.
- Schema discovery: gj_catalog(where: { kind: { eq: "table" } }) { id name summary details_json } to find tables and table evidence.
- Column discovery: gj_catalog(where: { kind: { eq: "column" }, table_name: { eq: "<table>" } }) { id name type summary evidence_json } to find columns, types, sensitivity notes, indexes, and filter hints.
- Relationship discovery: gj_catalog(search: "join <source> <target>", where: { kind: { eq: "relationship" } }) { id name summary evidence_json edges_json } before nesting related selectors.
- GraphJin language discovery: gj_catalog(where: { kind: { in: ["directive", "operator_set", "query_pattern", "mutation_pattern"] } }) { id kind name summary examples_json } before using GraphJin-specific syntax.
- Analytics discovery: gj_catalog(search: "running revenue rank window", where: { kind: { in: ["directive", "query_pattern", "deprecated_feature"] } }, order_by: { search_rank: desc }) { id kind name summary examples_json } for reporting/window-style features.
- Value-sensitive filters: if a filter depends on real status strings, enum labels, tenant names, or conventions, inspect sample/profile availability on table or column items before choosing values.
- Config and policy discovery: query_catalog(search: "<user instruction>") should return config_recipe rows for role, identity, source access, table classification, artifacts, GraphJin roots, and legacy roles[].tables migration. Inspect the recipe row before any gj_config mutation. In source mode, config writes use gj_config mode: "preview" with expected_catalog_revision, then mode: "apply" with preview_id and the exact same payload; use source_patches only for external-source access and use the top-level system patch for built-in root policy.
- Security posture: find guidance with gj_catalog(where: { kind: { eq: "system_capability" }, name: { eq: "gj_security.query" } }) { name summary details_json examples_json safety_json }, then query gj_security(where: { kind: { eq: "finding" }, severity: { in: ["high", "critical"] } }) { id scope config_id mode severity title recommendation evidence_json }. In agentic deployments, normal company users should rely on gj_catalog and approved gj_workflow_execution(insert); detailed gj_security, gj_config, and gj_workflow.code require an explicit authenticated grant.
- Runtime status: in agentic mode, find guidance with gj_catalog(where: { kind: { eq: "system_capability" }, name: { eq: "gj_runtime.query" } }) { name summary details_json examples_json safety_json }, then query gj_runtime(where: { kind: { in: ["status", "source", "event"] } }, order_by: { created_at: desc }, limit: 20) { kind source source_kind status severity summary next_action details_json }. Use this before guarded workflow/config/schema actions, after errors, and when stale schema, source health, disconnected DBs, degraded Redis, reload, discovery, or catalog refresh issues are suspected. gj_runtime is bounded decision support, not audit history.
- Workflow reuse: gj_catalog(search: "workflow", where: { kind: { eq: "workflow" } }) { id name summary input_schema_json } to find reusable workflows, then run mutation { gj_workflow_execution(insert: { workflow_name: "...", variables: {...} }) { status result_json error duration_ms } }. gj_workflow_execution is mutation-only and returns an ephemeral result row; it does not store run history. Disable workflows.capabilities.execute to remove execution for every caller; root access cannot re-enable it.
- Workflow create/update/delete: use mutation { gj_workflow(insert/update/delete: ...) { name source_hash catalog_revision } } when mcp.allow_workflow_updates is enabled.
- Artifact store: gj_artifacts is the owner-scoped store behind saved queries, fragments, and workflows. Save with mutation { gj_artifacts(insert: { name: "...", kind: "query", content: "..." }) { id name } }. Projection rows are a bounded search index: when content_truncated is true (or a JSON field is null in the projection), read the full row through gj_artifacts.
- Watches (standing questions): create with a unique per-conversation name and a cursor-backed subscription, e.g. mutation { gj_watch(insert: { name: "...", query: "subscription { orders(first: 25, after: $cursor) { id status } orders_cursor }" }) { id lifecycle status } } (or saved_query_name). Retain the returned watch ID: watch IDs are derived from owner plus name, and a per-watch-aware MCP host should RFC 6570-expand graphjin://watch-events/unseen/{watch_id} (percent-encoding reserved characters in the ID) and subscribe to that concrete resource. Watches are owner-scoped, durable by default, and cursor-resume across restarts. Parsed dev/agentic configs use runner all automatically, but no application subscription starts until an enabled, active, approved watch exists; prod/direct configs retain runner off. Use lifecycle: "ephemeral" plus a future lease_expires_at only when the user explicitly asks for a TTL, such as "watch this for 30 minutes"; expired ephemeral watches become status "expired" and enabled false. The aggregate graphjin://watch-events/unseen resource remains available for clients without per-URI subscription support; when using it, filter entries to this conversation's retained watch IDs before reading full events or acknowledging them. Pause/resume via status/enabled updates, delete via gj_watch(delete), review gj_watch_event(where: { watch_id: { eq: "<retained_watch_id>" }, seen: { eq: false } }, order_by: { created_at: desc }), and mark only reviewed events from retained watch IDs seen with gj_watch_event(update: { seen: true }). Use POST /api/v1/watches/cleanup-preview before /api/v1/watches/cleanup-apply; do not delete durable watches unless the user explicitly asks.
- Watching watch events: use an ascending cursor-backed subscription such as subscription watch_watch($watch_ids: [String!]!, $gj_watch_event_cursor: Cursor) { gj_watch_event(first: 25, after: $gj_watch_event_cursor, where: { watch_id: { in: $watch_ids } }, order_by: { created_at: asc }) { id watch_id data_hash created_at } gj_watch_event_cursor }. The watch_id eq/in filter must be conjunctive, non-empty, and exclude the new watch's own ID; GraphJin rejects global dependency cycles. Resume is best-effort within configured watch-event projection retention.
- Watch choice has two independent axes. Deterministic trigger plus notification means a plain GraphQL-filtered watch. Semantic or noisy trigger plus notification means a watch with a flow. Deterministic trigger plus an explicitly requested action means a watch with workflow or webhook delivery. Semantic or noisy trigger plus an explicitly requested action means flow plus delivery. "Tell me" and "let me know" request notification, not autonomous action. Put deterministic conditions in the GraphQL filter; condition_js is stored but is not executed.
- Watch flows: use enrich_json: { enabled: true, kind: "flow", flow: "default_watch_triage" } or put inline AxFlow Mermaid in flow. Flow content belongs to the watch; do not create a flow artifact or flows/ file. A new or changed flow pauses the watch with flow_approval pending and returns flow_hash. Preview it through gj_watch(where: { id: { eq: "..." } }, update: { flow_review_json: { decision: "preview", expected_flow_hash: "...", samples_json: [...] } }) and approve that same hash with decision: "approve"; approval requires a successful stored preview. The flow must return verdict notify/digest/discard, severity info/warn/critical, and summary of at most 280 characters. Flows have no tools or GraphJin bindings. notify remains unseen and wakes subscribers; digest queues until delivery_json.digest.window (default 1h) then creates one unseen data_json.kind="digest" event without another model call; discard becomes suppressed without a wake. Any flow/model/cap/output failure sends the raw inbox event, but it must never execute a workflow or webhook. Absence of a flow means no model call.
- Watch absence and snooze: use absence_json: { enabled: true, window: "4h", repeat: false } for "no data in 4h"; its synthetic event has data_json.kind="absence". Snooze an event through gj_watch_event(update: { snoozed_until: "<future RFC3339>" }) and clear with null; snoozing does not mark it seen.
- Rollup watches: for cross-watch correlation subscribe a durable watch to gj_watch_event with a conjunctive non-self watch_id eq/in filter. or/not filters and self references are rejected, the DAG guard rejects cycles, and saved-query dependency drift pauses the watch. Use digest for same-watch noise and a rollup watch for correlation across watch IDs. Relevant definition changes can require hash re-approval.
- Watch actions: workflow or webhook delivery is autonomous and must be explicitly requested. Creating or changing one pauses the watch with action_approval pending and returns action_hash. The hash pins query source, variables, flow, delivery configuration, and resolved workflow source. Explain the proposed action and stop for user confirmation; never create/change and approve it in the same agent run. On a later confirmed request, submit gj_watch(where: { id: { eq: "..." } }, update: { action_review_json: { decision: "approve", expected_action_hash: "..." } }). Relevant definition or workflow-source changes require review again.
- Watch notices: agent responses may include notices[] with kind "watch_events_unseen", count, and watch_ids. Query and acknowledge only the listed watch IDs. A notifications/resources/updated URI identifies which watch changed, but read that resource or gj_watch_event to retrieve event data.
- Refusal handling: when an agent response is blocked, read refusal { code because unblock lawful_alternative policy_final retryable }. Execute the unblock steps (each names a tool and args), then retry only if retryable; policy_final means do not retry — follow lawful_alternative or escalate to an operator.
- Model selection is automatic and server-first: configured server credentials always win; without them, ask_graphjin_agent requires this MCP client to support sampling/createMessage. agent.sampling "off" disables that client-model fallback. Caller identity, permissions, and evidence gates are unchanged.
- GraphQL catalog discovery: use gj_catalog as the single discovery root, for example gj_catalog(where: { kind: { eq: "table" } }) { id kind name summary } or gj_catalog(where: { kind: { eq: "capability" } }) { name summary safety_json }.
- Config and schema actions: use gj_config(id: "current", update: ...) for config changes; source-mode updates require preview/apply. MCP no longer exposes legacy schema reload/change helper tools.
- Query repair: inspect errors[].extensions.graphjin_repair, then inspect relevant schema or language items before retrying.

## Key DSL rules

- GROUP BY does not exist. Use distinct: [columns] instead.
- Aggregation fields use the pattern <fn>_<column>: count_id, sum_price, avg_quantity, etc.
- For arithmetic metrics such as revenue = price * qty, use expression aggregates like sum(expr: { mul: [price, qty] }).
- Analytics/reporting directives attach to real columns. Use @running, @moving, @previous, @next, @first, @last, @rank, @denseRank, and @rowNumber.
- Filter operators are typed. Use validate_where_clause when unsure.
- in/nin values MUST be arrays: { id: { in: [1,2,3] } }.
- Every query level has a default row limit. Set explicit limits on top-level and nested selectors.
- Never infer relationship paths from column names alone. Use relationship items, evidence, and graph edges.

## Discovery vs action

Catalog tools own nouns, facts, context, and evidence. In GraphQL terms, gj_catalog is the single catalog discovery root: filter by kind for tables, columns, relationships, workflows, language features, entrypoints, capabilities, and system capabilities. Control-plane GraphQL mutation roots own workflow/config verbs: gj_workflow, gj_workflow_execution, and gj_config. MCP action tools handle where-clause validation and query execution. Discover first, act second.
`
