import { startupEvent, startupPhase } from "@neko/telemetry/startup";
import { randomUUID } from "node:crypto";
import { acquireAgentSlot } from "./agent-slots";
import { retryWhileDatabaseUnavailable } from "@neko/db";
import { touchAgentRun } from "./store";
import { spawn, type ChildProcess } from "node:child_process";
import { cp, mkdir, mkdtemp, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { createInterface } from "node:readline";
import { performance } from "node:perf_hooks";
import {
  agentTurnTimeoutMs,
  type AgentBackend,
  type AgentEvent,
  type AgentRunOptions,
  type AgentRunResult,
  type AgentWorkspace,
} from "../agent-backend";
import type { RunWorkflowAgentBackendInput } from "../workflows/agent-core";
import type { RunWorkflowTurnDeps } from "../workflows/run-workflow-turn";
import type { RunAgentBackendInput } from "./agent-core";
import type { AxBackend } from "../agent-backends/ax";
import type { RunBinding } from "./broker";
import type { RunChatTurnDeps } from "./run-chat-turn";
import { copySkillOverrides } from "./workspace";
import {
  captureSandboxSkillEdits,
  hashSkillTree,
  readPersonalSkillFiles,
  SANDBOX_SKILL_HASH_COMMAND,
  skillCaptureDecision,
  skillFileHash,
  type SkillFileHashes,
  type SkillToolCall,
} from "../config-vcs/skill-capture";
import { KNOWLEDGE_FILES, readKnowledgeSnapshot } from "../knowledge-cache";
import { SandboxFailure } from "./sandbox-failure";

// Wire protocol shared with the agent runner (apps/worker/src/agent-sandbox/
// entry.ts). They MUST stay identical to apps/worker/src/agent-sandbox/
// protocol.ts (a unit test asserts that). The launcher reads these markers
// from the runner's stdout.
const EVENT_MARKER = "__openneko_event__";
const RESULT_MARKER = "__openneko_agent_result__";
const ARTIFACTS_MARKER = "__openneko_artifacts__";

/**
 * The agent runner: the bundled entry.js the images ship, or entry.ts from a
 * source checkout (run with tsx).
 */
function agentEntry(): string {
  return process.env.OPENNEKO_AGENT_ENTRY || "/opt/openneko-agent/entry.js";
}
const SANDBOX_RUNTIME_DIR = ".openneko";

/** Where each run's staged workspace lives; shared with serve and the sandbox. */
function runsRoot(): string {
  return process.env.OPENNEKO_AGENT_RUNS_DIR || path.join(tmpdir(), "openneko-agent-runs");
}

// Next may evaluate this module in separate instrumentation and route bundles
// inside one process. Those bundles must share one boot ID, or one bundle's
// sweep deletes another's live run.
const sandboxBootHost = globalThis as typeof globalThis & { __opennekoSandboxBootId?: string };
const SANDBOX_BOOT_ID = sandboxBootHost.__opennekoSandboxBootId ??= randomUUID();

/** web or worker; each host sweeps only the runs it staged. */
function sandboxOwner(): string {
  return process.env.OPENNEKO_SANDBOX_OWNER ||
    (process.env.OPENNEKO_BROKER_PORT ? `openneko-broker-${process.env.OPENNEKO_BROKER_PORT}` : "openneko");
}

function describeError(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/**
 * Deletes this host's run folders from earlier boots. A run removes its own
 * folder when it ends, but a restart mid-run leaves it behind.
 */
export async function reapStrandedRuns(root = runsRoot(), owner = sandboxOwner(), boot = SANDBOX_BOOT_ID): Promise<string[]> {
  let names: string[];
  try {
    names = await readdir(root);
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") return [];
    throw error;
  }
  const prefix = `${safeSegment(owner)}.`;
  const stranded = names.filter((name) => name.startsWith(prefix) && !name.startsWith(`${prefix}${boot}.`));
  await Promise.all(stranded.map((name) => rm(path.join(root, name), { recursive: true, force: true })));
  return stranded;
}

function safeSegment(value: string): string {
  return value.replace(/[^A-Za-z0-9_-]/g, "_").slice(0, 64);
}

const reaperHost = globalThis as typeof globalThis & { __opennekoStrandedReaper?: ReturnType<typeof setInterval> };

export interface SandboxLauncherOptions {
  /**
   * Model keys the run's agent may use, by variable name; unset loads the
   * org's key for each run. They reach only the runner, which passes them to
   * openneko serve with each turn; the run's tools and terminal never see
   * them.
   */
  modelKeys?: Record<string, string>;
  /** Extra variables for the runner. */
  env?: Record<string, string>;
  /** Broker coordinates for the agent's MCP tools. */
  brokerUrl?: string;
  /** Mint a per-run bearer token bound to {runId, orgId, threadId} (the broker forces
   *  org/run from the binding, never the request body). */
  brokerTokenFor?: (binding: RunBinding) => string;
  /** Release the run's token after it finishes (called in the run's finally). */
  brokerRelease?: (runId: string) => void;
  execTimeoutMs?: number;
  onLog?: (line: string) => void;
}

/**
 * Per-org launch settings captured before a run. Model keys are not among
 * them: the launcher loads the org's key for each run, so none stays in
 * process state.
 */
export type AgentRuntimeLaunchConfig = Pick<SandboxLauncherOptions, "modelKeys">;

type RunCore = (input: RunAgentBackendInput) => Promise<AgentRunResult>;
type RunWorkflowCore = (input: RunWorkflowAgentBackendInput) => Promise<AgentRunResult>;
export type AgentJobAccess = {
  /** Permit query-only GraphJin reads through the trusted host broker. */
  graphjinRead?: boolean;
  /** Permit read-only delegation to GraphJin's built-in server agent. */
  graphjinAgent?: boolean;
  /** Permit search-only access to the org's team memory layer via the broker. */
  memorySearch?: boolean;
};

export type RunJobAgentBackendInput = {
  backend: AgentBackend;
  orgId: string;
  runId: string;
  workspace: AgentWorkspace;
  run: AgentRunOptions;
  access: AgentJobAccess;
  /** Skill names the job's actor holds. Undefined means every skill. */
  allowedSkills?: readonly string[];
  emit: (event: AgentEvent) => Promise<void>;
};

type RunJobCore = (input: RunJobAgentBackendInput) => Promise<AgentRunResult>;
type SandboxRunKind = "work" | "workflow" | "agent-job";
type SandboxRunInput =
  | RunAgentBackendInput
  | RunWorkflowAgentBackendInput
  | RunJobAgentBackendInput;

type SerializableAgentRunOptions = Pick<
  AgentRunOptions,
  | "userMessage"
  | "timeoutMs"
  | "retries"
  | "debug"
  | "tag"
  | "skills"
  | "threadState"
  | "nativeDelegation"
  | "wantsCards"
  | "reasoningEffort"
  | "maxToolIterations"
>;

function serializableAgentRunOptions(
  run: AgentRunOptions,
): SerializableAgentRunOptions {
  return {
    ...(run.userMessage !== undefined ? { userMessage: run.userMessage } : {}),
    ...(run.timeoutMs !== undefined ? { timeoutMs: run.timeoutMs } : {}),
    ...(run.retries !== undefined ? { retries: run.retries } : {}),
    ...(run.debug !== undefined ? { debug: run.debug } : {}),
    ...(run.tag !== undefined ? { tag: run.tag } : {}),
    ...(run.skills !== undefined ? { skills: run.skills } : {}),
    ...(run.threadState !== undefined ? { threadState: run.threadState } : {}),
    ...(run.nativeDelegation !== undefined
      ? { nativeDelegation: run.nativeDelegation }
      : {}),
    ...(run.wantsCards !== undefined ? { wantsCards: run.wantsCards } : {}),
    ...(run.reasoningEffort !== undefined
      ? { reasoningEffort: run.reasoningEffort }
      : {}),
    ...(run.maxToolIterations !== undefined
      ? { maxToolIterations: run.maxToolIterations }
      : {}),
  };
}

export type StagedSandboxWorkspace = {
  orgRoot: string;
  workspace: AgentWorkspace;
  skillOverrides: string[];
};

function workspacePathInStage(
  workspace: AgentWorkspace,
  stageOrgRoot: string,
  source: string,
): string {
  const relative = path.relative(workspace.orgRoot, source);
  if (
    relative === "" ||
    (!relative.startsWith("..") && !path.isAbsolute(relative))
  ) {
    return path.join(stageOrgRoot, relative);
  }
  throw new Error(`agent workspace path escapes org root: ${source}`);
}

async function copyDirectoryIfPresent(
  source: string,
  destination: string,
): Promise<void> {
  try {
    await rm(destination, { recursive: true, force: true });
    await cp(source, destination, { recursive: true, force: true });
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
  }
}

/**
 * Stage the least-privilege filesystem for one run. Durable memory is already
 * retrieved into the prompt; unrelated threads, prior runs, and unchanged
 * image-baked skills do not cross the sandbox boundary.
 */
export async function stageSandboxWorkspace(
  workspace: AgentWorkspace,
  stageDir: string,
  options: { requiredSkillNames?: readonly string[]; allowedSkills?: readonly string[] } = {},
): Promise<StagedSandboxWorkspace> {
  const stageOrgRoot = path.join(
    stageDir,
    "workspace",
    path.basename(workspace.orgRoot),
  );
  const stagedWorkspace = Object.fromEntries(
    Object.entries(workspace).map(([key, value]) => [
      key,
      workspacePathInStage(workspace, stageOrgRoot, value),
    ]),
  ) as AgentWorkspace;

  await mkdir(stageOrgRoot, { recursive: true });
  const knowledge = await readKnowledgeSnapshot(workspace.knowledgeRoot);
  await Promise.all([
    knowledge
      ? mkdir(stagedWorkspace.knowledgeRoot, { recursive: true }).then(() => Promise.all(
        KNOWLEDGE_FILES.map(file => writeFile(path.join(stagedWorkspace.knowledgeRoot, file), knowledge.files[file])),
      ))
      : copyDirectoryIfPresent(workspace.knowledgeRoot, stagedWorkspace.knowledgeRoot),
    copyDirectoryIfPresent(
      workspace.threadUploadsRoot,
      stagedWorkspace.threadUploadsRoot,
    ),
    copyDirectoryIfPresent(workspace.runRoot, stagedWorkspace.runRoot),
  ]);

  // Preserve the expected workspace shape even when a selected source is
  // empty, while deliberately leaving memory and sibling upload/run roots out.
  await Promise.all(
    Object.values(stagedWorkspace).map((directory) =>
      mkdir(directory, { recursive: true }),
    ),
  );
  const skillOverrides = await copySkillOverrides(
    workspace.skillsRoot,
    stagedWorkspace.skillsRoot,
    options.requiredSkillNames,
    options.allowedSkills,
  );

  return {
    orgRoot: stageOrgRoot,
    workspace: stagedWorkspace,
    skillOverrides,
  };
}

/**
 * Build the `runCore` the launcher injects into runChatTurn: run the
 * agent loop in its own runner process, with its terminal in the sandbox.
 * The host-side prologue (prompt build) and epilogue (fence handling +
 * persistence) stay in runChatTurn around this call. Shared by the worker
 * (channel runs) and the web route (interactive chat) — both are control-plane
 * hosts that launch the sandbox but never run the agent loop themselves.
 */
export function makeSandboxRunCore(opts: SandboxLauncherOptions): RunCore {
  return makeSandboxCore(opts, "work") as RunCore;
}

export function makeSandboxWorkflowRunCore(
  opts: SandboxLauncherOptions,
): RunWorkflowCore {
  return makeSandboxCore(opts, "workflow") as RunWorkflowCore;
}

/** Run one non-interactive backend turn in a runner process. */
export function makeSandboxJobRunCore(
  opts: SandboxLauncherOptions,
): RunJobCore {
  return makeSandboxCore(opts, "agent-job") as RunJobCore;
}

/**
 * Wrap a configured backend so every run happens in a runner process.
 * The wrapper accepts only serializable, non-interactive run options; tools
 * are derived from `access` inside the box instead of trusting caller-supplied
 * MCP servers or permission callbacks.
 */
export async function sandboxAgentBackendForJob(opts: {
  backend: AgentBackend;
  orgId: string;
  runId: string;
  workspace: AgentWorkspace;
  access?: AgentJobAccess;
}): Promise<AgentBackend> {
  const access = opts.access ?? {};
  const needsBroker =
    access.graphjinRead || access.graphjinAgent || access.memorySearch;
  const broker = needsBroker
    ? await import("./broker").then(({ ensureAgentBroker }) =>
        ensureAgentBroker(),
      )
    : undefined;
  if (needsBroker && !broker) {
    throw new Error("agent job capabilities require the OpenNeko broker");
  }
  // Resolve the org's persisted provider into an immutable launch snapshot.
  // Import lazily because host-provision owns the gateway sync and itself
  // imports this module.
  const launchConfig = await import("../host-provision").then(
    ({ ensureHostConfigProvisioned }) =>
      ensureHostConfigProvisioned(opts.orgId),
  );
  const runCore = makeSandboxJobRunCore(
    sandboxLauncherOptionsFromConfig(launchConfig, broker),
  );

  return {
    id: opts.backend.id,
    configuredIdentity: opts.backend.configuredIdentity,
    model: opts.backend.model,
    capabilities: opts.backend.capabilities,
    async run(run): Promise<AgentRunResult> {
      const workspace = run.workspace ?? opts.workspace;
      if (workspace.orgRoot !== opts.workspace.orgRoot) {
        throw new Error("agent job attempted to replace its isolated workspace");
      }
      if (run.mcpServers) {
        throw new Error(
          "agent job tools must be declared through the access envelope",
        );
      }
      if (run.signal) {
        throw new Error("agent job AbortSignal forwarding is not supported");
      }
      return runCore({
        backend: opts.backend,
        orgId: opts.orgId,
        runId: opts.runId,
        workspace,
        run,
        access,
        emit: async (event) => {
          await run.onEvent?.(event);
        },
      });
    },
  };
}

/**
 * Starts this host's sweep of run folders stranded by an earlier boot. Every
 * run stages and removes its own folder, so nothing is held between runs.
 */
export async function prepareSandboxCapacity(
  opts: Pick<SandboxLauncherOptions, "onLog"> = {},
): Promise<void> {
  if (reaperHost.__opennekoStrandedReaper) return;
  const log = opts.onLog ?? console.log;
  const reap = () => reapStrandedRuns()
    .then((names) => {
      if (names.length) log(`deleted ${names.length} agent run folders left by an earlier start`);
    })
    .catch((error) => log(`could not delete agent run folders left by an earlier start: ${describeError(error)}`));
  const timer = setInterval(() => { void reap(); }, 300_000);
  timer.unref();
  reaperHost.__opennekoStrandedReaper = timer;
  await reap();
}

/** Explicit shutdown hook for hosts/tests. */
export async function closeSandboxReapers(): Promise<void> {
  if (reaperHost.__opennekoStrandedReaper) clearInterval(reaperHost.__opennekoStrandedReaper);
  reaperHost.__opennekoStrandedReaper = undefined;
}

/** Makes the stage readable and writable by the group serve and the sandbox share. */
async function shareWithGroup(dir: string): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    const child = spawn("chmod", ["-R", "g+rwX,o-rwx", dir], { stdio: "ignore" });
    child.on("error", reject);
    child.on("close", (code) => (code === 0 ? resolve() : reject(new Error(`chmod exited ${code}`))));
  });
}

function makeSandboxCore(
  opts: SandboxLauncherOptions,
  kind: SandboxRunKind,
): (input: SandboxRunInput) => Promise<AgentRunResult> {
  const log = opts.onLog ?? ((l: string) => console.log(`[agent-run] ${l}`));

  const runInSandbox = async function (
    input: SandboxRunInput,
  ): Promise<AgentRunResult> {
    const isJob = kind === "agent-job";
    const jobInput = isJob ? (input as RunJobAgentBackendInput) : null;
    const signal = isJob
      ? undefined
      : (input as RunAgentBackendInput | RunWorkflowAgentBackendInput).signal;
    if (signal?.aborted) throw abortError();
    const started = performance.now();
    const timed = async <T>(
      phase: string,
      operation: () => Promise<T>,
      attributes: Record<string, unknown> = {},
    ): Promise<T> => {
      const start = performance.now();
      let ok = false;
      try {
        const value = await startupPhase(`sandbox.${phase}`, operation, attributes);
        ok = true;
        return value;
      } catch (error) {
        // exec errors other than the launcher's own are the agent's.
        if (phase === "exec" || error instanceof SandboxFailure || (error instanceof Error && error.name === "AbortError")) throw error;
        throw new SandboxFailure(`sandbox.${phase}`, error instanceof Error ? error.message : String(error), { cause: error });
      } finally {
        log(JSON.stringify({
          type: "sandbox_phase",
          runId: input.runId,
          phase,
          durationMs: Math.round(performance.now() - start),
          ok,
          ...attributes,
        }));
      }
    };
    const inputPrompt = jobInput?.run.prompt ??
      (input as RunAgentBackendInput | RunWorkflowAgentBackendInput).prompt;

    // The run sees a staged copy of the least it needs, under the runs root
    // that serve and the sandbox share; workspace paths and the prompt are
    // remapped from the host org root to the stage.
    const hostOrgRoot = input.workspace.orgRoot;
    const root = runsRoot();
    await mkdir(root, { recursive: true });
    const stageDir = await mkdtemp(path.join(root, `${safeSegment(sandboxOwner())}.${SANDBOX_BOOT_ID}.`));
    const stageOrgRoot = path.join(stageDir, "workspace", path.basename(hostOrgRoot));
    const toStage = (s: string): string => s.split(hostOrgRoot).join(stageOrgRoot);

    const threadId = isJob
      ? `agent-job:${input.runId}`
      : (input as RunAgentBackendInput | RunWorkflowAgentBackendInput).threadId;
    const message = isJob
      ? jobInput?.run.userMessage ?? ""
      : (input as RunAgentBackendInput | RunWorkflowAgentBackendInput).userMessage;

    // Must outlive the runner's turn budget with margin, so a long turn dies
    // as the backend's honest timeout error — not an opaque kill from out here.
    const execBudgetMs =
      opts.execTimeoutMs ??
      (kind === "workflow"
        ? workflowExecBudgetMs(input as RunWorkflowAgentBackendInput)
        : (jobInput?.run.timeoutMs ?? agentTurnTimeoutMs())) + 120_000;
    let child: ChildProcess | undefined;
    // Unknown (timeout, crash) must preserve partial artifacts.
    let artifactsPresent: boolean | undefined;
    let staged: StagedSandboxWorkspace | undefined;
    let skillActor: { userId: string | null; role: string | null } | null = null;
    let skillBaseline: SkillFileHashes | undefined;
    const skillToolCalls: SkillToolCall[] = [];
    let skillEvidenceComplete = false;
    let skillEvidenceUnreadable = false;
    try {
      await input.emit({
        type: "status",
        message: "Preparing secure agent workspace…",
      });
      staged = await timed("stage", () => stageSandboxWorkspace(input.workspace, stageDir, {
        ...(input.allowedSkills ? { allowedSkills: input.allowedSkills } : {}),
      }));
      const stagedWorkspace = staged.workspace;
      if (!isJob) {
        // Skill edits made in the run are saved when the turn ends; a member
        // runs with their own saved skill versions laid over the company's.
        const { getWorkRunActor } = await import("./personas");
        skillActor = await getWorkRunActor(input.runId, input.orgId).catch(() => null);
        const personal = skillActor?.userId && skillActor.role !== "admin"
          ? await readPersonalSkillFiles(hostOrgRoot, skillActor.userId).catch(() => [])
          : [];
        for (const file of personal) {
          const target = path.join(stagedWorkspace.skillsRoot, file.path);
          await mkdir(path.dirname(target), { recursive: true });
          await writeFile(target, file.content);
        }
        skillBaseline = await hashSkillTree(input.workspace.skillsRoot);
        for (const file of personal) skillBaseline.set(file.path, skillFileHash(file.content));
      }

      // When serve hosts GraphJin, the run asks its agent there as the run's
      // user; the host mints the token GraphJin will verify.
      const dataAgentPath =
        (kind === "work" && (input as RunAgentBackendInput).graphjinDataPath === "agent") ||
        (kind === "workflow" && (input as RunWorkflowAgentBackendInput).graphjinDataPath === "agent") ||
        (isJob && jobInput?.access.graphjinAgent === true);
      const hostedGraphjinToken =
        dataAgentPath && process.env.OPENNEKO_SERVE_GRAPHJIN === "1" && process.env.OPENNEKO_SERVE_SOCKET
          ? await import("./control-plane").then(({ hostedGraphjinToken }) =>
              hostedGraphjinToken(input.orgId, isJob ? null : input.runId, execBudgetMs / 1000 + 600))
          : null;

      const job = {
        kind,
        ...(hostedGraphjinToken ? { hostedGraphjin: { token: hostedGraphjinToken } } : {}),
        orgId: input.orgId,
        threadId,
        runId: input.runId,
        message,
        prompt: toStage(inputPrompt),
        backendId: input.backend.id,
        ax: (input.backend as AxBackend).ax,
        workspace: stagedWorkspace,
        ...(input.allowedSkills ? { allowedSkills: [...input.allowedSkills] } : {}),
        ...(kind === "work"
          ? {
              threadState: (input as RunAgentBackendInput).threadState,
              pluginActions: (input as RunAgentBackendInput).pluginActions,
              packActions: (input as RunAgentBackendInput).packActions,
              sourceConfigEnabled:
                (input as RunAgentBackendInput).sourceConfigEnabled ?? false,
              ...((input as RunAgentBackendInput).graphjinToolPolicy
                ? {
                    graphjinToolPolicy: (input as RunAgentBackendInput)
                      .graphjinToolPolicy,
                  }
                : {}),
              ...((input as RunAgentBackendInput).graphjinDataPath === "agent"
                ? { graphjinDataPath: "agent" }
                : {}),
              ...((input as RunAgentBackendInput).nativeDelegation
                ? {
                    nativeDelegation: (input as RunAgentBackendInput)
                      .nativeDelegation,
                  }
                : {}),
              // Channel render intent — gates the render tool (see
              // docs/PER_CHANNEL_RENDERING.md). Default true if absent.
              wantsCards: (input as RunAgentBackendInput).wantsCards ?? true,
              cardSchema: (input as RunAgentBackendInput).cardSchema ?? "core",
            }
          : kind === "workflow"
            ? {
              workflowRunId: (input as RunWorkflowAgentBackendInput).workflowRunId,
              mode: (input as RunWorkflowAgentBackendInput).mode,
              networkHosts: (input as RunWorkflowAgentBackendInput).networkHosts,
              ...((input as RunWorkflowAgentBackendInput).graphjinDataPath === "agent"
                ? { graphjinDataPath: "agent" }
                : {}),
              triggeredByObservationId:
                (input as RunWorkflowAgentBackendInput).triggeredByObservationId ?? null,
              steps: (input as RunWorkflowAgentBackendInput).steps ?? [],
              maxContinuations:
                (input as RunWorkflowAgentBackendInput).maxContinuations ?? 0,
              ...((input as RunWorkflowAgentBackendInput).wantsCards === false ? { wantsCards: false } : {}),
              agentRun: serializableAgentRunOptions({
                prompt: inputPrompt,
                timeoutMs: (input as RunWorkflowAgentBackendInput).timeoutMs,
                reasoningEffort: (input as RunWorkflowAgentBackendInput).reasoningEffort,
                maxToolIterations: (input as RunWorkflowAgentBackendInput).maxToolIterations,
              }),
              }
            : {
                agentAccess: jobInput?.access ?? {},
                agentRun: serializableAgentRunOptions(jobInput?.run ?? { prompt: inputPrompt }),
              }),
      };
      const runtimeRoot = path.join(stagedWorkspace.runRoot, SANDBOX_RUNTIME_DIR);
      await mkdir(runtimeRoot, { recursive: true });
      const jobFile = path.join(runtimeRoot, "job.json");
      await writeFile(jobFile, JSON.stringify(job), { mode: 0o600 });
      await shareWithGroup(stageDir);

      log(
        `agent run staged: ${stageDir} (backend=${input.backend.id}, kind=${kind}, ` +
          `skill_overrides=${staged.skillOverrides.length})`,
      );
      await input.emit({ type: "status", message: "Agent is working…" });
      const entry = agentEntry();
      const env: Record<string, string> = {
        ...passThroughEnv(),
        HOME: stageDir,
        OPENNEKO_RUN_JOB_FILE: jobFile,
        ...(opts.brokerUrl ? { OPENNEKO_BROKER_URL: opts.brokerUrl } : {}),
        ...(opts.brokerUrl && opts.brokerTokenFor
          ? {
              OPENNEKO_BROKER_TOKEN: opts.brokerTokenFor({
                runId: input.runId,
                orgId: input.orgId,
                threadId,
                kind,
              }),
            }
          : {}),
        ...(opts.modelKeys ??
          (await import("../host-provision").then(({ loadAgentModelKeys }) => loadAgentModelKeys(input.orgId)))),
        ...(opts.env ?? {}),
      };
      const result = await timed("exec", () => execAndStream(
        {
          command: process.execPath,
          args: entry.endsWith(".ts") ? ["--import", "tsx", entry] : [entry],
          cwd: path.dirname(entry),
          env,
          onSpawn: (spawned) => { child = spawned; },
        },
        (event) => {
          if (event.type === "tool_start") skillToolCalls.push({ name: event.name, input: event.input });
          return input.emit(event);
        },
        execBudgetMs,
        signal,
        (present) => { artifactsPresent = present; },
        () => { skillEvidenceUnreadable = true; },
      ));
      skillEvidenceComplete = result.status === "completed" && !skillEvidenceUnreadable;
      return result;
    } finally {
      opts.brokerRelease?.(input.runId);
      // The runner's process group is the run's process tree.
      if (child) killGroup(child);
      // Copy artifacts the agent wrote back to the host run dir, so the
      // file-serving endpoint finds them. Best-effort, and runs even when the
      // turn errored or timed out, so a partial artifact isn't lost.
      if (staged && !isJob && !signal?.aborted && artifactsPresent !== false) {
        await timed("download", async () => {
          await mkdir(input.workspace.artifactRoot, { recursive: true });
          await cp(staged!.workspace.artifactRoot, input.workspace.artifactRoot, { recursive: true, force: true });
        }).catch((e) => log(`artifact copy-back skipped: ${(e as Error).message}`));
      }
      const skillDecision = skillCaptureDecision(skillEvidenceComplete && staged
        ? { toolCalls: skillToolCalls, cwd: staged.orgRoot, skillsRoots: [staged.workspace.skillsRoot] }
        : null);
      if (staged && skillActor && skillBaseline && !skillDecision.capture) {
        startupEvent("sandbox.skill_capture", { outcome: "skipped", reason: skillDecision.reason });
        log(JSON.stringify({
          type: "sandbox_phase", runId: input.runId, phase: "skill_capture",
          durationMs: 0, ok: true, skipped: true, reason: skillDecision.reason,
        }));
      } else if (staged && skillActor && skillBaseline) {
        const stagedSkills = staged.workspace.skillsRoot;
        await timed("skill_capture", () => captureSandboxSkillEdits({
          listSandboxHashes: async () => JSON.stringify(Object.fromEntries(await hashSkillTree(stagedSkills))),
          downloadSkill: (skill, destination) =>
            cp(path.join(stagedSkills, skill), destination, { recursive: true, force: true }),
          orgRoot: hostOrgRoot,
          orgId: input.orgId,
          runId: input.runId,
          actor: skillActor!,
          baseline: skillBaseline!,
        }), { reason: skillDecision.reason })
          .then((saved) => {
            if (saved) log(JSON.stringify({ type: "skill_capture", runId: input.runId, ...saved }));
          })
          .catch((e) => log(`skill capture failed: ${(e as Error).message}`));
      }
      await rm(stageDir, { recursive: true, force: true });
      log(JSON.stringify({
        type: "sandbox_phase",
        runId: input.runId,
        phase: "total",
        durationMs: Math.round(performance.now() - started),
      }));
    }
  };

  return async function sandboxRunCore(
    input: SandboxRunInput,
  ): Promise<AgentRunResult> {
    const emit = (input as { emit?: (event: AgentEvent) => Promise<void> }).emit;
    const signal = (input as { signal?: AbortSignal }).signal;
    const release = await acquireAgentSlot({
      signal,
      onWait: () => emit?.({ type: "status", message: "Waiting for another question to finish…" }),
      heartbeat: () => touchAgentRun(input.runId),
    });
    try {
      return await runInSandbox(input);
    } finally {
      release();
    }
  };
}

/** The runner's environment from this host: the search path, proxies and CA bundle, and serve's socket. Nothing else. */
function passThroughEnv(): Record<string, string> {
  const names = [
    "PATH", "LANG", "TZ", "NODE_ENV", "NODE_EXTRA_CA_CERTS",
    "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy", "ALL_PROXY", "NODE_USE_ENV_PROXY",
    "OPENNEKO_SERVE_SOCKET", "OPENNEKO_MCP_BRIDGE", "OPENNEKO_AX_SKILL_RERANK", "OPENNEKO_AGENT_TURN_TIMEOUT_MS",
  ];
  return Object.fromEntries(
    names.flatMap((name) => (process.env[name] ? [[name, process.env[name]!]] : [])),
  );
}

function killGroup(child: ChildProcess): void {
  if (!child.pid || child.exitCode != null) return;
  try {
    process.kill(-child.pid, "SIGKILL");
  } catch {
    child.kill("SIGKILL");
  }
}

/** The first turn and every continuation, back to back. */
export function workflowExecBudgetMs(input: RunWorkflowAgentBackendInput): number {
  const turnMs = input.timeoutMs ?? agentTurnTimeoutMs();
  return turnMs * (1 + (input.maxContinuations ?? 0));
}

/**
 * Build the agent runtime deps for runChatTurn. Every production host injects
 * this runCore; tests inject their own in-process runCore via deps.
 *
 * `broker` wires the run's MCP-tool path: the caller starts a host broker
 * bound to its control plane and passes the handle so every backend can reach
 * policy-gated host capabilities and return their UI events mid-turn.
 */
type SandboxBrokerHandle = {
  url: string;
  tokenFor: (binding: RunBinding) => string;
  release?: (runId: string) => void;
};

export function agentRuntimeDepsFromEnv(
  broker?: SandboxBrokerHandle,
): Pick<Partial<RunChatTurnDeps>, "runCore"> {
  return {
    runCore: makeSandboxRunCore(sandboxLauncherOptionsFromEnv(broker)),
  };
}

export function agentRuntimeDepsFromConfig(
  config: AgentRuntimeLaunchConfig,
  broker?: SandboxBrokerHandle,
): Pick<Partial<RunChatTurnDeps>, "runCore"> {
  return {
    runCore: makeSandboxRunCore(sandboxLauncherOptionsFromConfig(config, broker)),
  };
}

export function workflowRuntimeDepsFromEnv(
  broker?: SandboxBrokerHandle,
): Pick<Partial<RunWorkflowTurnDeps>, "runCore"> {
  return {
    runCore: makeSandboxWorkflowRunCore(sandboxLauncherOptionsFromEnv(broker)),
  };
}

export function workflowRuntimeDepsFromConfig(
  config: AgentRuntimeLaunchConfig,
  broker?: SandboxBrokerHandle,
): Pick<Partial<RunWorkflowTurnDeps>, "runCore"> {
  return {
    runCore: makeSandboxWorkflowRunCore(
      sandboxLauncherOptionsFromConfig(config, broker),
    ),
  };
}

export function sandboxLauncherOptionsFromConfig(
  config: AgentRuntimeLaunchConfig,
  broker?: SandboxBrokerHandle,
): SandboxLauncherOptions {
  return {
    ...config,
    brokerUrl: broker?.url,
    brokerTokenFor: broker?.tokenFor,
    brokerRelease: broker?.release,
  };
}

/** Options for a host whose model key is in its environment (development). */
export function sandboxLauncherOptionsFromEnv(
  broker?: SandboxBrokerHandle,
): SandboxLauncherOptions {
  const keyEnv = process.env.OPENNEKO_AGENT_MODEL_KEY_ENV;
  const key = (keyEnv && process.env[keyEnv]) || process.env.MODEL_API_KEY;
  return sandboxLauncherOptionsFromConfig({
    ...(key ? { modelKeys: { MODEL_API_KEY: key, ...(keyEnv ? { [keyEnv]: key } : {}) } } : {}),
  }, broker);
}

/**
 * Run the agent runner and stream its tagged stdout: EVENT_MARKER lines → emit (host
 * scrubs + persists → web UI), the RESULT_MARKER line → the AgentRunResult.
 */
function execAndStream(
  runner: {
    command: string;
    args: string[];
    cwd: string;
    env: Record<string, string>;
    onSpawn: (child: ChildProcess) => void;
  },
  emit: (event: AgentEvent) => Promise<void>,
  timeoutMs: number,
  signal?: AbortSignal,
  onArtifacts?: (present: boolean) => void,
  onUnreadableEvent?: () => void,
): Promise<AgentRunResult> {
  return new Promise((resolve, reject) => {
    // Its own process group, so ending the run ends every child it started.
    const child = spawn(runner.command, runner.args, {
      cwd: runner.cwd,
      env: runner.env as NodeJS.ProcessEnv,
      detached: true,
      stdio: ["ignore", "pipe", "pipe"],
    });
    runner.onSpawn(child);
    let result: AgentRunResult | undefined;
    let stderr = "";
    let settled = false;
    let sawAnswerEvent = false;
    let eventError: Error | undefined;
    let eventQueue = Promise.resolve();
    let timer: ReturnType<typeof setTimeout>;
    const cleanup = () => {
      clearTimeout(timer);
      signal?.removeEventListener("abort", onAbort);
    };
    const fail = (err: Error) => {
      if (settled) return;
      settled = true;
      cleanup();
      killGroup(child);
      reject(err);
    };
    const onAbort = () => fail(abortError());
    const rl = createInterface({ input: child.stdout });
    rl.on("line", (line: string) => {
      if (line.startsWith(ARTIFACTS_MARKER)) {
        const value = line.slice(ARTIFACTS_MARKER.length);
        if (value === "true" || value === "false") onArtifacts?.(value === "true");
        return;
      }
      const ev = line.indexOf(EVENT_MARKER);
      if (ev >= 0) {
        try {
          const event = JSON.parse(
            line.slice(ev + EVENT_MARKER.length),
          ) as AgentEvent;
          sawAnswerEvent ||=
            (event.type === "message" &&
              event.role === "assistant" &&
              event.content.trim().length > 0) ||
            (event.type === "surface" && event.messages.length > 0) ||
            event.type === "action_request_emit" ||
            event.type === "needs_input" ||
            event.type === "output_emit";
          // The bundled runtime can exit immediately after its result marker.
          // Serialize and drain host-side handlers so a fast process cannot
          // overtake message persistence or reorder streamed deltas.
          eventQueue = eventQueue.then(async () => {
            try {
              // A short Postgres restart must not fail an answer that is
              // nearly done; hold the event until the database is back.
              await retryWhileDatabaseUnavailable(() => emit(event));
            } catch (error) {
              eventError ??=
                error instanceof Error ? error : new Error(String(error));
            }
          });
        } catch {
          onUnreadableEvent?.();
        }
        return;
      }
      const rs = line.indexOf(RESULT_MARKER);
      if (rs >= 0) {
        try {
          result = JSON.parse(line.slice(rs + RESULT_MARKER.length)) as AgentRunResult;
        } catch {
          /* ignore */
        }
      }
    });
    child.stderr.on("data", (d: Buffer) => (stderr += d.toString("utf8")));
    timer = setTimeout(() => {
      fail(new SandboxFailure("sandbox.exec_timeout", `agent run timed out after ${timeoutMs}ms`));
    }, timeoutMs);
    timer.unref();
    if (signal?.aborted) onAbort();
    else signal?.addEventListener("abort", onAbort, { once: true });
    child.on("error", (err) => {
      fail(err);
    });
    child.on("close", (code) => {
      if (settled) return;
      void (async () => {
        await eventQueue;
        if (settled) return;
        settled = true;
        cleanup();
        if (eventError) {
          reject(new Error(`agent event delivery failed: ${eventError.message}`));
          return;
        }
        if (result) {
          const hasResultOutput =
            (typeof result.finalText === "string" &&
              result.finalText.trim().length > 0) ||
            (typeof result.rawText === "string" &&
              result.rawText.trim().length > 0);
          if (
            result.status === "completed" &&
            !hasResultOutput &&
            !sawAnswerEvent
          ) {
            reject(
              new Error(
                "agent runtime contract violation: completed without assistant output or surface",
              ),
            );
            return;
          }
          resolve(result);
          return;
        }
        reject(
          new SandboxFailure(
            "sandbox.no_result",
            `agent run exited ${code} without a result line; stderr=${stderr.slice(-500)}`,
          ),
        );
      })();
    });
  });
}

function abortError(): Error {
  const err = new Error("aborted");
  err.name = "AbortError";
  return err;
}
