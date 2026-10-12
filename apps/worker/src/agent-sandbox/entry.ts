import { existsSync, readFileSync } from "node:fs";
import { rm, symlink } from "node:fs/promises";
import { join } from "node:path";
import {
  type AgentBackendId,
  type AgentEvent,
  type AgentModelIdentity,
  type AgentRunOptions,
  type AgentRunResult,
  type AgentWorkspace,
} from "@neko/llm";
import { makeAgentBackend, type AxBackendConfig } from "@neko/llm/agent-runtime";
import {
  buildGraphjinAgentServer,
  buildGraphjinMcpServer,
  buildWorkMemoryServer,
  getBuiltinSkillsRoot,
  materializeBuiltinSkills,
  runAgentBackend,
  runWorkflowAgentBackend,
  type RunAgentBackendInput,
  type RunWorkflowAgentBackendInput,
} from "@neko/llm/sandbox-runtime";
import { BrokerControlPlane } from "./broker-client";
import { ARTIFACTS_MARKER, EVENT_MARKER, RESULT_MARKER } from "./protocol";
import { configureAgentRuntime } from "./runtime-contract";
import { hasArtifacts } from "./artifacts";
import { installDataClient } from "./data-client";
import { startDataSocket, type DataSocket } from "./data-socket";

/**
 * Runs INSIDE the agent's OpenShell sandbox (Phase 3). The launcher (work-run)
 * does the DB-bound prologue on the host, uploads the job + workspace, and
 * exec's this. We reconstruct the backend (with a PLACEHOLDER key — the real
 * key is injected by the OpenShell egress proxy, never here), run the agent
 * loop, and STREAM events back as tagged stdout lines that the launcher relays
 * to the host (which scrubs + persists). The model key never enters the box.
 *
 * Events go over stdout because `openshell sandbox exec` streams stdout to the
 * host in real time. Hermes MCP tools use BrokerControlPlane only when the run
 * grants brokered control-plane capabilities.
 */
interface SandboxJob {
  kind?: "work" | "workflow" | "agent-job";
  orgId: string;
  threadId: string;
  runId: string;
  message: string;
  prompt: string;
  backendId: AgentBackendId;
  configuredIdentity?: AgentModelIdentity;
  ax?: AxBackendConfig;
  model?: string;
  backendState?: Record<string, unknown>;
  pluginActions?: RunAgentBackendInput["pluginActions"];
  packActions?: RunAgentBackendInput["packActions"];
  sourceConfigEnabled?: boolean;
  dataSurface?: RunAgentBackendInput["dataSurface"];
  graphjinToolPolicy?: RunAgentBackendInput["graphjinToolPolicy"];
  graphjinDataPath?: RunAgentBackendInput["graphjinDataPath"];
  nativeDelegation?: RunAgentBackendInput["nativeDelegation"];
  wantsCards?: boolean;
  cardSchema?: "rich" | "core";
  workflowRunId?: string;
  mode?: "live" | "headless";
  networkHosts?: string[];
  triggeredByObservationId?: string | null;
  steps?: RunWorkflowAgentBackendInput["steps"];
  input?: Record<string, unknown>;
  maxContinuations?: number;
  workspace: AgentWorkspace;
  /** Skill names the run's actor holds. Undefined means every skill. */
  allowedSkills?: string[];
  /** Explicit least-privilege envelope for non-interactive agent jobs. */
  agentAccess?: {
    graphjinRead?: boolean;
    graphjinAgent?: boolean;
    memorySearch?: boolean;
  };
  agentRun?: Pick<
    AgentRunOptions,
    | "userMessage"
    | "timeoutMs"
    | "retries"
    | "debug"
    | "tag"
    | "skills"
    | "backendState"
    | "nativeDelegation"
    | "wantsCards"
    | "reasoningEffort"
    | "maxToolIterations"
  >;
}

function requireEnv(name: string): string {
  const value = process.env[name];
  if (!value) throw new Error(`agent-sandbox: missing required env ${name}`);
  return value;
}

function emitLine(marker: string, obj: unknown): void {
  // One JSON object per line, newline-delimited, with a leading newline so a
  // marker never glues onto unflushed backend output on the same line.
  process.stdout.write(`\n${marker}${JSON.stringify(obj)}\n`);
}

function loadJob(): SandboxJob {
  // Large prompts are uploaded as a file to dodge env/ARG_MAX limits; small
  // jobs may come inline via OPENNEKO_RUN_JOB.
  const file = process.env.OPENNEKO_RUN_JOB_FILE;
  const raw = file ? readFileSync(file, "utf8") : requireEnv("OPENNEKO_RUN_JOB");
  return JSON.parse(raw) as SandboxJob;
}

export async function main(job = loadJob()): Promise<void> {
  // The launcher transfers only custom or modified skill directories. Fill in
  // unchanged built-ins from the agent image. Skill bodies remain filesystem
  // resources and are read only when the agent chooses one.
  await materializeBuiltinSkills(job.workspace.skillsRoot, job.allowedSkills);
  // Hermes scans HERMES_HOME/skills. Point it at the per-run catalog so a
  // required skill staged by the host is visible to Hermes' native skill tool.
  if (process.env.HERMES_HOME) {
    const hermesSkillsRoot = join(process.env.HERMES_HOME, "skills");
    await rm(hermesSkillsRoot, { recursive: true, force: true });
    await symlink(job.workspace.skillsRoot, hermesSkillsRoot, "dir");
  }
  const brokerUrl = process.env.OPENNEKO_BROKER_URL;
  const brokerToken = process.env.OPENNEKO_BROKER_TOKEN;

  const backend = makeAgentBackend({
    id: job.backendId,
    configuredIdentity: job.configuredIdentity,
    ...(job.ax ? { ax: job.ax } : {}),
  });

  // MCP tools reach the control plane through stdio bridge children that
  // Hermes ACP launches from this path.
  const controlPlane =
    brokerUrl && brokerToken
      ? new BrokerControlPlane(brokerUrl, brokerToken)
      : undefined;
  const emit = (event: AgentEvent): Promise<void> => {
    emitLine(EVENT_MARKER, event);
    return Promise.resolve();
  };

  const kind = job.kind ?? "work";
  if (kind !== "agent-job" && !controlPlane) {
    throw new Error("agent-sandbox: work run missing broker control plane");
  }
  const dataSocket = await startRunDataSocket(job, controlPlane);
  try {
    await runJob(job, kind, backend, controlPlane, emit);
  } finally {
    await dataSocket?.close();
  }
}

/**
 * Gives skill scripts the run's neko_graphjin tools, so a script fetches its
 * own data instead of passing every query through the model.
 */
async function startRunDataSocket(
  job: SandboxJob,
  controlPlane: BrokerControlPlane | undefined,
): Promise<DataSocket | undefined> {
  if (!controlPlane) return undefined;
  const kind = job.kind ?? "work";
  // Scripts query GraphJin directly, so they get no socket on the agent path.
  if (job.graphjinDataPath === "agent") return undefined;
  const enabled =
    kind === "workflow" ||
    (kind === "work" && job.dataSurface !== "records") ||
    (kind === "agent-job" && job.agentAccess?.graphjinRead === true);
  if (!enabled) return undefined;
  const socket = await startDataSocket({
    runId: job.runId,
    buildServer: () =>
      buildGraphjinMcpServer({
        orgId: job.orgId,
        ...(kind === "agent-job" ? {} : { runId: job.runId }),
        controlPlane,
        ...(kind === "work" && job.graphjinToolPolicy
          ? { toolPolicy: job.graphjinToolPolicy }
          : {}),
      }),
  });
  await installDataClient(job.workspace.binRoot, socket.path);
  process.env.OPENNEKO_DATA_SOCKET = socket.path;
  process.env.PYTHONPATH = [job.workspace.binRoot, process.env.PYTHONPATH]
    .filter(Boolean)
    .join(":");
  return socket;
}

async function runJob(
  job: SandboxJob,
  kind: NonNullable<SandboxJob["kind"]>,
  backend: ReturnType<typeof makeAgentBackend>,
  controlPlane: BrokerControlPlane | undefined,
  emit: (event: AgentEvent) => Promise<void>,
): Promise<void> {
  let result: AgentRunResult;
  if (kind === "agent-job") {
    const graphjinRead = job.agentAccess?.graphjinRead === true;
    const graphjinAgent = job.agentAccess?.graphjinAgent === true;
    const memorySearch = job.agentAccess?.memorySearch === true;
    if ((graphjinRead || graphjinAgent || memorySearch) && !controlPlane) {
      throw new Error("agent-sandbox: brokered job capability missing broker");
    }
    const mcpServers =
      graphjinRead || graphjinAgent || memorySearch
        ? {
            ...(graphjinRead
              ? {
                  neko_graphjin: buildGraphjinMcpServer({
                    orgId: job.orgId,
                    controlPlane,
                  }),
                }
              : {}),
            ...(graphjinAgent
              ? {
                  neko_graphjin_agent: buildGraphjinAgentServer({
                    orgId: job.orgId,
                    runId: job.runId,
                    controlPlane,
                  }),
                }
              : {}),
            ...(memorySearch
              ? {
                  neko_memory: buildWorkMemoryServer(
                    { orgId: job.orgId, runId: job.runId },
                    { exposeSave: false, controlPlane },
                  ),
                }
              : {}),
          }
        : undefined;
    result = await backend.run({
      ...(job.agentRun ?? {}),
      prompt: job.prompt,
      userMessage: job.agentRun?.userMessage ?? (job.message || undefined),
      orgId: job.orgId,
      workspace: job.workspace,
      onEvent: emit,
      mcpServers,
      wantsCards: false,
      mcpBridgeEnv:
        graphjinRead || graphjinAgent || memorySearch
          ? {
              OPENNEKO_MCP_ORG_ID: job.orgId,
              OPENNEKO_MCP_MODE: "agent-job",
              OPENNEKO_MCP_THREAD_ID: job.threadId,
              OPENNEKO_MCP_RUN_ID: job.runId,
              OPENNEKO_MCP_SKILLS_ROOT: job.workspace.skillsRoot,
              OPENNEKO_MCP_PLUGIN_ACTIONS: "[]",
              OPENNEKO_MCP_MEMORY_READ_ONLY: "1",
            }
          : undefined,
    });
  } else if (kind === "workflow") {
    if (!controlPlane) {
      throw new Error("agent-sandbox: workflow run missing broker control plane");
    }
    const workflowRunId = job.workflowRunId;
    if (!workflowRunId) {
      throw new Error("agent-sandbox: workflow job missing workflowRunId");
    }
    result = await runWorkflowAgentBackend({
      backend,
      prompt: job.prompt,
      userMessage: job.message,
      orgId: job.orgId,
      threadId: job.threadId,
      runId: job.runId,
      workflowRunId,
      mode: job.mode ?? "headless",
      networkHosts: job.networkHosts ?? [],
      ...(job.graphjinDataPath ? { graphjinDataPath: job.graphjinDataPath } : {}),
      triggeredByObservationId: job.triggeredByObservationId ?? null,
      workspace: job.workspace,
      controlPlane,
      emit,
      timeoutMs: job.agentRun?.timeoutMs,
      steps: job.steps ?? [],
      input: job.input ?? {},
      maxContinuations: job.maxContinuations ?? 0,
      ...(job.allowedSkills ? { allowedSkills: job.allowedSkills } : {}),
      ...(job.agentRun?.reasoningEffort
        ? { reasoningEffort: job.agentRun.reasoningEffort }
        : {}),
      ...(job.agentRun?.maxToolIterations
        ? { maxToolIterations: job.agentRun.maxToolIterations }
        : {}),
    });
  } else {
    if (!controlPlane) {
      throw new Error("agent-sandbox: work run missing broker control plane");
    }
    result = await runAgentBackend({
      backend,
      prompt: job.prompt,
      userMessage: job.message,
      orgId: job.orgId,
      threadId: job.threadId,
      runId: job.runId,
      workspace: job.workspace,
      backendState: job.backendState,
      pluginActions: job.pluginActions ?? [],
      packActions: job.packActions ?? [],
      sourceConfigEnabled: job.sourceConfigEnabled ?? false,
      dataSurface: job.dataSurface ?? "customer",
      ...(job.graphjinToolPolicy
        ? { graphjinToolPolicy: job.graphjinToolPolicy }
        : {}),
      ...(job.graphjinDataPath ? { graphjinDataPath: job.graphjinDataPath } : {}),
      ...(job.nativeDelegation
        ? { nativeDelegation: job.nativeDelegation }
        : {}),
      wantsCards: job.wantsCards ?? true,
      cardSchema: job.cardSchema ?? "core",
      controlPlane,
      emit,
    });
  }

  emitLine(RESULT_MARKER, result);
}

async function run(): Promise<void> {
  const runtime = configureAgentRuntime({ entryUrl: import.meta.url });
  const skillsRoot = getBuiltinSkillsRoot();
  if (!existsSync(skillsRoot)) {
    throw new Error(
      `agent runtime contract invalid: built-in skills not found at ${skillsRoot}`,
    );
  }

  if (process.argv.includes("--preflight")) {
    process.stdout.write(
      `${JSON.stringify({
        status: "ok",
        skillsRoot,
        mcpBridgePath: runtime.mcpBridgePath,
        lazyInstallsDisabled: process.env.HERMES_DISABLE_LAZY_INSTALLS === "1",
      })}\n`,
    );
    return;
  }
  const job = loadJob();
  try {
    await main(job);
  } finally {
    // Report filesystem evidence even after an agent error. A crash/timeout or
    // unreadable directory emits no hint, so the host still attempts recovery.
    const present = await hasArtifacts(job.workspace.artifactRoot).catch(() => undefined);
    if (present !== undefined) emitLine(ARTIFACTS_MARKER, present);
  }
}

run().catch((err: unknown) => {
  console.error(
    "[agent-sandbox] fatal:",
    err instanceof Error ? err.message : String(err),
  );
  process.exit(1);
});
