package agent

import "strings"

// Skill IDs are stable API and telemetry identifiers. Skills are flat,
// independent guides: capability filtering decides which guides Ax receives,
// while GraphJin runtime policy remains the authorization boundary.
const (
	skillDataDiscovery = "data_discovery"
	skillDataWrite     = "data_write"
	skillCodeRead      = "code_read"
	skillCodeWrite     = "code_write"
	skillWorkflowRead  = "workflow_read"
	skillWorkflowExec  = "workflow_execute"
	skillWorkflowWrite = "workflow_write"
	skillWatchRead     = "watch_read"
	skillWatchWrite    = "watch_write"
	skillWatchFlow     = "watch_flow"
	skillWatchDelivery = "watch_delivery"
	skillAdminRead     = "admin_read"
	skillAdminWrite    = "admin_write"
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
		name:             "Watch lifecycle",
		content:          watchWriteInstruction,
		write:            true,
		requiresAllRoots: []string{systemRootWatch},
	},
	{
		id:               skillWatchFlow,
		name:             "Watch flow enrichment",
		content:          watchFlowInstruction,
		write:            true,
		requiresAllRoots: []string{systemRootWatch},
	},
	{
		id:               skillWatchDelivery,
		name:             "Watch actions",
		content:          watchDeliveryInstruction,
		write:            true,
		requiresAllRoots: []string{systemRootWatch},
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

const dataDiscoveryInstruction = `Skill: data_discovery. Discover application data through the GraphJin catalog before querying. ` +
	`Inspect the relevant table, column, relationship, or saved-query detail rows and use only returned names and relationship paths. Prefer an approved saved query; otherwise validate the discovered shape and use execute_graphql. Answer from observed results, not assumptions.`

const dataWriteInstruction = `Skill: data_write. Apply application-data changes safely. ` +
	`Prefer an approved saved mutation; otherwise learn the insert/update/upsert/delete shape from mutation_pattern and target-table detail rows, validate the input, and author the mutation with execute_graphql. Core enforces role and RLS on every write. ` +
	`Before any mutation, establish this run's shape evidence for each target table. Preview or validate before writing and verify the result afterwards. If no permitted write path exists, return blocked with the catalog evidence and missing capability.`

const codeReadInstruction = `Skill: code_read (gj_code / CodeSQL source). ` +
	`Investigate code by discovering gj_code symbols, references, db_references, and docs using help:code and code system-capability rows for exact shapes. Trace provenance from symbol to references to database schema, then answer from discovered evidence.`

const codeWriteInstruction = `Skill: code_write (gj_code / CodeSQL source). ` +
	`Change code through the code-source preview/apply workflow: learn the managed mutation shape, inspect security guidance, preview the edit, apply it through gj_code, and verify the result. If the change capability is unavailable, return blocked with evidence.`

const workflowReadInstruction = `Skill: workflow_read. Discover reusable workflows through workflow catalog rows. ` +
	`Inspect the chosen workflow by id, including variables, description, and safety notes, and summarize what it does and which governed path can run it. Discovery alone must not execute or modify it.`

const workflowExecuteInstruction = `Skill: workflow_execute. Run an existing workflow only through gj_workflow_execution. ` +
	`First inspect the chosen workflow detail by id, including variables and safety notes, then execute the approved workflow with the requested inputs and report its actual outcome. If workflow execution is unavailable, return blocked with the workflow evidence and missing capability.`

const workflowWriteInstruction = `Skill: workflow_write. Author or update workflow definitions through gj_workflow. ` +
	`Inspect existing workflow detail and exact mutation guidance, preview the definition, save through the governed workflow capability, and verify the stored result. Do not execute the workflow unless the separate execution capability is available and the request requires it.`

const watchReadInstruction = `Skill: watch_read (gj_watch / gj_watch_event). Review standing watches and their fired-event inbox. ` +
	`Discover exact query shapes from watch system-capability rows; watches and events are owner-scoped. For a watch_events_unseen notice, query gj_watch_event only for its listed watch_ids, then mark only those reviewed events seen; never acknowledge an event from a watch ID not associated with this conversation. ` +
	`A watch's status, last_error, and failure_count explain why it is not firing.`

const watchWriteInstruction = `Skill: watch_write (gj_watch). Create, update, pause, resume, or delete standing watches through the governed root. ` +
	`Before a mutation, inspect security guidance and the gj_watch capability detail. Create a unique per-conversation name and retain the returned ID. Use a subscription query or saved_query_name plus optional variables_json and delivery_json; pause or resume through status/enabled and remove through gj_watch(delete). ` +
	`Choose from two independent questions. If the trigger is deterministic, express it in the GraphQL subscription filter; condition_js is not executed. If the trigger is semantic, needs judgment, or the stream is noisy, attach a flow. If the user only asks to be told, use inbox notification and do not attach a workflow or webhook. If the user explicitly asks GraphJin to act after the trigger, propose workflow or webhook delivery and follow the separate action-review process. ` +
	`Per-watch MCP clients should subscribe to graphjin://watch-events/unseen/{watch_id}; aggregate clients must filter and acknowledge only retained watch IDs. Watches run durably under the owner's stored identity. Parsed dev and agentic configs already use runner all, but no subscription starts until an enabled active watch exists. Enabling watches after an explicit opt-out is an admin config change, not a watch mutation.`

const watchFlowInstruction = `Skill: watch_flow. Add, preview, and approve AxFlow enrichment for an existing watch. ` +
	`Use a flow only when the trigger needs semantic judgment or a noisy stream needs triage; do not hand-write one when a deterministic GraphQL filter or default_watch_triage suffices. Put either default_watch_triage or inline AxFlow Mermaid in enrich_json with enabled:true and kind:"flow"; do not create a flow artifact or flows/ file. ` +
	`A new or changed flow pauses with flow_approval pending. Preview or approve it through gj_watch(where:{id:{eq:"..."}}, update:{flow_review_json:{decision:"preview|approve|reject", expected_flow_hash:"...", samples_json:[...]}}). Approval must match a successful preview of the current flow_hash. ` +
	`Flows have no tools and must return notify/digest/discard, info/warn/critical, and a summary no longer than 280 characters. Flow failure sends a raw inbox notification but never executes a workflow or webhook.`

const watchDeliveryInstruction = `Skill: watch_delivery. Attach workflow or webhook delivery only when the user explicitly asks GraphJin to perform an action after a watch fires. ` +
	`A notification request such as "tell me" or "let me know" is not permission to act. Deterministic action triggers use a GraphQL-filtered watch plus delivery; semantic or noisy action triggers add an inline watch flow before delivery. Inspect a named workflow before proposing it. ` +
	`Creating or changing autonomous delivery pauses the watch and returns action_hash with action_approval pending. Explain the exact proposed effect and hash, then stop for user confirmation. In a later run, approve or reject only that hash through gj_watch(where:{id:{eq:"..."}}, update:{action_review_json:{decision:"approve|reject", expected_action_hash:"..."}}). Never create and approve an action in the same run.`

const adminReadInstruction = `Skill: admin_read. Investigate the visible control plane read-only. ` +
	`Use gj_security for posture, findings, effective policy, and config audits; use gj_runtime for health, source health, and recent activity; inspect gj_config only through visible governed reads. Discover exact shapes from system-capability or help rows and answer from that evidence.`

const adminWriteInstruction = `Skill: admin_write. Change the control plane through guarded gj_config paths only. ` +
	`First read visible security and runtime evidence. Discover the matching config_recipe and follow preflight, preview, apply, and verify while obeying forbidden_patterns and stop_conditions. Respect resolved mode defaults and do not add redundant agent, artifact, watch, or MCP toggles when dev or agentic mode already supplies them.`
