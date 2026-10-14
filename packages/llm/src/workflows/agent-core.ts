import type {
  AgentBackend,
  AgentEvent,
  AgentReasoningEffort,
  AgentRunResult,
  AgentWorkspace,
} from "../agent-backend";
import type { AllowedLibrary } from "../library/staging";
import type { AgentControlPlane } from "../work/control-plane";
import type { GraphjinDataPath } from "../work/graphjin-tool-policy";
import {
  buildGraphjinAgentServer,
  buildGraphjinMcpServer,
  buildLibraryServer,
  buildRecordsReadServer,
  buildWorkMemoryServer,
} from "../work/tools";
import { buildWorkflowActionServer } from "./action-tool-server";
import { buildWorkflowOutputServer } from "./output-tool-server";
import { formatScriptStepResults, runScriptSteps } from "./script-steps";
import type { WorkflowStep } from "./store";

export interface RunWorkflowAgentBackendInput {
  /** Hermes backend. In the sandbox it is reconstructed from config. */
  backend: AgentBackend;
  prompt: string;
  userMessage: string;
  orgId: string;
  threadId: string;
  /** The underlying work_run id that owns the sandbox and broker token. */
  runId: string;
  workflowRunId: string;
  mode: "live" | "headless";
  networkHosts: string[];
  triggeredByObservationId?: string | null;
  /** "agent" when the org opted in to GraphJin's server-side agent. */
  graphjinDataPath?: GraphjinDataPath;
  workspace: AgentWorkspace;
  /** Skill names the run's actor holds. Undefined means every skill. */
  allowedSkills?: readonly string[];
  /** Team library files the actor holds. Undefined means the whole team library. */
  allowedLibrary?: AllowedLibrary;
  /** In-process on the host; broker-backed inside the agent sandbox. */
  controlPlane: AgentControlPlane;
  emit: (event: AgentEvent) => Promise<void>;
  signal?: AbortSignal;
  timeoutMs?: number;
  tag?: string;
  /** Steps with a script run in order before the agent turn. */
  steps?: readonly WorkflowStep[];
  /** Trigger payload, exposed to script steps. */
  input?: Record<string, unknown>;
  reasoningEffort?: AgentReasoningEffort;
  maxToolIterations?: number;
  /** Keep the answer card. Defaults to true; the API returns its table. */
  wantsCards?: boolean;
  /** New turns allowed in the same run directory after a turn budget expires. */
  maxContinuations?: number;
}

/**
 * Sandbox-runnable workflow agent loop. The DB-bound prologue/epilogue stays in
 * runWorkflowTurn; this core only rebuilds workflow MCP tools and calls the
 * backend. Inside OpenShell, tools reach persistence through the broker-backed
 * control plane.
 */
export async function runWorkflowAgentBackend(
  input: RunWorkflowAgentBackendInput,
): Promise<AgentRunResult> {
  const {
    backend,
    prompt,
    userMessage,
    orgId,
    threadId,
    runId,
    workflowRunId,
    mode,
    triggeredByObservationId,
    networkHosts,
    graphjinDataPath = "direct",
    workspace,
    controlPlane,
    emit,
    signal,
    timeoutMs,
    tag,
    steps = [],
    reasoningEffort,
    maxToolIterations,
    maxContinuations = 0,
  } = input;

  const mcp = backend.capabilities.mcpTools;
  const mcpServers = mcp
    ? {
        ...(graphjinDataPath === "agent"
          ? { neko_graphjin_agent: buildGraphjinAgentServer({ orgId, runId, controlPlane }) }
          : { neko_graphjin: buildGraphjinMcpServer({ orgId, runId, controlPlane }) }),
        neko_workflow_output: buildWorkflowOutputServer({
          orgId,
          workflowRunId,
          workRunId: runId,
          emit,
          controlPlane,
        }),
        neko_action: buildWorkflowActionServer({
          orgId,
          workflowRunId,
          workRunId: runId,
          triggeredByObservationId,
          emit,
          controlPlane,
        }),
        neko_memory: buildWorkMemoryServer(
          {
            orgId,
            threadId,
            runId,
          },
          { controlPlane },
        ),
        // Search-only: workflows consult the document library the same
        // way chat turns do (layering resolved from the run binding).
        neko_library: buildLibraryServer(
          { orgId, threadId, runId },
          { controlPlane },
        ),
        neko_records: buildRecordsReadServer({ orgId, runId, controlPlane }),
      }
    : undefined;

  void mode;

  const stepResults = steps.some((step) => step.script)
    ? await runScriptSteps({
        steps,
        workspace,
        emit,
        ...(input.input ? { input: input.input } : {}),
        ...(input.allowedSkills ? { allowedSkills: input.allowedSkills } : {}),
        ...(signal ? { signal } : {}),
      })
    : [];
  const stepBlock = formatScriptStepResults(stepResults);
  const firstPrompt = stepBlock ? `${prompt}\n\n${stepBlock}` : prompt;

  // A turn that hits its budget is continued in the same run directory. Its
  // timeout error is held back unless no continuation follows.
  const trail = new ActionTrail();
  let heldTimeoutError: AgentEvent | null = null;
  const turnEmit = async (event: AgentEvent): Promise<void> => {
    trail.record(event);
    if (event.type === "error" && event.message.startsWith("hermes turn exceeded its ")) {
      heldTimeoutError = event;
      return;
    }
    await emit(event);
  };

  let turnPrompt = firstPrompt;
  for (let continuation = 0; ; continuation++) {
    heldTimeoutError = null;
    const result = await backend.run({
      prompt: turnPrompt,
      userMessage,
      orgId,
      workspace,
      onEvent: turnEmit,
      mcpServers,
      mcpBridgeEnv: mcp
        ? {
            OPENNEKO_MCP_MODE: "workflow",
            OPENNEKO_MCP_ORG_ID: orgId,
            OPENNEKO_MCP_THREAD_ID: threadId,
            OPENNEKO_MCP_RUN_ID: runId,
            OPENNEKO_MCP_SKILLS_ROOT: workspace.skillsRoot,
            OPENNEKO_MCP_WORKFLOW_RUN_ID: workflowRunId,
            ...(input.wantsCards !== false ? { OPENNEKO_MCP_CARD_SCHEMA: "rich" } : {}),
            ...(triggeredByObservationId
              ? { OPENNEKO_MCP_TRIGGERED_BY_OBSERVATION_ID: triggeredByObservationId }
              : {}),
          }
        : undefined,
      tag,
      signal,
      timeoutMs,
      ...(reasoningEffort ? { reasoningEffort } : {}),
      ...(maxToolIterations ? { maxToolIterations } : {}),
      wantsCards: input.wantsCards ?? true,
      networkHosts,
    });
    if (!result.timedOut || continuation >= maxContinuations || signal?.aborted) {
      if (heldTimeoutError) await emit(heldTimeoutError);
      if (result.timedOut && continuation > 0 && result.error) {
        return {
          ...result,
          error: `${result.error}; ${continuation} continuation(s) also ran out of time`,
        };
      }
      return result;
    }
    await emit({
      type: "status",
      message: `Turn budget reached; continuing the run (${continuation + 1}/${maxContinuations})`,
    });
    turnPrompt = `${firstPrompt}\n\n${continuationBlock(trail, continuation + 1)}`;
  }
}

function continuationBlock(trail: ActionTrail, number: number): string {
  return `<continuation number="${number}">
An earlier turn of this same run reached its time budget and stopped. Its
work in the run directory is intact. Read the files it wrote there, then
continue from where it stopped. Repeat only work whose output file is
missing or incomplete.

Last actions of the earlier turn:
${trail.summary() || "(none recorded)"}
</continuation>`;
}

class ActionTrail {
  private readonly actions: string[] = [];
  private text = "";

  record(event: AgentEvent): void {
    if (event.type === "tool_start") {
      const input = event.input === undefined ? "" : ` ${JSON.stringify(event.input)}`;
      this.actions.push(`- ${event.name}${input.slice(0, 300)}`);
      if (this.actions.length > 15) this.actions.shift();
    } else if (event.type === "message" && event.role === "assistant") {
      this.text = (this.text + event.content).slice(-1_000);
    }
  }

  summary(): string {
    const text = this.text.trim();
    return [...this.actions, ...(text ? [`Last assistant text: ${text}`] : [])].join("\n");
  }
}
