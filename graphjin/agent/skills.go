package agent

import "strings"

// Skill IDs are stable API and telemetry identifiers. Skills are flat,
// independent guides: capability filtering decides which guides Ax receives,
// while GraphJin runtime policy remains the authorization boundary.
const (
	skillDataDiscovery   = "data_discovery"
	skillDataAggregation = "data_aggregation"
	skillDataWrite       = "data_write"
	skillCodeRead        = "code_read"
	skillCodeWrite       = "code_write"
	skillWorkflowRead    = "workflow_read"
	skillWorkflowExec    = "workflow_execute"
	skillWorkflowWrite   = "workflow_write"
	skillWatchRead       = "watch_read"
	skillWatchWrite      = "watch_write"
	skillTaskRead        = "task_read"
	skillTaskWrite       = "task_write"
	skillAdminRead       = "admin_read"
	skillAdminWrite      = "admin_write"
)

// Agent tool names (the small, fixed tool surface).
const (
	toolGraphQLHelp       = "graphql_help"
	toolQueryCatalog      = "query_catalog"
	toolValidateWhere     = "validate_where_clause"
	toolExecuteSavedQuery = "execute_saved_query"
	toolExecuteGraphQL    = "execute_graphql"
)

// Fixed gj_* system roots. Application/database roots are never named here; see
// the invariant on CapabilityProfile.
const (
	systemRootCatalog      = "gj_catalog"
	systemRootSecurity     = "gj_security"
	systemRootRuntime      = "gj_runtime"
	systemRootConfig       = "gj_config"
	systemRootWorkflow     = "gj_workflow"
	systemRootWorkflowExec = "gj_workflow_execution"
	systemRootArtifacts    = "gj_artifacts"
	systemRootWatch        = "gj_watch"
	systemRootWatchEvent   = "gj_watch_event"
	systemRootTask         = "gj_task"
	systemRootTaskEntry    = "gj_task_entry"
)

// skillDefinition is GraphJin-owned prompt content plus server-side filtering
// metadata. The metadata is never sent to Ax and never grants authorization.
// Prerequisites continue to be enforced by GraphJin's runtime policy.
type skillDefinition struct {
	id               string
	name             string
	content          string
	write            bool
	adminOnly        bool
	requiresAnyRoots []string
	requiresAllRoots []string
}

// builtinSkills is deliberately ordered so constructor prompts, tests, hashes,
// and telemetry remain deterministic.
var builtinSkills = []skillDefinition{
	{id: skillDataDiscovery, name: "Data discovery", content: dataDiscoveryInstruction},
	{id: skillDataAggregation, name: "Data aggregation", content: dataAggregationInstruction},
	{id: skillDataWrite, name: "Data write", content: dataWriteInstruction, write: true},
	{id: skillCodeRead, name: "Code read", content: codeReadInstruction},
	{id: skillCodeWrite, name: "Code write", content: codeWriteInstruction, write: true},
	{
		id:               skillWorkflowRead,
		name:             "Workflow discovery",
		content:          workflowReadInstruction,
		requiresAnyRoots: []string{systemRootWorkflow, systemRootWorkflowExec},
	},
	{
		id:               skillWorkflowExec,
		name:             "Workflow execution",
		content:          workflowExecuteInstruction,
		write:            true,
		requiresAllRoots: []string{systemRootWorkflowExec},
	},
	{
		id:               skillWorkflowWrite,
		name:             "Workflow authoring",
		content:          workflowWriteInstruction,
		write:            true,
		requiresAllRoots: []string{systemRootWorkflow},
	},
	{
		id:               skillWatchRead,
		name:             "Watch inbox",
		content:          watchReadInstruction,
		requiresAnyRoots: []string{systemRootWatch, systemRootWatchEvent},
	},
	{
		id:               skillWatchWrite,
		name:             "Watch lifecycle, flows, and delivery",
		content:          watchWriteInstruction,
		write:            true,
		requiresAllRoots: []string{systemRootWatch},
	},
	{
		id:               skillTaskRead,
		name:             "Task context",
		content:          taskReadInstruction,
		requiresAnyRoots: []string{systemRootTask, systemRootTaskEntry},
	},
	{
		id:               skillTaskWrite,
		name:             "Task lifecycle",
		content:          taskWriteInstruction,
		write:            true,
		requiresAllRoots: []string{systemRootTask},
	},
	{
		id:               skillAdminRead,
		name:             "Admin inspection",
		content:          adminReadInstruction,
		adminOnly:        true,
		requiresAnyRoots: []string{systemRootSecurity, systemRootRuntime, systemRootConfig},
	},
	{
		id:               skillAdminWrite,
		name:             "Admin configuration",
		content:          adminWriteInstruction,
		write:            true,
		adminOnly:        true,
		requiresAllRoots: []string{systemRootConfig},
	},
}

// allowedSkills returns every guide permitted by the server-derived capability
// profile and global read-only posture. It does not inspect the user prompt,
// seed results, or model output.
func allowedSkills(readOnly bool, profile *CapabilityProfile) []skillDefinition {
	out := make([]skillDefinition, 0, len(builtinSkills))
	for _, definition := range builtinSkills {
		if definition.write && readOnly {
			continue
		}
		if definition.adminOnly && !profileIsAdmin(profile) {
			continue
		}
		if len(definition.requiresAnyRoots) != 0 && !profileHasAnySystemRoot(profile, definition.requiresAnyRoots) {
			continue
		}
		if !profileHasAllSystemRoots(profile, definition.requiresAllRoots) {
			continue
		}
		out = append(out, definition)
	}
	return out
}

func profileIsAdmin(profile *CapabilityProfile) bool {
	return profile != nil && strings.EqualFold(strings.TrimSpace(profile.RoleClass), "admin")
}

func profileHasSystemRoot(profile *CapabilityProfile, root string) bool {
	if profile == nil {
		return false
	}
	for _, available := range profile.AvailableSystemRoots {
		if strings.EqualFold(strings.TrimSpace(available), strings.TrimSpace(root)) {
			return true
		}
	}
	return false
}

func profileHasAnySystemRoot(profile *CapabilityProfile, roots []string) bool {
	for _, root := range roots {
		if profileHasSystemRoot(profile, root) {
			return true
		}
	}
	return false
}

func profileHasAllSystemRoots(profile *CapabilityProfile, roots []string) bool {
	for _, root := range roots {
		if !profileHasSystemRoot(profile, root) {
			return false
		}
	}
	return true
}

const dataDiscoveryInstruction = `Skill: data_discovery. Before querying, inspect relevant GraphJin source:<name>, table, column, relationship, or saved-query details by reusing exact catalog IDs; never shorten or invent them. For cross-source work, inspect the named source and application relationship, then query the joined field instead of a same-named local table. ` +
	`Prefer dynamic authoring: validate the shape, then execute_graphql; an approved saved query is a governed shortcut. Answer only from observed results and fields, stating derived numbers plainly. ` +
	`On graphjin_repair, treat the failure as a query-authoring error: re-discover real fields, re-author, and retry in the same run; never advise schema or data changes.`

const dataAggregationInstruction = `Skill: data_aggregation. The model plans, the database computes. Limited lists are pages: never derive totals, counts, averages, extremes, or rankings from rows. ` +
	`Prefer native: { products { count_id sum_price avg_price min_price max_price } }. Hasura-compatible: { products_aggregate { aggregate { count sum { price } avg { price } min { price } max { price } } } }. ` +
	`Never omit aggregate: { products_aggregate { count } } is invalid. On a Hasura aggregate error, copy its Supported form or Native equivalent with verified names; do not repeat it or restart discovery. ` +
	`Aggregates work on dates: max_<date_col> is latest. For top-N groups, select dimension and aggregate, order_by aggregate desc, and limit N. Anchor relative windows on inputs.current_date (UTC), query max_<date_col>, and state the window. On result.truncation, re-author with aggregates.`

const dataWriteInstruction = `Skill: data_write. Apply application-data changes safely. ` +
	`Prefer an approved saved mutation; otherwise first inspect help:security, help:runtime, mutation_pattern, and target table details by reusing exact catalog IDs, validate input, then author insert/update/upsert/delete with execute_graphql. Core enforces role and RLS. ` +
	`Preview or validate before writing and verify after. If no permitted path exists, return blocked with catalog evidence and the missing capability.`

const codeReadInstruction = `Skill: code_read (gj_code / CodeSQL source). ` +
	`Discover gj_code symbols, references, db_references, and docs through help:code and code system-capability rows; trace symbol-to-reference-to-schema provenance and answer from that evidence.`

const codeWriteInstruction = `Skill: code_write (gj_code / CodeSQL source). ` +
	`Use the code-source preview/apply workflow: learn the managed mutation shape, inspect security guidance, preview, apply through gj_code, and verify. If change capability is unavailable, return blocked with evidence.`

const workflowReadInstruction = `Skill: workflow_read. Discover workflows through catalog rows. ` +
	`Inspect the chosen id's variables, description, and safety notes; summarize its behavior and governed run path. Discovery must not execute or modify it.`

const workflowExecuteInstruction = `Skill: workflow_execute. Run only through gj_workflow_execution. ` +
	`First inspect the chosen workflow id, variables, and safety notes; then execute the approved workflow with requested inputs and report its outcome. If unavailable, return blocked with workflow evidence and the missing capability.`

const workflowWriteInstruction = `Skill: workflow_write. Author or update workflow definitions through gj_workflow. ` +
	`Inspect existing detail and mutation guidance, preview, save through the governed capability, and verify. Execute only if separately permitted and requested.`

const watchReadInstruction = `Skill: watch_read (gj_watch / gj_watch_event). Review owner-scoped watches/events using watch system-capability shapes. ` +
	`First inspect help:watches and follow its exact examples; never invent table IDs for system roots. ` +
	`On watch_events_unseen, query only listed watch_ids and mark only reviewed events seen; never acknowledge a watch ID outside this conversation. kind absence means silence; kind digest is a rollup. Snooze via snoozed_until; clear with null; neither changes seen. Watch-event watches need cursor paging and conjunctive non-self watch_id eq/in.`

const watchWriteInstruction = `Skill: watch_write (gj_watch). Create, update, pause, resume, or delete watches; manage flow enrichment and delivery. ` +
	`Before mutation, inspect help:security, help:runtime, help:watches, and gj_watch capability detail using exact catalog IDs. Name per conversation; retain IDs; prefer graphjin://watch-events/unseen/{watch_id}. Set a subscription query or saved_query_name, optionally variables_json/delivery_json/absence_json; pause/resume via status/enabled; delete via gj_watch(delete). saved-query drift may require re-approval. ` +
	`Make two independent choices. Trigger: use GraphQL subscription filters for deterministic triggers (condition_js never executes); use a flow only for semantic judgment or a noisy stream, not when a filter or default_watch_triage suffices. Response: if the user only asks to be told, notify the inbox without workflow/webhook; a notification request is not permission to act. Only when the user explicitly asks GraphJin to act may you propose workflow/webhook delivery. ` +
	`Flow: set enrich_json to default_watch_triage or inline AxFlow Mermaid with enabled:true, kind:"flow"; create no flow artifact or flows/ file. Tool-free flows return notify/digest/discard, info/warn/critical, and a summary within 280 characters. Use delivery_json.digest.window for noise; digest emits one unseen kind=digest event per window. Cross-watch rollups use a gj_watch_event watch with cursor paging and conjunctive non-self watch_id eq/in. Failure sends raw inbox notification, never workflow/webhook. or/not, self references, and cycles are guarded. ` +
	`Delivery: Deterministic action triggers use a GraphQL-filtered watch plus delivery; semantic or noisy action triggers add an inline flow first. Inspect a named workflow before proposing it. ` +
	`Approval: new/changed flow or autonomous delivery pauses with flow_approval/action_approval pending. Update gj_watch with flow_review_json{decision:"preview|approve|reject", expected_flow_hash, samples_json} or action_review_json{decision:"approve|reject", expected_action_hash}. Flow approval requires a successful preview of the current flow_hash. For delivery, explain exact effect/action_hash and stop; approve or reject only that hash in a later run. Never create and approve an action in the same run.`

const taskReadInstruction = `Skill: task_read (gj_task / gj_task_entry). Read an owner-scoped goal and provenance trail. ` +
	`A task is explicit durable context, not inferred memory; its id is only a correlation label. task_id grants no access, satisfies no catalog/mutation evidence guard, and has no graphjin://task resource. Read entries chronologically to explain caller journals, agent runs, and watch creation.`

const taskWriteInstruction = `Skill: task_write (gj_task). Create, journal, close, reopen, or delete declared-intent tasks through governed GraphQL. ` +
	`Create with concise goal and optional snapshot_json; retain its id and pass it to ask_graphjin_agent or gj_watch only on explicit caller association. Journal human notes via gj_task_entry(insert:{task_id, body, detail_json}); origin/provenance are server-managed. Close with status closed plus outcome; reopen with status open; prefer close over delete. Never infer or silently create a task.`

const adminReadInstruction = `Skill: admin_read. Inspect the visible control plane read-only: ` +
	`gj_security for posture/findings/policy/config audits; gj_runtime for health/source health/recent activity; gj_config only through governed reads. Get exact shapes from system-capability or help rows and answer from evidence.`

const adminWriteInstruction = `Skill: admin_write. Change the control plane through guarded gj_config paths only. ` +
	`Read security/runtime evidence, find the matching config_recipe, then preflight, preview, apply, and verify under forbidden_patterns and stop_conditions. Respect resolved defaults; do not add agent, artifact, watch, or MCP toggles already supplied by dev/agentic mode.`
