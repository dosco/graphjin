import {
  shellToolName,
  type AgentBackendId,
  type AgentWorkspace,
} from "../agent-backend";
import type { KnowledgePackContents } from "../knowledge-pack";
import {
  AX_DELEGATION,
  GRAPHJIN_DATE_RULE,
  VALUE_ESTIMATE_INSTRUCTIONS,
  buildDataAccessSection,
  buildMemorySection,
} from "../prompts/sections";
import type { WorkflowRecord } from "./store";
import { GRAPHJIN_AGENT_ASK_TOOL_TITLE, GRAPHJIN_EXECUTE_GRAPHQL_TOOL_TITLE } from "../graphjin/mcp-names";
import type { GraphjinDataPath } from "../work/graphjin-tool-policy";

export type BuildWorkflowRunnerPromptInput = {
  workflow: WorkflowRecord;
  mode: "live" | "headless";
  /** Efficacy evals disable time-saved metadata. Defaults to true. */
  includeUxMetadata?: boolean;
  memoryContext?: string;
  /** True when Hermes has brokered MCP tools for this run. */
  mcpTools: boolean;
  backend: AgentBackendId;
  workspace: AgentWorkspace;
  knowledge: KnowledgePackContents;
  /** "agent" when the org opted in to GraphJin's server-side agent. */
  graphjinDataPath?: GraphjinDataPath;
  /** Installed plugin action kinds, so the runner uses real kinds (e.g. send_slack_dm) not generic ones. */
  pluginActions?: readonly PluginActionPromptDescriptor[];
};

const HEADLESS_TAIL = `<mode>headless</mode>

<headless_guidance>
No operator is present during this run. Make the best decision you can
from the workflow's instructions and the data available. When you
genuinely cannot proceed without operator input, ask once and the run
will pause for the operator to resume manually. Take state-changing
actions only through an approved action request — see <actions> above.
</headless_guidance>`;

const LIVE_TAIL = `<mode>live</mode>

<live_guidance>
An operator is watching this run's event stream. Use AskUserQuestion
sparingly, for genuinely ambiguous choices or irreversible decisions.
</live_guidance>`;

const MCP_OUTPUTS_BLOCK = `<outputs>
Most workflow value is non-mutating. Emit outputs liberally via
\`mcp_neko_workflow_output_emit\` — reports, findings, observations,
recommendations, briefing card proposals. Tag each with \`scope\` and
\`mood\` (\`good\`, \`watch\`, or \`act\`) so other workflows and humans
can find them.

When this workflow is an observe-and-report kind (check a signal, flag
if it moves), \`kind: "observation"\` is the right shape: emit the
observation and end. Let the work stop where the steps say it should.
</outputs>`;

// Installed plugin action kinds, surfaced to the runner the same way the
// chat path surfaces them (see work/prompt.ts). Without this the runner
// agent invents a generic `send_message`, which no adapter handles and no
// kind-scoped policy rule matches — so the action silently stalls at
// pending_approval and never sends.
export type PluginActionPromptDescriptor = {
  kind: string;
  description: string;
  scope?: "external" | "internal";
  default_mode?:
    | "auto"
    | "ask"
    | "deny"
    | { external?: "auto" | "ask" | "deny"; internal?: "auto" | "ask" | "deny" };
  example?: Record<string, unknown>;
};

function isDeniedEverywhere(
  defaultMode: PluginActionPromptDescriptor["default_mode"],
): boolean {
  if (defaultMode === "deny") return true;
  if (defaultMode && typeof defaultMode === "object") {
    const keys = Object.keys(defaultMode) as Array<"external" | "internal">;
    return keys.length > 0 && keys.every((k) => defaultMode[k] === "deny");
  }
  return false;
}

function activeKinds(
  pluginActions: readonly PluginActionPromptDescriptor[],
): PluginActionPromptDescriptor[] {
  return pluginActions.filter((d) => !isDeniedEverywhere(d.default_mode));
}

function installedKindsBlock(
  pluginActions: readonly PluginActionPromptDescriptor[],
): string {
  const active = activeKinds(pluginActions);
  if (active.length === 0) return "";
  const rows = active
    .map((d) => {
      const head = `  - \`${d.kind}\` (scope: ${d.scope ?? "external"}) — ${d.description.split("\n")[0]}`;
      return d.example
        ? `${head}\n    example payload: ${JSON.stringify(d.example)}`
        : head;
    })
    .join("\n");
  return `
Installed action kinds — when a step calls for one of these, use the
EXACT \`kind\` value below; do NOT substitute a generic kind like
\`send_message\` (the policy rules and the executing adapter both match
on the exact kind, so a generic name silently fails to route).
Always use the exact scope listed for an installed kind:
${rows}
`;
}

function buildMcpActionsBlock(
  pluginActions: readonly PluginActionPromptDescriptor[],
): string {
  return `<actions>
Workflows decide; actions mutate. When a step needs to change real-world
or internal state, propose it through \`mcp_neko_action_request\` and
let policy decide whether it auto-executes, queues for operator
approval, or is denied.
${installedKindsBlock(pluginActions)}
For mutations with no installed kind, use a generic kind: \`mutate_record\`,
\`open_pr\`, \`run_command\` (external) or \`memory_write\`,
\`briefing_create\`, \`schedule_workflow\` (internal).

Fill in \`risk_level\` honestly (\`low\`, \`medium\`, \`high\`,
\`critical\`) — policy uses it to route — but never repeat that value
back to the operator in prose; it's noise from their point of view.
Use the one-sentence \`summary\` to name WHAT will change and WHY in
plain language; that's what the operator may read before approving.
When a request returns \`decision: denied\`, surface the reason to the
operator and stop; re-attempting after a denial is wasted effort.
</actions>`;
}

function buildNativeDelegationBlock(backend: AgentBackendId): string {
  if (backend === "ax") return AX_DELEGATION;
  if (backend !== "hermes") return "";
  return `<delegation>
Use Hermes native \`delegate_task\` when an independent workflow step would
benefit from fresh context or parallel investigation. You decide whether to
delegate, how many subtasks to create, and which \`toolsets\` each child gets.
OpenNeko does not provide named subagent profiles.

Every delegated child starts with no parent history, so include the exact
business question, source/table hints, file paths, constraints, and expected
return shape in \`goal\` and \`context\`. Use \`tasks\` for independent parallel
checks. Use \`role: "orchestrator"\` only for genuinely multi-stage work and
only when the configured delegation depth supports it.
</delegation>`;
}

export function buildWorkflowRunnerPrompt(
  input: BuildWorkflowRunnerPromptInput,
): string {
  const { workflow, mode, memoryContext, mcpTools, backend, workspace, knowledge } = input;
  if (!mcpTools) {
    throw new Error("Workflow runs require the native GraphJin broker tool");
  }
  const pluginActions = input.pluginActions ?? [];
  const shellTool = shellToolName(backend);
  const dataAccessSection = input.graphjinDataPath === "agent"
    ? buildDataAccessSection({ shellTool, agentTool: GRAPHJIN_AGENT_ASK_TOOL_TITLE, workspace, knowledge, inlineKnowledge: "syntax" })
    : buildDataAccessSection({
        shellTool,
        queryTool: GRAPHJIN_EXECUTE_GRAPHQL_TOOL_TITLE,
        queryIdentity: "actor",
        readOnly: true,
        scriptAccess: true,
        workspace,
        knowledge,
        inlineKnowledge: "syntax",
      });

  const stepsBlock = workflow.steps
    .map((step, index) =>
      step.script
        ? `  ${index + 1}. ${step.description} (script step: already ran; see <step_results>)`
        : `  ${index + 1}. ${step.description}`,
    )
    .join("\n");

  const overlay = workflow.systemPromptOverlay.trim();
  const overlayBlock = overlay
    ? `<author_instructions>
The workflow's author left these rules for every run. Respect them
unless they directly conflict with safety or policy.

${overlay}
</author_instructions>

`
    : "";

  const memorySection = buildMemorySection({
    searchTool: true,
    saveMode: "tool",
    memoryContext,
  });

  const goalBlock = workflow.goal.trim()
    ? `<goal>${workflow.goal.trim()}</goal>\n`
    : "";

  const outputsBlock = MCP_OUTPUTS_BLOCK;
  const actionsBlock = buildMcpActionsBlock(pluginActions);

  return `<role>
You are running a saved OpenNeko workflow as part of an operational loop.
Each run typically watches a signal, frames what it sees, decides what
should happen next, and emits one or more outputs. Many runs end with an
output and no further action — that's a complete shape.
</role>

<workflow>
<name>${workflow.name}</name>
<description>${workflow.description || "(none provided)"}</description>
${goalBlock}</workflow>

${overlayBlock}<steps>
Follow these in rough order. Ask the operator if anything is genuinely
ambiguous.

${stepsBlock || "  (no steps defined)"}
</steps>

<phases>
The workflow's value usually comes from one or more of these:

- Observe — gather the signals you need (sources of record, prior
  outputs, this run's inputs).
- Understand — frame what you see using memory and constraints.
- Decide — commit to a next step.
- Act — produce outputs, observations, or action requests as appropriate.
</phases>

${dataAccessSection}

<rules>
${GRAPHJIN_DATE_RULE}
</rules>

${outputsBlock}

<files>
Save every report or file you make under ${workspace.artifactRoot}. Files
saved there become downloads for the operator. Files saved anywhere else are
lost when the run ends.
</files>

${actionsBlock}

${memorySection}

${buildNativeDelegationBlock(backend)}

	<finishing>
After producing your output(s), send one short final assistant message
summarising what you did.${input.includeUxMetadata === false ? "" : ` Then emit one time-saved block.

${VALUE_ESTIMATE_INSTRUCTIONS}`}
</finishing>

${mode === "headless" ? HEADLESS_TAIL : LIVE_TAIL}`;
}

const TRIGGER_INPUT_MAX_CHARS = 16_000;

export function buildWorkflowSeedMessage(input: {
  workflow: Pick<WorkflowRecord, "name">;
  triggerKind: "manual" | "cron" | "subscription" | "watcher" | "api";
  userMessage?: string;
  triggerPayload?: Record<string, unknown>;
}): string {
  const { workflow, triggerKind, userMessage, triggerPayload } = input;
  if (userMessage?.trim()) return userMessage;
  const now = new Date().toISOString();
  const start =
    triggerKind === "cron"
      ? `[scheduled run started at ${now}] Begin executing the "${workflow.name}" workflow.`
      : triggerKind === "subscription"
        ? `[subscription-triggered run started at ${now}] Begin executing the "${workflow.name}" workflow.`
        : triggerKind === "api"
          ? `Begin executing the externally admitted "${workflow.name}" workflow with the supplied API input.`
          : `Begin executing the "${workflow.name}" workflow.`;
  if (!triggerPayload || Object.keys(triggerPayload).length === 0) return start;
  let json = JSON.stringify(triggerPayload, null, 2);
  if (json.length > TRIGGER_INPUT_MAX_CHARS) json = `${json.slice(0, TRIGGER_INPUT_MAX_CHARS)}\n... (truncated)`;
  return `${start}

This run's input is the event that triggered it. Do the work for this input.

<trigger_input>
${json}
</trigger_input>`;
}
