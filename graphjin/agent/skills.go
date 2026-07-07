package agent

import "strings"

// Skill names. Skills are split per domain × operation (read vs write). data_discovery is
// the base/default READ mode (no fragment; the always-on discovery loop lives in the base
// prompt).
const (
	skillDataDiscovery = "data_discovery" // data domain, read (= base/default)
	skillDataWrite     = "data_write"     // data domain, write (mutations)
	skillCodeRead      = "code_read"
	skillCodeWrite     = "code_write"
	skillWorkflowRead  = "workflow_read"
	skillWorkflowWrite = "workflow_write"
	skillAdminRead     = "admin_read"
	skillAdminWrite    = "admin_write"
	skillWatchRead     = "watch_read"
	skillWatchWrite    = "watch_write"
)

// Agent tool names (the small, fixed tool surface).
const (
	toolGraphQLHelp       = "graphql_help"
	toolQueryCatalog      = "query_catalog"
	toolValidateWhere     = "validate_where_clause"
	toolExecuteSavedQuery = "execute_saved_query"
	toolExecuteGraphQL    = "execute_graphql"
)

// Fixed gj_* system roots. Application/database roots are never named here; see the
// invariant on CapabilityProfile.
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

// skill is a focused, deterministic specialization of the discovery loop. It only shapes
// the prompt (instruction fragment) and narrows the tool set; it never widens visibility or
// relaxes a protocol guard. Required-evidence is expressed over the existing discoveryState
// so skills do not become a second source of authority. Access is always enforced by core.
type skill struct {
	name        string
	instruction string
	// allowTool reports whether a tool may remain in this skill's surface. The agent
	// intersects this with the mode/config-gated tool set, so a skill can only remove
	// tools, never add them.
	allowTool func(tool string) bool
	// requiredEvidence, when non-nil, must return true for an "answered" response to
	// stand; otherwise the protocol guard downgrades the answer to "blocked".
	requiredEvidence func(*discoveryState) bool
}

func allowAllTools(string) bool { return true }

func allowToolSet(tools ...string) func(string) bool {
	set := make(map[string]bool, len(tools))
	for _, t := range tools {
		set[t] = true
	}
	return func(tool string) bool { return set[tool] }
}

func securityRuntimeRequired(s *discoveryState) bool {
	return s != nil && s.securityRuntimeEvidence
}

// dataWriteEvidence backs the per-call mutation gate: an "answered" data_write
// response stands only when no executed mutation lacked target-shape evidence.
func dataWriteEvidence(s *discoveryState) bool {
	return s != nil && !s.hasUncoveredMutation
}

// codeWriteEvidence requires security/runtime evidence whenever a raw gj_code
// write actually executed (defense in depth behind the execute_graphql gate).
func codeWriteEvidence(s *discoveryState) bool {
	if s == nil {
		return false
	}
	return !s.codeWriteExecuted || s.securityRuntimeEvidence
}

// workflowWriteEvidence requires that any raw workflow execution followed a
// workflow (or saved-query) detail inspection in this run.
func workflowWriteEvidence(s *discoveryState) bool {
	if s == nil {
		return false
	}
	if !s.workflowExecuted {
		return true
	}
	return len(s.workflowsDetailed) != 0 || len(s.savedQueriesDetailed) != 0
}

// watchWriteEvidence requires security/runtime evidence whenever a raw gj_watch
// write actually executed (defense in depth behind the execute_graphql gate).
func watchWriteEvidence(s *discoveryState) bool {
	if s == nil {
		return false
	}
	return !s.watchWriteExecuted || s.securityRuntimeEvidence
}

// builtinSkills is the fixed registry.
//
// data_discovery is the DEFAULT/base READ mode, not a specialization: the always-on
// discovery loop (progressive gj_catalog discovery, GraphQL syntax, executing the right
// query) lives in the base agent prompt, so this entry adds no extra fragment. Every other
// entry is a thin router layered on top of the base only when the task/role/operation calls
// for it. All skills permit the full tool surface (allowAllTools); access is enforced by
// GraphJin core (roles + RLS), never by skills.
var builtinSkills = map[string]skill{
	skillDataDiscovery: {name: skillDataDiscovery, allowTool: allowAllTools},
	skillDataWrite:     {name: skillDataWrite, instruction: dataWriteInstruction, allowTool: allowAllTools, requiredEvidence: dataWriteEvidence},
	skillCodeRead:      {name: skillCodeRead, instruction: codeReadInstruction, allowTool: allowAllTools},
	skillCodeWrite:     {name: skillCodeWrite, instruction: codeWriteInstruction, allowTool: allowAllTools, requiredEvidence: codeWriteEvidence},
	skillWorkflowRead:  {name: skillWorkflowRead, instruction: workflowReadInstruction, allowTool: allowAllTools},
	skillWorkflowWrite: {name: skillWorkflowWrite, instruction: workflowWriteInstruction, allowTool: allowAllTools, requiredEvidence: workflowWriteEvidence},
	skillAdminRead:     {name: skillAdminRead, instruction: adminReadInstruction, allowTool: allowAllTools},
	skillAdminWrite:    {name: skillAdminWrite, instruction: adminWriteInstruction, allowTool: allowAllTools, requiredEvidence: securityRuntimeRequired},
	skillWatchRead:     {name: skillWatchRead, instruction: watchReadInstruction, allowTool: allowAllTools},
	skillWatchWrite:    {name: skillWatchWrite, instruction: watchWriteInstruction, allowTool: allowAllTools, requiredEvidence: watchWriteEvidence},
}

// selectSkill deterministically chooses a skill from the request instruction, the seed
// catalog evidence, the read-only flag, and the caller's capability profile. Selection is
// server-authoritative; it never reads model output. The DOMAIN is decided by catalog kind +
// the bounded gj_* system roots visible to the caller (never an app-root list); the OPERATION
// (read vs write) is decided by a deterministic write-intent verb scan of the instruction.
// Misclassification is guidance-only — core (roles + RLS) and the Go protocol guard enforce
// safety regardless of which fragment is chosen.
func selectSkill(instruction string, seed any, readOnly bool, profile *CapabilityProfile) skill {
	// read_only forces a read posture: never select a write skill.
	write := !readOnly && hasWriteIntent(instruction)

	kinds := seedKinds(seed)

	if profile != nil {
		adminVisible := profileHasSystemRoot(profile, systemRootConfig) ||
			profileHasSystemRoot(profile, systemRootSecurity) ||
			profileHasSystemRoot(profile, systemRootRuntime)
		if adminVisible && (kinds["config"] || kinds["config_recipe"] || kinds["system_capability"]) {
			if write {
				return builtinSkills[skillAdminWrite]
			}
			return builtinSkills[skillAdminRead]
		}

		watchVisible := profileHasSystemRoot(profile, systemRootWatch) ||
			profileHasSystemRoot(profile, systemRootWatchEvent)
		if watchVisible && hasWatchIntent(instruction) {
			if write {
				return builtinSkills[skillWatchWrite]
			}
			return builtinSkills[skillWatchRead]
		}

		workflowVisible := profileHasSystemRoot(profile, systemRootWorkflow) ||
			profileHasSystemRoot(profile, systemRootWorkflowExec)
		if workflowVisible && kinds["workflow"] {
			if write {
				return builtinSkills[skillWorkflowWrite]
			}
			return builtinSkills[skillWorkflowRead]
		}
	}

	// code is task-based, not role-gated: gj_code is a CodeSQL source queried like data
	// (not a gj_* system root), so select it when the seed surfaces a code source.
	if seedHasCodeSource(seed) {
		if write {
			return builtinSkills[skillCodeWrite]
		}
		return builtinSkills[skillCodeRead]
	}

	// data domain (default): read = base (data_discovery), write = data_write.
	if write {
		return builtinSkills[skillDataWrite]
	}
	return builtinSkills[skillDataDiscovery]
}

func profileHasSystemRoot(p *CapabilityProfile, root string) bool {
	if p == nil {
		return false
	}
	for _, r := range p.AvailableSystemRoots {
		if strings.EqualFold(strings.TrimSpace(r), root) {
			return true
		}
	}
	return false
}

// seedKinds collects the catalog kinds present in the seed discovery result.
func seedKinds(seed any) map[string]bool {
	kinds := map[string]bool{}
	for _, card := range catalogCards(seed) {
		if k := stringFromMap(card, "kind"); k != "" {
			kinds[strings.ToLower(k)] = true
		}
	}
	return kinds
}

// seedHasCodeSource reports whether the seed discovery surfaced a CodeSQL/code source
// (source cards carry source_kind "code"), signalling a code investigation/change task.
func seedHasCodeSource(seed any) bool {
	for _, card := range catalogCards(seed) {
		if strings.EqualFold(stringFromMap(card, "source_kind"), "code") {
			return true
		}
	}
	return false
}

// writeIntentStems are lowercase verb stems (no trailing 'e') matched as a prefix on
// instruction word tokens, so they catch inflections (create/creates/created/creating).
// The scan errs toward recall; a false positive only attaches write guidance to a read,
// which is harmless because core + the protocol guard enforce safety either way.
var writeIntentStems = []string{
	"add", "creat", "insert", "upsert", "updat", "modif", "delet", "remov", "drop",
	"renam", "appl", "sav", "grant", "revok", "enabl", "disabl", "patch", "migrat",
	"chang", "edit", "set", "writ", "fix", "provision", "deploy", "install",
	"reset", "approv", "reject", "publish", "rollback",
	"run", "execut", "trigger", "launch", // workflow execution is a write-like action
	"paus", "resum", "mark", // watch lifecycle + mark-seen inbox actions are writes
}

// hasWriteIntent reports whether the instruction expresses an imperative change.
func hasWriteIntent(instruction string) bool {
	for _, tok := range tokenizeWords(strings.ToLower(instruction)) {
		for _, stem := range writeIntentStems {
			if strings.HasPrefix(tok, stem) {
				return true
			}
		}
	}
	return false
}

// watchIntentStems route standing-question/inbox tasks to the watch skills when
// the watch roots are visible; matched as token prefixes like writeIntentStems.
var watchIntentStems = []string{"watch", "alert", "notif", "monitor", "subscri", "inbox"}

// hasWatchIntent reports whether the instruction is about watches or their inbox.
func hasWatchIntent(instruction string) bool {
	for _, tok := range tokenizeWords(strings.ToLower(instruction)) {
		for _, stem := range watchIntentStems {
			if strings.HasPrefix(tok, stem) {
				return true
			}
		}
	}
	return false
}

func tokenizeWords(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'))
	})
}

const dataWriteInstruction = `Skill: data_write. Apply application-data changes safely. ` +
	`Prefer an approved saved mutation (execute_saved_query); otherwise learn the insert/update/upsert/delete shape from mutation_pattern rows, validate the input, and author the mutation with execute_graphql. Core enforces role and RLS on every write. ` +
	`Before any mutation, establish this run's shape evidence for each target table — inspect its table detail row, validate_where_clause it, or inspect a mutation_pattern detail — unverified mutations are rejected. ` +
	`Preview/validate before writing and verify the result afterwards. If no permitted write path exists, return blocked with the catalog evidence and the missing capability.`

const codeReadInstruction = `Skill: code_read (gj_code / CodeSQL source). ` +
	`Investigate code by discovering gj_code rows — symbols, references, db_references (code-to-table links), and docs — using help:code and the code system_capability rows for exact shapes; trace provenance (symbol -> references -> db_reference -> schema). ` +
	`Answer from the discovered evidence. If a required code capability is not visible to this caller, return blocked with the evidence and the missing capability.`

const codeWriteInstruction = `Skill: code_write (gj_code / CodeSQL source). ` +
	`Change code through the code-source preview/apply workflow: learn the shape from mutation_pattern rows, preview the edit, then apply it through the gj_code managed mutation, checking gj_security before write-capable actions. ` +
	`If the code-change capability is not visible to this caller, return blocked with the evidence and the missing capability.`

const workflowReadInstruction = `Skill: workflow_read. Discover reusable workflows via query_catalog({kind:"workflow"}); ` +
	`inspect the chosen workflow by id — its variables, description, and safety notes — and summarize what it does and how to run it. This is discovery; do not execute.`

const workflowWriteInstruction = `Skill: workflow_write. Execute or author workflows through the governed surface. ` +
	`To run a workflow, inspect it by id (variables, safety) then execute it through the approved path (gj_workflow_execution). ` +
	`To author or update a workflow, preview the definition then save it through the gj_workflow capability. ` +
	`If running or saving requires a capability not visible to this caller, return blocked with the workflow evidence and the missing capability.`

const adminReadInstruction = `Skill: admin_read (admin). Investigate the control plane read-only. ` +
	`Read gj_security for posture, findings, effective policy, and config audits; read gj_runtime for health, source health, and recent events/activity. ` +
	`Discover exact query shapes from the gj_security / gj_runtime system_capability rows (or graphql_help({for:"security"}) / graphql_help({for:"runtime"})) and answer from that evidence. ` +
	`If a required control-plane root is not visible to this caller, return blocked with the evidence and the missing capability.`

const adminWriteInstruction = `Skill: admin_write (admin). Change the control plane through guarded paths only. ` +
	`First read gj_security / gj_runtime as evidence. Then discover the matching config_recipe and follow it — preflight, preview, apply, then verify — obeying its forbidden_patterns and stop_conditions, applying gj_config / gj_workflow changes only through that guarded path. ` +
	`If a required control-plane capability is not visible to this caller, return blocked with the evidence and the missing capability.`

const watchReadInstruction = `Skill: watch_read (gj_watch / gj_watch_event). Review standing watches and their fired-event inbox. ` +
	`Discover exact query shapes from the gj_watch / gj_watch_event system_capability rows (or help:watches); watches and events are owner-scoped — you only ever see the caller's rows. ` +
	`For the inbox loop, query gj_watch_event ordered by created_at desc, then mark reviewed events seen with gj_watch_event(update: { seen: true }) — the correct response to a watch_events_unseen notice. ` +
	`A watch's status, last_error, and failure_count explain why it is not firing. If gj_watch / gj_watch_event is not visible to this caller, return blocked with the evidence and the missing capability.`

const watchWriteInstruction = `Skill: watch_write (gj_watch). Create, update, pause, or delete standing watches through the governed root. ` +
	`Before any gj_watch mutation, inspect the help:security or help:runtime catalog row with query_catalog({id: "help:security"}) — the control-plane write gate — plus the gj_watch system_capability detail row. ` +
	`Create or update with gj_watch(insert/update) — name plus either a subscription query or a saved_query_name, optionally variables_json, condition_js, delivery_json, enrich_json; pause or resume via status/enabled; remove with gj_watch(delete). Watches run durably under the owner's stored identity; per-owner limits and event retention are enforced by config. ` +
	`Enabling watches is an admin config change (config_recipe), not a watch mutation. If the watch capability is not visible to this caller, return blocked with the evidence and the missing capability.`
