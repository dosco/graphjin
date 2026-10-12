import { startupEvent, startupPhase } from "@neko/telemetry/startup";
import { copyAllowedTeamLibrary, type AllowedLibrary } from "../library/staging";
import { createHash, randomUUID } from "node:crypto";
import { RECONCILE_COMMAND, syncSandboxDirectories } from "./sandbox-sync";
import { acquireStableSandboxInputs, clearStableSandboxInputs, type StableWorkspace } from "./sandbox-staging-cache";
import { SandboxPool, type WarmSlot } from "./sandbox-pool";
import { acquireAgentSlot } from "./agent-slots";
import { retryWhileDatabaseUnavailable } from "@neko/db";
import { touchAgentRun } from "./store";
import { spawn } from "node:child_process";
import { cp, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
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
  type AgentBackendId,
} from "../agent-backend";
import type { RunWorkflowAgentBackendInput } from "../workflows/agent-core";
import type { RunWorkflowTurnDeps } from "../workflows/run-workflow-turn";
import type { RunAgentBackendInput } from "./agent-core";
import { AX_HARNESS_BINARY, VENDORED_HERMES_MODEL_BINARY } from "../agent-runtime-contract";
import type { AxBackend } from "../agent-backends/ax";
import type { RunBinding } from "./broker";
import type { RunChatTurnDeps } from "./run-chat-turn";
import { copySkillOverrides } from "./workspace";
import {
  captureSandboxSkillEdits,
  hashSkillTree,
  readPersonalSkillFiles,
  SANDBOX_SKILL_HASH_COMMAND,
  skillFileHash,
  type SkillFileHashes,
} from "../config-vcs/skill-capture";
import { KNOWLEDGE_FILES, readKnowledgeSnapshot } from "../knowledge-cache";
import { SandboxFailure } from "./sandbox-failure";

// Wire protocol shared with the in-image entrypoint. The agent runs in a
// separate container, so these can't share a module at runtime — they MUST
// stay identical to apps/worker/src/agent-sandbox/protocol.ts (a unit test
// asserts that). The launcher greps the exec's stdout for these markers.
const EVENT_MARKER = "__openneko_event__";
const RESULT_MARKER = "__openneko_agent_result__";
const ARTIFACTS_MARKER = "__openneko_artifacts__";

/**
 * Standalone bundle inside the `agent` Docker stage, beside its bridge/assets.
 */
const AGENT_ENTRY = "/app/entry.js";
const SANDBOX_RUNTIME_DIR = ".openneko";

const SANDBOX_OWNER_LABEL = "openneko.owner";
const SANDBOX_BOOT_LABEL = "openneko.boot";
// Next may evaluate this module in separate instrumentation and route bundles
// inside one process. Those bundles share the warm pool via globalThis, so
// they must also share its boot label or one reaper deletes another's box.
const sandboxBootHost = globalThis as typeof globalThis & { __opennekoSandboxBootId?: string };
const SANDBOX_BOOT_ID = sandboxBootHost.__opennekoSandboxBootId ??= randomUUID();

/** web or worker; each host deletes only boxes it owns. */
function sandboxOwner(): string {
  return process.env.OPENNEKO_SANDBOX_OWNER ||
    (process.env.OPENNEKO_BROKER_PORT ? `openneko-broker-${process.env.OPENNEKO_BROKER_PORT}` : "openneko");
}

/** Labels that let the next boot of this host find boxes a restart stranded. */
export function sandboxOwnerLabelArgs(owner = sandboxOwner(), boot = SANDBOX_BOOT_ID): string[] {
  return ["--label", `${SANDBOX_OWNER_LABEL}=${owner}`, "--label", `${SANDBOX_BOOT_LABEL}=${boot}`];
}

/**
 * Deletes this host's sandboxes from earlier boots. The create command that
 * would delete a warm or job box dies with its host process, so without this
 * every restart leaves running boxes behind.
 */
export async function reapStrandedSandboxes(
  run: (args: string[], timeoutMs: number) => Promise<string>,
  owner = sandboxOwner(),
  boot = SANDBOX_BOOT_ID,
): Promise<string[]> {
  const listed: Array<{ name: string; labels?: Record<string, string> }> = [];
  let pageToken = "";
  do {
    const response = JSON.parse(await run([
      "sandbox", "list", "--selector", `${SANDBOX_OWNER_LABEL}=${owner}`,
      "-o", "json", "--page-size", "500",
      ...(pageToken ? ["--page-token", pageToken] : []),
    ], 30_000)) as {
      sandboxes: Array<{ name: string; labels?: Record<string, string> }>;
      next_page_token: string;
    };
    listed.push(...response.sandboxes);
    if (response.next_page_token && response.next_page_token === pageToken) {
      throw new Error("OpenShell repeated a sandbox list page token");
    }
    pageToken = response.next_page_token;
  } while (pageToken);
  const stranded = listed.filter((box) => box.labels?.[SANDBOX_BOOT_LABEL] !== boot).map((box) => box.name);
  const outcomes = await Promise.allSettled(stranded.map(async (name) => {
    try {
      await run(["sandbox", "delete", name], 60_000);
    } catch (error) {
      // Another process or the gateway may have removed it after list.
      if (!/\b(?:not found|does not exist)\b/i.test(describeError(error))) throw error;
    }
  }));
  const failed = outcomes.filter((outcome): outcome is PromiseRejectedResult => outcome.status === "rejected");
  if (failed.length) throw new AggregateError(failed.map((outcome) => outcome.reason), `could not delete ${failed.length} stranded sandboxes`);
  return stranded;
}

function describeError(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

const reaperHost = globalThis as typeof globalThis & {
  __opennekoStrandedReapers?: Map<string, { boot: string; timer: ReturnType<typeof setInterval> }>;
};
const strandedReapers = reaperHost.__opennekoStrandedReapers ??= new Map();

export interface SandboxLauncherOptions {
  /** `openshell` binary; default resolves from PATH. */
  cli?: string;
  /** Registered gateway name (mTLS) — preferred; else gatewayEndpoint; else CLI default. */
  gatewayName?: string;
  gatewayEndpoint?: string;
  /** Agent image (the Dockerfile `agent` stage), e.g. ghcr.io/open-neko/agent:<ver>. */
  agentImage: string;
  /** Per-sandbox limits, including Hermes and all tool children. */
  cpu?: string;
  memory?: string;
  /** OpenShell provider holding the model key — the proxy injects it; never in the box. */
  modelProvider?: string;
  /** Model endpoint egress; always scoped to the vendored Hermes executable. */
  modelHosts?: ReadonlyArray<{ host: string; port?: number }>;
  /** Extra env exported into the exec sh-wrapper (e.g. HERMES_HOME). Values must be safe. */
  env?: Record<string, string>;
  /**
   * Alias the OpenShell-injected credential env var (holds the
   * `openshell:resolve:env:…` placeholder) to the env var the backend reads —
   * e.g. {from:"MODEL_API_KEY", to:"GEMINI_API_KEY"} for hermes-gemini. The proxy
   * still substitutes the real key on egress, so the box only sees the
   * placeholder. `to` comes from the exhaustive Admin-provider runtime contract.
   */
  keyAliases?: ReadonlyArray<{ from: string; to: string }>;
  /**
   * Host HERMES_HOME (hermesHomeForOrg) to mirror into the box KEYLESS — only
   * config.yaml travels; the `.env` is emptied so the proxy-injected key (via
   * keyAliases, process env) is what hermes uses. Set for the hermes backend.
   */
  hermesHomeHostPath?: string;
  /** Broker coordinates for Hermes MCP tools. */
  brokerUrl?: string;
  /** Mint a per-run bearer token bound to {runId, orgId, threadId} (the broker forces
   *  org/run from the binding, never the request body). */
  brokerTokenFor?: (binding: RunBinding) => string;
  /** Release the run's token after it finishes (called in the run's finally). */
  brokerRelease?: (runId: string) => void;
  /** Process-local generic slots; defaults to one. Zero disables prewarming. */
  warmPoolSize?: number;
  warmIdleMs?: number;
  execTimeoutMs?: number;
  onLog?: (line: string) => void;
}

/**
 * Per-org model routing captured from the persisted provider row immediately
 * before a sandbox run. Keep this separate from process.env: the web and
 * worker hosts are long-lived, and Next.js may evaluate route bundles at
 * different times inside the same process.
 */
export type AgentRuntimeLaunchConfig = Pick<
  SandboxLauncherOptions,
  "modelProvider" | "modelHosts" | "keyAliases" | "hermesHomeHostPath"
>;

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
  /** Team library files the actor holds. Undefined means the whole team library. */
  allowedLibrary?: AllowedLibrary;
  emit: (event: AgentEvent) => Promise<void>;
};

type RunJobCore = (input: RunJobAgentBackendInput) => Promise<AgentRunResult>;
type SandboxRunKind = "work" | "workflow" | "agent-job";
type SandboxRunInput =
  | RunAgentBackendInput
  | RunWorkflowAgentBackendInput
  | RunJobAgentBackendInput;

const SHELL_KEY_RX = /^[A-Z_][A-Z0-9_]*$/;
// Shell variable names also support the profile's uppercase MODEL_API_KEY.
const SHELL_VARNAME_RX = /^[A-Za-z_][A-Za-z0-9_]*$/;

function shellQuote(value: string): string {
  return `'${value.replace(/'/g, `'\\''`)}'`;
}

type SerializableAgentRunOptions = Pick<
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
    ...(run.backendState !== undefined ? { backendState: run.backendState } : {}),
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

type SandboxEgressRule = { host: string; binary: string; port?: number; credentialProvider?: string };

export type OpenShellSandboxPolicy = {
  version: 1;
  filesystem_policy: {
    include_workdir: true;
    read_only: string[];
    read_write: string[];
  };
  landlock: { compatibility: "best_effort" };
  process: { run_as_user: "sandbox"; run_as_group: "sandbox" };
  network_policies: Record<
    string,
    {
      name: string;
      binaries: Array<{ path: string }>;
      endpoints: Array<{
        host: string;
        port: number;
        protocol: "rest";
        enforcement: "enforce";
        credential_binding?: { provider: string };
        rules: Array<{ allow: { method: "*"; path: "/**" } }>;
      }>;
    }
  >;
};

/**
 * Build the complete policy before sandbox creation. Grouping is by binary,
 * so model, broker, and GraphJin endpoints never gain one another's executable
 * allowlist through an endpoint/binary cross-product.
 */
export function buildSandboxPolicy(
  egress: ReadonlyArray<SandboxEgressRule>,
): OpenShellSandboxPolicy {
  const byBinary = new Map<
    string,
    Map<string, { host: string; port: number; credentialProvider?: string }>
  >();
  for (const rule of egress) {
    const port = rule.port ?? 443;
    const endpoints = byBinary.get(rule.binary) ?? new Map();
    const key = `${rule.host}\0${port}`;
    const previous = endpoints.get(key);
    endpoints.set(key, {
      host: rule.host,
      port,
      ...(rule.credentialProvider || previous?.credentialProvider
        ? { credentialProvider: rule.credentialProvider ?? previous!.credentialProvider }
        : {}),
    });
    byBinary.set(rule.binary, endpoints);
  }

  const networkPolicies: OpenShellSandboxPolicy["network_policies"] = {};
  let index = 0;
  for (const [binary, endpoints] of [...byBinary.entries()].sort(([a], [b]) =>
    a.localeCompare(b),
  )) {
    const label =
      path.posix.basename(binary).replace(/[^a-zA-Z0-9_]/g, "_") ||
      "binary";
    const name = `allow_${label}_${index++}`;
    networkPolicies[name] = {
      name,
      binaries: [{ path: binary }],
      endpoints: [...endpoints.values()]
        .sort((a, b) => a.host.localeCompare(b.host) || a.port - b.port)
        .map(({ host, port, credentialProvider }) => ({
          host,
          port,
          protocol: "rest",
          enforcement: "enforce",
          ...(credentialProvider ? { credential_binding: { provider: credentialProvider } } : {}),
          rules: [{ allow: { method: "*", path: "/**" } }],
        })),
    };
  }

  return {
    version: 1,
    filesystem_policy: {
      include_workdir: true,
      read_only: [
        "/usr",
        "/lib",
        "/proc",
        "/dev/urandom",
        "/app",
        "/etc",
        "/var/log",
      ],
      read_write: ["/sandbox", "/tmp", "/dev/null"],
    },
    landlock: { compatibility: "best_effort" },
    process: { run_as_user: "sandbox", run_as_group: "sandbox" },
    network_policies: networkPolicies,
  };
}

export type StagedSandboxWorkspace = {
  orgRoot: string;
  workspace: AgentWorkspace;
  skillOverrides: string[];
  stable?: Awaited<ReturnType<typeof acquireStableSandboxInputs>>;
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
  options: { requiredSkillNames?: readonly string[]; allowedSkills?: readonly string[]; allowedLibrary?: AllowedLibrary; cached?: boolean } = {},
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
  const stable = options.cached ? await acquireStableSandboxInputs(workspace, options.requiredSkillNames, options.allowedSkills, options.allowedLibrary) : undefined;
  try {
  const knowledge = stable ? null : await readKnowledgeSnapshot(workspace.knowledgeRoot);
  await Promise.all([
    stable ? Promise.resolve() : knowledge
      ? mkdir(stagedWorkspace.knowledgeRoot, { recursive: true }).then(() => Promise.all(
        KNOWLEDGE_FILES.map(file => writeFile(path.join(stagedWorkspace.knowledgeRoot, file), knowledge.files[file])),
      ))
      : copyDirectoryIfPresent(workspace.knowledgeRoot, stagedWorkspace.knowledgeRoot),
    copyDirectoryIfPresent(
      workspace.threadUploadsRoot,
      stagedWorkspace.threadUploadsRoot,
    ),
    copyDirectoryIfPresent(workspace.runRoot, stagedWorkspace.runRoot),
    // Team library OKF bundle (approved concepts only — the materializer
    // never writes personal-layer rows there, so staging it leaks nothing;
    // personal concepts reach the agent via mcp_neko_library_search,
    // scoped server-side to the run's owner).
    stable ? Promise.resolve() : options.allowedLibrary
      ? copyAllowedTeamLibrary(
          path.join(workspace.orgRoot, "library", "okf"),
          path.join(stageOrgRoot, "library", "okf"),
          options.allowedLibrary,
        )
      : copyDirectoryIfPresent(
          path.join(workspace.orgRoot, "library", "okf"),
          path.join(stageOrgRoot, "library", "okf"),
        ),
  ]);

  // Preserve the expected workspace shape even when a selected source is
  // empty, while deliberately leaving memory and sibling upload/run roots out.
  await Promise.all(
    Object.values(stagedWorkspace).map((directory) =>
      mkdir(directory, { recursive: true }),
    ),
  );
  const skillOverrides = stable?.skillOverrides ?? await copySkillOverrides(
    workspace.skillsRoot,
    stagedWorkspace.skillsRoot,
    options.requiredSkillNames,
    options.allowedSkills,
  );

  return {
    orgRoot: stageOrgRoot,
    workspace: stagedWorkspace,
    skillOverrides,
    ...(stable ? { stable } : {}),
  };
  } catch (error) { await stable?.release(); throw error; }
}

/**
 * Build the `runCore` the launcher injects into runChatTurn: run the
 * agent loop in an OpenShell sandbox.
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

/** Run one non-interactive backend turn inside OpenShell. */
export function makeSandboxJobRunCore(
  opts: SandboxLauncherOptions,
): RunJobCore {
  return makeSandboxCore(opts, "agent-job") as RunJobCore;
}

/**
 * Wrap a configured backend so every run happens in an OpenShell sandbox.
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
  // imports this module's OpenShell helpers.
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
          "agent job tools must be declared through the OpenShell access envelope",
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

// Next instrumentation and route bundles must see the same process-local pool.
const poolHost = globalThis as typeof globalThis & { __opennekoWarmPools?: Map<string, SandboxPool> };
const warmPools = poolHost.__opennekoWarmPools ??= new Map<string, SandboxPool>();

export async function prepareSandboxCapacity(
  opts = sandboxLauncherOptionsFromEnv(),
  workspace?: StableWorkspace,
  backendId?: AgentBackendId,
): Promise<void> {
  makeSandboxRunCore(opts);
  // Only Hermes uses warm slots; an Ax org keeps no idle sandboxes.
  if (backendId === "ax") return;
  const pool = getSandboxPool(opts, workspace);
  await startupPhase("sandbox.prewarm", async () => {
    await pool?.ready();
  });
}

/** Explicit shutdown hook for hosts/tests; sandbox-side idle expiry also survives host loss. */
export async function closeSandboxPools(): Promise<void> {
  for (const { timer } of strandedReapers.values()) clearInterval(timer);
  strandedReapers.clear();
  await Promise.all([...warmPools.values()].map(pool => pool.close()));
  warmPools.clear();
  await clearStableSandboxInputs();
}

function getSandboxPool(opts: SandboxLauncherOptions, workspace?: StableWorkspace): SandboxPool | undefined {
  const warmSize = opts.warmPoolSize ?? 1;
  if (!warmSize) return undefined;
  const cli = opts.cli ?? "openshell";
  const gatewayArgs = opts.gatewayName ? ["--gateway", opts.gatewayName] : opts.gatewayEndpoint ? ["--gateway-endpoint", opts.gatewayEndpoint] : [];
  const cpu = opts.cpu ?? "2", memory = opts.memory ?? "1Gi", idleMs = opts.warmIdleMs ?? 180_000;
  const runCleanup = (args: string[], timeout: number) => runProcessOnce(cli, [...gatewayArgs, ...args], timeout);
  // Preloaded org knowledge never enters another organization's spare queue.
  const poolKey = JSON.stringify([cli, gatewayArgs, opts.agentImage, cpu, memory, warmSize, idleMs, workspace?.orgRoot]);
  let pool = warmPools.get(poolKey);
  const gatewayKey = JSON.stringify([cli, gatewayArgs]);
  if (strandedReapers.get(gatewayKey)?.boot !== SANDBOX_BOOT_ID) {
    const previous = strandedReapers.get(gatewayKey);
    if (previous) clearInterval(previous.timer);
    let running = false;
    const reap = async () => {
      if (running) return;
      running = true;
      try {
        const names = await reapStrandedSandboxes(runCleanup);
        if (names.length) (opts.onLog ?? console.log)(`deleted ${names.length} sandboxes left by an earlier start`);
      } catch (error) {
        (opts.onLog ?? console.error)(`could not delete sandboxes left by an earlier start: ${describeError(error)}`);
      } finally {
        running = false;
      }
    };
    const timer = setInterval(() => { void reap(); }, 300_000);
    timer.unref();
    strandedReapers.set(gatewayKey, { boot: SANDBOX_BOOT_ID, timer });
    void reap();
  }
  if (!pool) {
    pool = new SandboxPool({ size: warmSize, idleMs,
      onEvent: attributes => startupEvent("sandbox.pool", { poolId: createHash("sha256").update(poolKey).digest("hex").slice(0, 16), ...attributes }),
      create: () => createWarmSandbox({ cli, gatewayArgs, image: opts.agentImage, cpu, memory, idleMs, runCleanup, workspace }),
      onError: (error) => (opts.onLog ?? console.error)(`warm sandbox preparation failed: ${describeError(error)}`),
    });
    warmPools.set(poolKey, pool);
    pool.replenish();
  }
  return pool;
}

function makeSandboxCore(
  opts: SandboxLauncherOptions,
  kind: SandboxRunKind,
): (input: SandboxRunInput) => Promise<AgentRunResult> {
  const cli = opts.cli ?? "openshell";
  const log = opts.onLog ?? ((l: string) => console.log(`[agent-sandbox] ${l}`));
  const cpu = opts.cpu ?? "2";
  const memory = opts.memory ?? "1Gi";
  if (!/^(?:[1-9]\d*|\d+\.\d+|[1-9]\d*m)$/.test(cpu) || parseFloat(cpu) <= 0) {
    throw new Error("Invalid agent sandbox CPU limit");
  }
  if (!/^[1-9]\d*(?:Ki|Mi|Gi|Ti|K|M|G|T)?$/.test(memory)) {
    throw new Error("Invalid agent sandbox memory limit");
  }

  const gatewayArgs = opts.gatewayName
    ? ["--gateway", opts.gatewayName]
    : opts.gatewayEndpoint
      ? ["--gateway-endpoint", opts.gatewayEndpoint]
      : [];

  const runCleanup = (args: string[], timeoutMs: number): Promise<string> =>
    runProcessOnce(cli, [...gatewayArgs, ...args], timeoutMs);

  const warmSize = opts.warmPoolSize ?? 1;
  const idleMs = opts.warmIdleMs ?? 180_000;
  if (!Number.isInteger(warmSize) || warmSize < 0 || warmSize > 10 ||
      !Number.isInteger(idleMs) || idleMs < 1_000 || idleMs > 3_600_000) {
    throw new Error("Invalid warm sandbox limits");
  }

  const runInSandbox = async function (
    input: SandboxRunInput,
  ): Promise<AgentRunResult> {
    // Warm slots preload Hermes. Ax starts in a fresh sandbox: binding a warm
    // slot costs it more than a create, and the idle fork server would end the slot mid-run.
    const hermes = input.backend.id === "hermes";
    let pool = kind === "work" && hermes ? getSandboxPool(opts, input.workspace) : undefined;
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
    ): Promise<T> => {
      const start = performance.now();
      let ok = false;
      try {
        const value = await startupPhase(`sandbox.${phase}`, operation);
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
        }));
      }
    };
    const run = (args: string[], timeoutMs: number, stdin?: string): Promise<string> =>
      runProcessOnce(cli, [...gatewayArgs, ...args], timeoutMs, signal, stdin);
    const inputPrompt = jobInput?.run.prompt ??
      (input as RunAgentBackendInput | RunWorkflowAgentBackendInput).prompt;
    // OpenShell 0.1.x limits sandbox names to 19 characters. Hash the full
    // run ID so distinct IDs that share a prefix cannot collide.
    let name = `neko-${isJob ? "j" : "w"}-${createHash("sha256").update(input.runId).digest("hex").slice(0, 12)}`;

    // The box is a separate filesystem; the host workspace path (~/.config/… or
    // /Users/…) can't be recreated under the sandbox user's home. Upload the
    // workspace under /sandbox and remap every workspace path + the prompt from
    // the host orgRoot prefix to the box prefix (all roots are under orgRoot).
    const hostOrgRoot = input.workspace.orgRoot;
    const boxOrgRoot = path.posix.join("/sandbox", path.basename(hostOrgRoot));
    const toBox = (s: string): string => s.split(hostOrgRoot).join(boxOrgRoot);
    const boxWorkspace = Object.fromEntries(
      Object.entries(input.workspace).map(([k, v]) => [
        k,
        typeof v === "string" ? toBox(v) : v,
      ]),
    ) as RunAgentBackendInput["workspace"];

    // Every sandboxed path uses brokered GraphQL. The source URL and actor
    // credential stay on the trusted host, and no source egress is granted to
    // any general-purpose sandbox binary.
    const recordsScoped =
      !isJob &&
      kind === "work" &&
      (input as RunAgentBackendInput).dataSurface === "records";

    const threadId = isJob
      ? `agent-job:${input.runId}`
      : (input as RunAgentBackendInput | RunWorkflowAgentBackendInput).threadId;
    const message = isJob
      ? jobInput?.run.userMessage ?? ""
      : (input as RunAgentBackendInput | RunWorkflowAgentBackendInput).userMessage;

    const job = {
      kind,
      orgId: input.orgId,
      threadId,
      runId: input.runId,
      message,
      prompt: toBox(inputPrompt),
      backendId: input.backend.id,
      configuredIdentity: input.backend.configuredIdentity,
      // Hermes reads its model from the staged config.yaml; Ax gets its route here.
      ...(input.backend.id === "ax" ? { ax: (input.backend as AxBackend).ax } : {}),
      workspace: boxWorkspace,
      ...(input.allowedSkills ? { allowedSkills: [...input.allowedSkills] } : {}),
      ...(kind === "work"
        ? {
            backendState: (input as RunAgentBackendInput).backendState,
            pluginActions: (input as RunAgentBackendInput).pluginActions,
            packActions: (input as RunAgentBackendInput).packActions,
            sourceConfigEnabled:
              (input as RunAgentBackendInput).sourceConfigEnabled ?? false,
            botEnabled: (input as RunAgentBackendInput).botEnabled ?? false,
            dataSurface:
              (input as RunAgentBackendInput).dataSurface ?? "customer",
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
            // Channel render intent — gates the in-box render tool (see
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
            input: (input as RunWorkflowAgentBackendInput).input ?? {},
            maxContinuations:
              (input as RunWorkflowAgentBackendInput).maxContinuations ?? 0,
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

    // MCP tools reach the control plane via the broker — node's
    // fetch routes through the egress proxy. Keep this endpoint scoped to the
    // node binary in the creation-time policy.
    const brokerEgress: SandboxEgressRule[] = opts.brokerUrl
      ? (() => {
          const u = new URL(opts.brokerUrl);
          return [
            {
              host: u.hostname,
              port: Number(u.port) || 80,
              binary: "/usr/local/bin/node",
            },
          ];
        })()
      : [];
    // OpenShell binds model egress to the process that calls the model.
    const modelBinary = hermes ? VENDORED_HERMES_MODEL_BINARY : AX_HARNESS_BINARY;
    const egressRules: SandboxEgressRule[] = [
      ...(opts.modelHosts ?? []).map((endpoint, index) => ({
        ...endpoint,
        binary: modelBinary,
        ...(index === 0 && opts.modelProvider
          ? { credentialProvider: opts.modelProvider }
          : {}),
      })),
      ...(kind === "workflow"
        ? ((input as RunWorkflowAgentBackendInput).networkHosts ?? []).map((host) => ({
            host,
            binary: modelBinary,
          }))
        : []),
      ...brokerEgress,
    ];

    const stageDir = await mkdtemp(path.join(tmpdir(), "oss-agent-"));
    let sandboxCreated = false;
    let lease: Awaited<ReturnType<SandboxPool["acquire"]>> | undefined;
    let warmSlot: WarmSlot | undefined;
    let healthy = false;
    // Unknown (old image, timeout, crash) must preserve partial artifacts.
    let artifactsPresent: boolean | undefined;
    let stableInputs: StagedSandboxWorkspace["stable"];
    let skillActor: { userId: string | null; role: string | null } | null = null;
    let skillBaseline: SkillFileHashes | undefined;
    try {
      await input.emit({
        type: "status",
        message: "Preparing secure agent workspace…",
      });
      const staged = await timed("stage", () => stageSandboxWorkspace(input.workspace, stageDir, {
        // A records-scoped turn must remain functional during a rolling
        // upgrade even if the sandbox image predates the records skill.
        requiredSkillNames: recordsScoped ? ["records"] : [],
        ...(input.allowedSkills ? { allowedSkills: input.allowedSkills } : {}),
        ...(input.allowedLibrary ? { allowedLibrary: input.allowedLibrary } : {}),
        cached: Boolean(pool),
      }));
      stableInputs = staged.stable;
      if (!isJob) {
        // Skill edits made in the box are saved when the turn ends; a member
        // runs with their own saved skill versions laid over the company's.
        const { getWorkRunActor } = await import("./personas");
        skillActor = await getWorkRunActor(input.runId, input.orgId).catch(() => null);
        const personal = skillActor?.userId && skillActor.role !== "admin"
          ? await readPersonalSkillFiles(hostOrgRoot, skillActor.userId).catch(() => [])
          : [];
        for (const file of personal) {
          const target = path.join(staged.workspace.skillsRoot, file.path);
          await mkdir(path.dirname(target), { recursive: true });
          await writeFile(target, file.content);
        }
        skillBaseline = await hashSkillTree(input.workspace.skillsRoot);
        for (const file of personal) skillBaseline.set(file.path, skillFileHash(file.content));
      }
      if (stableInputs) startupEvent("sandbox.staging_cache", { outcome: stableInputs.hit ? "hit" : "miss" });
      const stageRuntimeRoot = path.join(
        staged.workspace.runRoot,
        SANDBOX_RUNTIME_DIR,
      );
      await mkdir(stageRuntimeRoot, { recursive: true });
      const jobFile = path.join(stageRuntimeRoot, "job.json");
      await writeFile(jobFile, JSON.stringify(job));
      const hermesStage = opts.hermesHomeHostPath && hermes
        ? await stageKeylessHermesHome(
            opts.hermesHomeHostPath,
            pool ? path.join(stageDir, "hermes-home") : path.join(stageRuntimeRoot, "hermes-home"),
          )
        : null;
      const policyFile = path.join(stageDir, "policy.json");
      await writeFile(
        policyFile,
        JSON.stringify(buildSandboxPolicy(egressRules)),
      );
      const sandboxJobPath = path.posix.join(
        boxWorkspace.runRoot,
        SANDBOX_RUNTIME_DIR,
        "job.json",
      );
      const coldHermesHome = path.posix.join(
        boxWorkspace.runRoot,
        SANDBOX_RUNTIME_DIR,
        "hermes-home",
      );
      let sandboxHermesHome = pool ? "/sandbox/.hermes-warm/home" : coldHermesHome;

      await input.emit({
        type: "status",
        message: "Starting secure agent sandbox…",
      });
      if (pool) {
        const workInput = input as RunAgentBackendInput;
        const reuse = workInput.sandboxUser;
        // The host supplies an opaque CURRENT authorization revision. Never
        // derive it from a prompt, agent backendState, or a browser claim.
        const session = reuse?.principalId && reuse.authorizationRevision ? {
          key: JSON.stringify([input.orgId, reuse.principalId]),
          modelScope: createHash("sha256").update(JSON.stringify([opts.modelProvider, input.backend.id, input.backend.configuredIdentity])).digest("hex"),
          authorizationScope: createHash("sha256").update(reuse.authorizationRevision).digest("hex"),
          scope: createHash("sha256").update(JSON.stringify([
            reuse.authorizationRevision, opts.modelProvider, opts.modelHosts,
            opts.keyAliases, opts.env, input.backend.id, input.backend.configuredIdentity,
            workInput.pluginActions, workInput.packActions, workInput.sourceConfigEnabled, workInput.botEnabled ?? false, workInput.allowedSkills ?? null, workInput.allowedLibrary ?? null,
            workInput.dataSurface, workInput.graphjinToolPolicy, workInput.graphjinDataPath, workInput.nativeDelegation,
            workInput.backendState, opts.brokerUrl,
            hermesStage ? await readFile(path.join(hermesStage, "config.yaml"), "utf8") : null,
          ])).digest("hex"),
        } : undefined;
        try {
          lease = await startupPhase("sandbox.acquire", () => pool!.acquire(session, signal));
        } catch (error) {
          // A warm spare that never became ready must not fail the question.
          if (signal?.aborted) throw error;
          log(`warm sandbox unavailable, starting a fresh one: ${describeError(error)}`);
          pool = undefined;
          sandboxHermesHome = coldHermesHome;
        }
      }
      if (pool && lease) {
        warmSlot = lease.slot;
        if (!warmSlot) throw new Error("Warm sandbox admission returned no slot");
        name = warmSlot.name;
        sandboxCreated = true;
        if (signal?.aborted) throw abortError();
        await timed("warm_bind", async () => {
          const desiredHome = hermesStage ?? path.join(stageDir, "empty-home");
          await mkdir(desiredHome, { recursive: true });
          const bindPolicy = async () => {
            // A reused slot has the same current authorization/model scope.
            if (lease!.reused) return;
            if (opts.modelProvider) {
              await timed("warm_provider", () => run(["sandbox", "provider", "attach", name, opts.modelProvider!], 60_000));
            }
            await timed("warm_policy", () => run(["policy", "set", name, "--policy", policyFile, "--wait", "--timeout", "60"], 65_000));
            if (opts.modelProvider) {
              // Endpointless profile credentials remain withheld until the
              // policy binds this provider to the model endpoint.
              await timed("warm_provider_ready", () => run([
                "sandbox", "provider", "status", name, opts.modelProvider!,
                "--wait", "--timeout", "60",
              ], 65_000));
            }
          };
          const syncInputs = async () => {
            const phases = ["workspace", "config"];
            const results = await timed("inputs_sync", () => syncSandboxDirectories({
              checkout: true,
              directories: [
                { source: staged.orgRoot, destination: boxOrgRoot, layers: stableInputs ? [stableInputs.manifest] : undefined },
                { source: desiredHome, destination: sandboxHermesHome },
              ].map((directory, index) => ({
                ...directory, deltaRoot: path.join(stageDir, `delta-${phases[index]}`),
                upload: (delta: string) => timed(`${phases[index]}_upload`, () => run([
                  "sandbox", "upload", name, delta, path.posix.dirname(directory.destination), "--no-git-ignore",
                ], 120_000)).then(() => {}),
              })),
              reconcile: manifest => timed("inputs_reconcile", () => run([
                "sandbox", "exec", "-n", name, "--no-tty", "--",
                "/usr/local/uv/tools/hermes-agent/bin/python", "-c", RECONCILE_COMMAND,
              ], 30_000, manifest)),
            }));
            results.forEach((result, index) => startupEvent(`sandbox.${phases[index]}_delta`, result));
          };
          // OpenShell 0.1.2 rejects concurrent sandbox mutations with
          // "sandbox was modified by another operation". Attach the provider
          // and policy before syncing files against the settled revision.
          await bindPolicy();
          await syncInputs();
        });
        log(JSON.stringify({ type: "sandbox_warm", runId: input.runId, sandboxName: name,
          mode: lease.reused ? "user" : lease.slot ? "generic" : "miss" }));
      } else {
        const createArgs = [
          "sandbox",
          "create",
          "--name",
          name,
          "--from",
          opts.agentImage,
          "--cpu",
          cpu,
          "--memory",
          memory,
          "--detach",
          "--no-tty",
          "--no-auto-providers",
          ...sandboxOwnerLabelArgs(),
          ...(opts.modelProvider ? ["--provider", opts.modelProvider] : []),
          "--policy",
          policyFile,
          "--",
          "sleep",
          "infinity",
        ];
        const reclaimAndCreate = async () => {
          // Run names are deterministic so a durable queue retry can collide
          // with an OpenShell sandbox orphaned by a worker restart or deploy.
          // Replace only that exact run sandbox, then let the normal finally
          // path own cleanup for the newly created instance.
          log(`replacing stale agent sandbox after name collision: ${name}`);
          await runCleanup(["sandbox", "delete", name], 60_000);
          await timed("create", () => run(createArgs, 180_000));
        };
        try {
          await timed("create", () => run(createArgs, 180_000));
        } catch (error) {
          const message = error instanceof Error ? error.message : String(error);
          if (message.includes("already exists")) {
            await reclaimAndCreate();
          } else {
            // Not a name collision: a transient gateway hiccup (restart mid
            // deploy) or a first-use image pull that outran the timeout — the
            // gateway-side pull keeps going, so a second attempt usually rides
            // its cache. Retry once before surfacing the real error; a timed-out
            // first attempt may have half-registered the name, which the retry
            // then reclaims.
            log(
              `agent sandbox create failed (${message.slice(0, 200)}); retrying once`,
            );
            await new Promise((resolve) => setTimeout(resolve, 3_000));
            try {
              await timed("create", () => run(createArgs, 180_000));
            } catch (retryError) {
              const retryMessage =
                retryError instanceof Error ? retryError.message : String(retryError);
              if (!retryMessage.includes("already exists")) throw retryError;
              await reclaimAndCreate();
            }
          }
        }
        sandboxCreated = true;
        // OpenShell 0.1.2 rejects --upload together with a create command.
        // Keep the sandbox alive, then upload after creation has settled.
        await timed("workspace_upload", () => run([
          "sandbox", "upload", name, staged.orgRoot,
          path.posix.dirname(boxOrgRoot), "--no-git-ignore",
        ], 120_000));
        if (opts.modelProvider) {
          await timed("provider_ready", () => run([
            "sandbox", "provider", "status", name, opts.modelProvider!,
            "--wait", "--timeout", "60",
          ], 65_000));
        }
      }

      log(
        `agent sandbox ready: ${name} (backend=${input.backend.id}, kind=${kind}, ` +
          `graphjin=brokered, skill_overrides=${staged.skillOverrides.length})`,
      );
      await input.emit({ type: "status", message: "Agent is working…" });
      const result = await timed("exec", () => execAndStream(
        cli,
        gatewayArgs,
        name,
        buildInnerCommand({
          env: {
            OPENNEKO_RUN_JOB_FILE: sandboxJobPath,
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
            ...(opts.env ?? {}),
            ...(pool && hermes ? { OPENNEKO_HERMES_WARM: "1", HOME: sandboxHermesHome, HERMES_HOME: sandboxHermesHome } : {}),
            ...(hermesStage ? { HERMES_HOME: sandboxHermesHome } : {}),
          },
          keyAliases: opts.keyAliases,
        }),
        input.emit,
        // Must outlive the in-box turn budget with margin, so a long turn
        // dies as the backend's honest timeout error — not an opaque
        // exec-stream kill from out here.
        opts.execTimeoutMs ??
          (kind === "workflow"
            ? workflowExecBudgetMs(input as RunWorkflowAgentBackendInput)
            : (jobInput?.run.timeoutMs ?? agentTurnTimeoutMs())) + 120_000,
        signal,
        (present) => { artifactsPresent = present; },
      ));
      healthy = result.status === "completed" && !signal?.aborted;
      return result;
    } finally {
      await stableInputs?.release();
      opts.brokerRelease?.(input.runId);
      // Pull artifacts the agent wrote in the box back to the host run dir
      // before deleting the box — otherwise the file-serving endpoint reads an
      // empty host dir and 404s the download. Best-effort, and runs even when
      // the turn errored or timed out (the box is still alive here), so a
      // partial artifact from a long run isn't lost.
      if (sandboxCreated && !isJob && !signal?.aborted && artifactsPresent !== false) {
        await mkdir(input.workspace.artifactRoot, { recursive: true }).catch(
          () => {},
        );
        await timed("download", () => runCleanup(
          [
            "sandbox",
            "download",
            name,
            boxWorkspace.artifactRoot,
            input.workspace.artifactRoot,
          ],
          120_000,
        )).catch((e) => log(`artifact pull-back skipped: ${(e as Error).message}`));
      }
      if (sandboxCreated && skillActor && skillBaseline) {
        await timed("skill_capture", () => captureSandboxSkillEdits({
          listSandboxHashes: () => runCleanup([
            "sandbox", "exec", "-n", name, "--no-tty", "--",
            "/usr/local/uv/tools/hermes-agent/bin/python", "-c", SANDBOX_SKILL_HASH_COMMAND, boxWorkspace.skillsRoot,
          ], 60_000),
          downloadSkill: (skill, destination) => runCleanup([
            "sandbox", "download", name, path.posix.join(boxWorkspace.skillsRoot, skill), destination,
          ], 120_000).then(() => {}),
          orgRoot: hostOrgRoot,
          orgId: input.orgId,
          runId: input.runId,
          actor: skillActor!,
          baseline: skillBaseline!,
        }))
          .then((saved) => {
            if (saved) log(JSON.stringify({ type: "skill_capture", runId: input.runId, ...saved }));
          })
          .catch((e) => log(`skill capture failed: ${(e as Error).message}`));
      }
      // The sandbox is the process-tree boundary. Deleting it terminates the
      // backend plus every child/sub-agent, and cleanup must not inherit the
      // already-aborted run signal.
      if (lease) {
        await lease.release(warmSlot, healthy && !signal?.aborted);
      } else if (sandboxCreated) {
        await timed("delete", () => runCleanup(["sandbox", "delete", name], 60_000)).catch(() => {});
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

/** Script steps, then the first turn and every continuation, back to back. */
export function workflowExecBudgetMs(input: RunWorkflowAgentBackendInput): number {
  const turnMs = input.timeoutMs ?? agentTurnTimeoutMs();
  const stepsMs = (input.steps ?? [])
    .filter((step) => step.script)
    .reduce((sum, step) => sum + (step.script!.timeoutSeconds ?? 1_200) * 1_000, 0);
  return stepsMs + turnMs * (1 + (input.maxContinuations ?? 0));
}

/** `policy update` adding the model endpoint(s) scoped to the backend binary. */
export function buildModelEgressArgs(
  name: string,
  egress: ReadonlyArray<{ host: string; binary: string; port?: number }>,
): string[] | null {
  if (egress.length === 0) return null;
  const args = ["policy", "update", name];
  for (const { host, port } of egress) {
    args.push("--add-endpoint", `${host}:${port ?? 443}:read-write:rest:enforce`);
  }
  for (const { binary } of egress) args.push("--binary", binary);
  for (const { host, port } of egress)
    args.push("--add-allow", `${host}:${port ?? 443}:*:/**`);
  args.push("--wait", "--timeout", "60");
  return args;
}

export function buildScopedEgressArgs(
  name: string,
  egress: ReadonlyArray<{ host: string; binary: string; port?: number }>,
): string[][] {
  const byBinary = new Map<
    string,
    Array<{ host: string; binary: string; port?: number }>
  >();
  for (const rule of egress) {
    const rules = byBinary.get(rule.binary) ?? [];
    rules.push(rule);
    byBinary.set(rule.binary, rules);
  }
  return Array.from(byBinary.values()).flatMap((rules) => {
    const args = buildModelEgressArgs(name, rules);
    return args ? [args] : [];
  });
}

/**
 * Build the OpenShell runtime deps for runChatTurn from env. SEC9: OpenShell
 * is the only agent runtime — every production host injects this runCore;
 * tests inject their own in-process runCore via deps. Env-wired for now;
 * per-org auto-sync (provider/egress/key-var from the org row) is a follow-up.
 *
 * `broker` wires the sandbox MCP-tool path: the caller starts a host broker
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

/** One warm box serves Ask on the web. Worker runs are background work and start cold. */
function defaultWarmPoolSize(): number {
  return sandboxOwner().includes("worker") ? 0 : 1;
}

export function sandboxLauncherOptionsFromConfig(
  config: AgentRuntimeLaunchConfig,
  broker?: SandboxBrokerHandle,
): SandboxLauncherOptions {
  return {
    agentImage:
      process.env.OPENNEKO_AGENT_IMAGE ?? "ghcr.io/open-neko/agent:latest",
    gatewayName: process.env.OPENSHELL_GATEWAY || undefined,
    gatewayEndpoint: process.env.OPENSHELL_GATEWAY_ENDPOINT || undefined,
    cpu: process.env.OPENNEKO_AGENT_CPUS || undefined,
    memory: process.env.OPENNEKO_AGENT_MEMORY || undefined,
    warmPoolSize: Number(process.env.OPENNEKO_AGENT_WARM_POOL_SIZE ?? defaultWarmPoolSize()),
    warmIdleMs: Number(process.env.OPENNEKO_AGENT_WARM_IDLE_MS ?? 180_000),
    ...config,
    brokerUrl: broker?.url,
    brokerTokenFor: broker?.tokenFor,
    brokerRelease: broker?.release,
  };
}

export function sandboxLauncherOptionsFromEnv(
  broker?: SandboxBrokerHandle,
): SandboxLauncherOptions {
  // Comma-separated: the model endpoint AND any resolution hosts (hermes needs
  // models.dev), all scoped to the backend's one connecting binary.
  const hosts = (process.env.OPENNEKO_AGENT_MODEL_HOST ?? "")
    .split(",")
    .map((h) => h.trim())
    .filter(Boolean);
  // OpenShell injects the credential under the credential NAME (default
  // `MODEL_API_KEY`); alias it to the env var the backend reads (the hermes
  // provider→key map, e.g. GEMINI_API_KEY). The proxy swaps in the real key on
  // egress, so the box only ever holds the placeholder.
  const keyEnv = process.env.OPENNEKO_AGENT_MODEL_KEY_ENV;
  const configuredCredential = process.env.OPENNEKO_AGENT_MODEL_CREDENTIAL;
  const credName =
    !configuredCredential || configuredCredential === "api_key"
      ? "MODEL_API_KEY"
      : configuredCredential;
  return sandboxLauncherOptionsFromConfig({
    modelProvider: process.env.OPENNEKO_AGENT_MODEL_PROVIDER || undefined,
    modelHosts: hosts.map((host) => ({ host })),
    keyAliases: keyEnv ? [{ from: credName, to: keyEnv }] : undefined,
    hermesHomeHostPath: process.env.OPENNEKO_AGENT_HERMES_HOME || undefined,
  }, broker);
}

// Generic provider profile: holds just the model credential. The proxy
// substitutes the injected `openshell:resolve:env:…` placeholder wherever the
// agent puts it (query parameters or provider-specific auth headers,
// verified), so endpoints/binaries aren't needed here; egress is applied
// per-run by the launcher using the vendored executable. One profile covers
// every provider.
const OPENNEKO_AGENT_PROFILE_ID = "openneko-agent";
const OPENNEKO_AGENT_CREDENTIAL = "MODEL_API_KEY";
const OPENNEKO_AGENT_PROFILE_YAML = `id: ${OPENNEKO_AGENT_PROFILE_ID}
display_name: OpenNeko Agent
description: OpenNeko agent model credential (generic; egress applied per-run)
category: agent
credentials:
- name: api_key
  description: model API key — proxy substitutes the placeholder on egress
  env_vars:
  - MODEL_API_KEY
  required: true
  auth_style: query
  header_name: ''
  query_param: key
endpoints: []
binaries: []
inference_capable: false
discovery:
  credentials:
  - api_key
`;

/**
 * Ensure a gateway-side OpenShell provider exists holding the org's model key,
 * so the egress proxy can inject it (the key never enters the box). Idempotent:
 * registers the generic profile, then creates-or-updates the named provider.
 * Run at worker startup (provisionHostConfig) — replaces the manual
 * `openshell provider create` step. Egress + the key-env alias stay with the
 * launcher; this only owns the credential.
 */
/** Runs in flight keep the provider they started with; a replaced one goes after this. */
const STALE_PROVIDER_DELAY_MS = 30 * 60_000;

export async function ensureOpenShellProvider(opts: {
  providerName: string;
  apiKey: string;
  /** Name prefix of versioned providers: an existing name already holds this credential. */
  family?: string;
  staleDelayMs?: number;
  cli?: string;
  gatewayName?: string;
  gatewayEndpoint?: string;
}): Promise<void> {
  const cli = opts.cli ?? "openshell";
  const gatewayArgs = opts.gatewayName
    ? ["--gateway", opts.gatewayName]
    : opts.gatewayEndpoint
      ? ["--gateway-endpoint", opts.gatewayEndpoint]
      : [];
  const run = (args: string[]): Promise<string> =>
    runProcessOnce(cli, [...gatewayArgs, ...args], 60_000);

  const dir = await mkdtemp(path.join(tmpdir(), "oss-profile-"));
  let profileImportError: unknown;
  try {
    const file = path.join(dir, `${OPENNEKO_AGENT_PROFILE_ID}.yaml`);
    await writeFile(file, OPENNEKO_AGENT_PROFILE_YAML);
    // Import is upsert-ish; "already registered" is fine. But a swallowed
    // TRANSPORT failure (gateway restarting) means the profile type the
    // create below references may not exist — remember the failure so the
    // create's error path can surface the real cause instead of a cryptic
    // unknown-profile error.
    await run(["provider", "profile", "import", "--file", file]).catch((e) => {
      profileImportError = e;
    });
  } finally {
    await rm(dir, { recursive: true, force: true });
  }

  const credential = `${OPENNEKO_AGENT_CREDENTIAL}=${opts.apiKey}`;
  const safeError = (error: unknown): string =>
    opts.apiKey
      ? describeError(error).replaceAll(opts.apiKey, "[REDACTED_SECRET]")
      : describeError(error);
  const create = () => run([
    "provider", "create", "--name", opts.providerName,
    "--type", OPENNEKO_AGENT_PROFILE_ID, "--credential", credential,
  ]);
  if (opts.family) {
    const family = opts.family;
    const listed = (await run(["provider", "list", "--names"])).split(/\s+/).filter(Boolean);
    if (!listed.includes(opts.providerName)) {
      await create().catch((error) => {
        throw new Error(
          `OpenShell provider ${opts.providerName} creation failed: ${safeError(error)}` +
          (profileImportError ? `; profile import: ${safeError(profileImportError)}` : ""),
        );
      });
    }
    const stale = listed.filter((name) => name !== opts.providerName && (name === family || name.startsWith(`${family}-`)));
    if (stale.length > 0) {
      setTimeout(() => {
        void Promise.allSettled(stale.map((name) => run(["provider", "delete", name])));
      }, opts.staleDelayMs ?? STALE_PROVIDER_DELAY_MS).unref();
    }
    return;
  }

  // The provider exists after the first run, so refresh its key first. A
  // create-first order wrote a failed duplicate insert to the gateway's
  // database on every sync.
  try {
    await run(["provider", "update", opts.providerName, "--credential", credential]);
  } catch (updateError) {
    try {
      // A provider created before OpenShell 0.1.2 stores the legacy `api_key`
      // credential key, which 0.1.2 cannot update. Recreate it.
      if (describeError(updateError).includes("invalid argument")) {
        await run(["provider", "delete", opts.providerName]);
      }
      await create();
    } catch (createError) {
      throw new Error(
        `OpenShell provider ${opts.providerName} reconciliation failed: ` +
        `update: ${safeError(updateError)}; ` +
        `create: ${safeError(createError)}` +
        (profileImportError ? `; profile import: ${safeError(profileImportError)}` : ""),
      );
    }
  }
}

/** Remove one explicitly named gateway-side provider. Eval fixtures use this
 * after each isolated org so copied model credentials do not accumulate in
 * OpenShell after the corresponding metadata row has been deleted. */
export async function deleteOpenShellProvider(opts: {
  providerName: string;
  /** Also delete the versioned providers named `<providerName>-<fingerprint>`. */
  family?: boolean;
  cli?: string;
  gatewayName?: string;
  gatewayEndpoint?: string;
}): Promise<void> {
  const cli = opts.cli ?? "openshell";
  const gatewayArgs = opts.gatewayName
    ? ["--gateway", opts.gatewayName]
    : opts.gatewayEndpoint
      ? ["--gateway-endpoint", opts.gatewayEndpoint]
      : [];
  const names = opts.family
    ? (await runProcessOnce(cli, [...gatewayArgs, "provider", "list", "--names"], 60_000))
      .split(/\s+/)
      .filter((name) => name === opts.providerName || name.startsWith(`${opts.providerName}-`))
    : [opts.providerName];
  for (const name of names) {
    await runProcessOnce(cli, [...gatewayArgs, "provider", "delete", name], 60_000);
  }
}

/**
 * Prove that the configured OpenShell gateway is reachable over its real mTLS
 * control-plane path. `openshell status` is not suitable for readiness: it
 * exits successfully even when no gateway is configured. Listing providers is
 * a read-only gateway RPC, so a successful call proves both registration and
 * connectivity without creating a sandbox or exposing credentials.
 */
export async function verifyOpenShellGateway(opts: {
  cli?: string;
  gatewayName?: string;
  gatewayEndpoint?: string;
} = {}): Promise<void> {
  const cli = opts.cli ?? "openshell";
  const gatewayArgs = opts.gatewayName
    ? ["--gateway", opts.gatewayName]
    : opts.gatewayEndpoint
      ? ["--gateway-endpoint", opts.gatewayEndpoint]
      : [];
  await runProcessOnce(
    cli,
    [...gatewayArgs, "provider", "list", "--names"],
    15_000,
  );
}

/**
 * Mirror a host HERMES_HOME into `destination` KEYLESS: copy
 * config.yaml, write an empty `.env`. hermes reads `.env` before the process
 * env, so an empty `.env` lets the proxy-injected key (keyAliases) win — and
 * the real key never lands in the box.
 */
async function stageKeylessHermesHome(
  hostPath: string,
  destination: string,
): Promise<string> {
  await mkdir(destination, { recursive: true });
  const config = await readFile(path.join(hostPath, "config.yaml"), "utf8");
  await writeFile(path.join(destination, "config.yaml"), config);
  await writeFile(path.join(destination, ".env"), "");
  return destination;
}

function buildInnerCommand(o: {
  env: Record<string, string>;
  keyAliases?: ReadonlyArray<{ from: string; to: string }>;
}): string {
  const exports = Object.entries(o.env)
    .map(([k, v]) => {
      if (!SHELL_KEY_RX.test(k)) throw new Error(`bad env key ${k}`);
      return `export ${k}=${shellQuote(v)}`;
    })
    .join("; ");
  // Reference the OpenShell-injected var at runtime ("$from") — never a value.
  const aliases = (o.keyAliases ?? [])
    .map(({ from, to }) => {
      if (!SHELL_VARNAME_RX.test(from) || !SHELL_VARNAME_RX.test(to)) {
        throw new Error(`bad key alias ${from}->${to}`);
      }
      return `export ${to}="$${from}"`;
    })
    .join("; ");
  // Run from the deployed package root so tsx and workspace dependencies use
  // the exact production closure created by pnpm deploy.
  const parts = [
    exports,
    aliases,
    "cd /app",
    `exec node ${AGENT_ENTRY}`,
  ];
  return parts.filter(Boolean).join("; ");
}

function runProcessOnce(
  cmd: string,
  args: string[],
  timeoutMs: number,
  signal?: AbortSignal,
  stdin?: string,
): Promise<string> {
  return new Promise((resolve, reject) => {
    const child = spawn(cmd, args, { stdio: [stdin === undefined ? "ignore" : "pipe", "pipe", "pipe"] });
    let stdout = "";
    let stderr = "";
    let settled = false;
    let timer: ReturnType<typeof setTimeout>;
    const cleanup = () => {
      clearTimeout(timer);
      signal?.removeEventListener("abort", onAbort);
    };
    const fail = (err: Error) => {
      if (settled) return;
      settled = true;
      cleanup();
      reject(err);
    };
    const onAbort = () => {
      child.kill("SIGKILL");
      fail(abortError());
    };
    child.stdout!.on("data", (d: Buffer) => (stdout += d.toString("utf8")));
    child.stderr!.on("data", (d: Buffer) => (stderr += d.toString("utf8")));
    timer = setTimeout(() => {
      child.kill("SIGKILL");
      fail(new Error(`openshell ${args[0] ?? ""} timed out after ${timeoutMs}ms`));
    }, timeoutMs);
    timer.unref();
    if (signal?.aborted) onAbort();
    else signal?.addEventListener("abort", onAbort, { once: true });
    child.on("error", (err) => {
      fail(err);
    });
    child.on("close", (code) => {
      if (settled) return;
      settled = true;
      cleanup();
      if (code !== 0) {
        // Redact secret-bearing values (--credential name=value) — this
        // message reaches console logs.
        const shown = args
          .map((a, i) =>
            args[i - 1] === "--credential" ? a.replace(/=.*/u, "=[redacted]") : a,
          )
          .join(" ");
        reject(
          new Error(
            `openshell ${shown.slice(0, 160)} exited ${code}; stderr=${stderr.slice(0, 400)}`,
          ),
        );
        return;
      }
      resolve(stdout);
    });
    if (stdin !== undefined) {
      child.stdin?.on("error", error => {
        // EPIPE usually means the CLI rejected the command; let its exit status
        // report the actual error. Other transport failures must stop the run.
        if ((error as NodeJS.ErrnoException).code !== "EPIPE") {
          child.kill("SIGKILL");
          fail(error);
        }
      });
      child.stdin?.end(stdin);
    }
  });
}

/**
 * exec entry.ts and stream its tagged stdout: EVENT_MARKER lines → emit (host
 * scrubs + persists → web UI), the RESULT_MARKER line → the AgentRunResult.
 */
function execAndStream(
  cli: string,
  gatewayArgs: string[],
  name: string,
  innerCmd: string,
  emit: (event: AgentEvent) => Promise<void>,
  timeoutMs: number,
  signal?: AbortSignal,
  onArtifacts?: (present: boolean) => void,
): Promise<AgentRunResult> {
  return new Promise((resolve, reject) => {
    const timeoutSec = Math.max(1, Math.ceil(timeoutMs / 1000));
    const child = spawn(
      cli,
      [
        ...gatewayArgs,
        "sandbox",
        "exec",
        "-n",
        name,
        "--no-tty",
        "--timeout",
        String(timeoutSec),
        "--",
        "sh",
        "-c",
        innerCmd,
      ],
      { stdio: ["ignore", "pipe", "pipe"] },
    );
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
      child.kill("SIGKILL");
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
          /* ignore a partial/garbled event line */
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
      fail(new SandboxFailure("sandbox.exec_timeout", `agent sandbox exec timed out after ${timeoutMs}ms`));
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
            `agent sandbox exited ${code} without a result line; stderr=${stderr.slice(0, 500)}`,
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

/** Keep the creation command attached: --no-keep deletes the box when the
 * clean parent exits on its own idle timeout. */
async function createWarmSandbox(o: {
  cli: string; gatewayArgs: string[]; image: string; cpu: string; memory: string; idleMs: number;
  runCleanup: (args: string[], timeout: number) => Promise<string>;
  workspace?: StableWorkspace;
}): Promise<WarmSlot> {
  const name = `neko-p-${randomUUID().replaceAll("-", "").slice(0, 12)}`;
  const dir = await mkdtemp(path.join(tmpdir(), "oss-warm-"));
  const policy = path.join(dir, "policy.json");
  await writeFile(policy, JSON.stringify(buildSandboxPolicy([])));
  const child = spawn(o.cli, [...o.gatewayArgs, "sandbox", "create", "--name", name,
    "--from", o.image, "--cpu", o.cpu, "--memory", o.memory,
    "--no-tty", "--no-keep", "--no-auto-providers", ...sandboxOwnerLabelArgs(), "--policy", policy,
    "--", "/usr/local/uv/tools/hermes-agent/bin/python", "/app/hermes-warm.py", "serve",
    String(Math.ceil(o.idleMs / 1000))], { stdio: ["ignore", "pipe", "pipe"] });
  let alive = true;
  const closed = new Promise<void>(resolve => child.on("close", () => { alive = false; resolve(); }));
  child.stderr.resume();
  const destroy = async () => {
    try {
      await o.runCleanup(["sandbox", "delete", name], 60_000);
    } catch (error) {
      if (!/not found|does not exist/i.test(String(error))) throw error;
    } finally {
      alive = false;
      child.kill("SIGTERM");
    }
  };
  try {
    await new Promise<void>((resolve, reject) => {
      const lines = createInterface({ input: child.stdout });
      const timer = setTimeout(() => reject(new Error("warm sandbox readiness timeout")), 180_000);
      timer.unref();
      const done = (error?: Error) => { clearTimeout(timer); lines.close(); error ? reject(error) : resolve(); };
      lines.on("line", line => { if (line.trim() === "__openneko_warm_ready__") done(); });
      child.once("error", error => done(error));
      child.once("close", () => done(new Error("warm sandbox exited before readiness")));
    });
    child.stdout.resume();
    let preloadedRevision: string | undefined;
    const preload = async () => {
      if (!o.workspace) return;
      const inputs = await acquireStableSandboxInputs(o.workspace);
      try {
        if (preloadedRevision === inputs.revision) return;
        if (!preloadedRevision) {
          await startupPhase("sandbox.preload", () => o.runCleanup([
            "sandbox", "upload", name, inputs.root, "/sandbox", "--no-git-ignore",
          ], 120_000));
        } else {
          const stage = await mkdtemp(path.join(tmpdir(), "oss-preload-"));
          try {
            const empty = path.join(stage, "empty"); await mkdir(empty);
            await startupPhase("sandbox.preload", () => syncSandboxDirectories({
              directories: [{ source: empty, layers: [inputs.manifest], destination: path.posix.join("/sandbox", path.basename(inputs.root)), deltaRoot: path.join(stage, "delta"),
                upload: delta => o.runCleanup(["sandbox", "upload", name, delta, "/sandbox", "--no-git-ignore"], 120_000).then(() => {}),
              }],
              reconcile: manifest => runProcessOnce(o.cli, [...o.gatewayArgs, "sandbox", "exec", "-n", name, "--no-tty", "--",
                "/usr/local/uv/tools/hermes-agent/bin/python", "-c", RECONCILE_COMMAND], 30_000, undefined, manifest),
            }));
          } finally { await rm(stage, { recursive: true, force: true }); }
        }
        preloadedRevision = inputs.revision;
        startupEvent("sandbox.preload_delta", { files: Object.values(inputs.manifest.entries).filter(Boolean).length });
      } finally { await inputs.release(); }
    };
    await preload();
    return { name, alive: () => alive, destroy, closed,
      refresh: async () => {
        await o.runCleanup(["sandbox", "exec", "-n", name, "--no-tty", "--",
          "/usr/local/uv/tools/hermes-agent/bin/python", "/app/hermes-warm.py", "checkout"], 15_000);
        await preload();
      },
    };
  } catch (error) {
    await destroy().catch(() => {});
    throw error;
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
}
