import {
  access,
  cp,
  mkdir,
  mkdtemp,
  readFile,
  readdir,
  rm,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Readable, Writable } from "node:stream";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentEvent, AgentWorkspace } from "../src/agent-backend";
import type { RunAgentBackendInput } from "../src/work/agent-core";
import { GRAPHJIN_DIRECT_GOVERNED_POLICY } from "../src/work/graphjin-tool-policy";
import { SandboxFailure } from "../src/work/sandbox-failure";
import { KNOWLEDGE_FILES, refreshKnowledgeSnapshot } from "../src/knowledge-cache";
import type { RunWorkflowAgentBackendInput } from "../src/workflows/agent-core";

/**
 * The launcher shells out to the `openshell` CLI. We mock spawn: non-exec calls
 * (create/upload/delete) return empty stdout + exit 0; the `exec` call returns
 * a stdout stream carrying tagged EVENT + RESULT lines, which the launcher must
 * relay to `emit` and parse into the AgentRunResult.
 */
const h = vi.hoisted(() => {
  const calls: { args: string[]; stdin?: string }[] = [];
  const state = {
    holdExec: false,
    deleteMissing: false,
    collideOnNextCreate: false,
    failProviderReconcile: false,
    legacyProvider: false,
    missingProvider: false,
    providerNames: [] as string[],
    execLines: undefined as string[] | undefined,
  };
  function spawn(_cmd: string, args: string[]) {
    const call: { args: string[]; stdin?: string } = { args };
    calls.push(call);
    const isExec = args.includes("exec");
    const missingDelete = args.includes("delete") && state.deleteMissing;
    const createCollision =
      args.includes("create") && state.collideOnNextCreate;
    const providerFailure = state.failProviderReconcile && args.includes("provider") &&
      (args.includes("import") || args.includes("create") || args.includes("update"));
    if (createCollision) state.collideOnNextCreate = false;
    // A provider stored before OpenShell 0.1.2 exists and rejects updates
    // until it is deleted.
    const legacyProviderFailure = state.legacyProvider && args.includes("provider") &&
      (args.includes("create") || args.includes("update"));
    if (state.legacyProvider && args.includes("provider") && args.includes("delete")) state.legacyProvider = false;
    const missingProviderFailure = state.missingProvider && args.includes("provider") && args.includes("update");
    if (state.missingProvider && args.includes("provider") && args.includes("create")) state.missingProvider = false;
    const reg = (store: Record<string, Array<(...a: unknown[]) => void>>) =>
      (ev: string, cb: (...a: unknown[]) => void) => {
        (store[ev] ??= []).push(cb);
      };
    const fire = (
      store: Record<string, Array<(...a: unknown[]) => void>>,
      ev: string,
      ...a: unknown[]
    ) => (store[ev] ?? []).forEach((cb) => cb(...a));
    const ch: Record<string, Array<(...a: unknown[]) => void>> = {};
    const stderr = Readable.from(
      missingProviderFailure ? ["provider not found"] :
      legacyProviderFailure ? [args.includes("update") ? "provider update failed (Client specified an invalid argument)" : "provider already exists"] :
      providerFailure ? [`INVALID_ARGUMENT ${args.includes("update") ? "update rejected" : "other failure"}: SECRET-KEY`] :
      missingDelete ? ["sandbox not found"] : createCollision
        ? ["Error: × sandbox 'work-run-1' already exists\n"]
        : [],
    );
    const providerList = args.includes("provider") && args.includes("list");
    const lines = (providerList ? state.providerNames.map((name) => `${name}\n`) : isExec
      ? state.execLines ?? [
          'noise before\n',
          `\n__openneko_event__${JSON.stringify({ type: "message", role: "assistant", content: "hi" })}\n`,
          `\n__openneko_agent_result__${JSON.stringify({ status: "completed", finalText: "hi there", threadState: { t: 1 } })}\n`,
        ]
      : []);
    let closed = false;
    const closeOnce = () => {
      if (closed) return;
      closed = true;
      fire(ch, "close", providerFailure || legacyProviderFailure || missingProviderFailure || createCollision || missingDelete ? 1 : 0);
    };
    const stdout = Readable.from(lines);
    const stdin = new Writable({
      write(chunk, _encoding, done) { call.stdin = (call.stdin ?? "") + chunk.toString(); done(); },
    });
    if (!isExec || !state.holdExec) {
      stdout.on("end", () => queueMicrotask(closeOnce));
    }
    return {
      stdin,
      stdout,
      stderr,
      on: reg(ch),
      once: reg(ch),
      kill() {
        closeOnce();
        return true;
      },
    };
  }
  return { calls, state, spawn };
});

vi.mock("node:child_process", () => ({ spawn: h.spawn }));

// Capture the job descriptor the launcher writes (then uploads to the box), so
// we can assert exactly what crosses the host→sandbox boundary.
const jobCapture = vi.hoisted(() => ({
  jobs: [] as Array<Record<string, unknown>>,
  policies: [] as Array<ReturnType<typeof JSON.parse>>,
}));
vi.mock("node:fs/promises", async (importOriginal) => {
  const actual = await importOriginal<typeof import("node:fs/promises")>();
  return {
    ...actual,
    writeFile: async (p: unknown, data: unknown, ...rest: unknown[]) => {
      if (typeof p === "string" && p.endsWith("job.json")) {
        try {
          jobCapture.jobs.push(JSON.parse(String(data)));
        } catch {
          /* ignore non-JSON writes */
        }
      }
      if (typeof p === "string" && p.endsWith("policy.json")) {
        try {
          jobCapture.policies.push(JSON.parse(String(data)));
        } catch {
          /* ignore non-JSON writes */
        }
      }
      return (actual.writeFile as (...a: unknown[]) => Promise<void>)(p, data, ...rest);
    },
  };
});

const runActor = vi.hoisted(() => ({ actor: null as { userId: string | null; role: string | null } | null }));
vi.mock("../src/work/personas", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../src/work/personas")>()),
  getWorkRunActor: async () => {
    if (!runActor.actor) throw new Error("no database");
    return runActor.actor;
  },
}));

const {
  makeSandboxRunCore,
  prepareSandboxCapacity,
  closeSandboxReapers,
  reapStrandedSandboxes,
  sandboxOwnerLabelArgs,
  makeSandboxJobRunCore,
  makeSandboxWorkflowRunCore,
  buildModelEgressArgs,
  buildSandboxPolicy,
  buildScopedEgressArgs,
  deleteOpenShellProvider,
  ensureOpenShellProvider,
  sandboxLauncherOptionsFromConfig,
  sandboxLauncherOptionsFromEnv,
  stageSandboxWorkspace,
  verifyOpenShellGateway,
  workflowExecBudgetMs,
} = await import("../src/work/sandbox-launcher");

describe("reapStrandedSandboxes", () => {
  it("shares one boot label across module instances in the same process", async () => {
    const current = sandboxOwnerLabelArgs("openneko-web");
    vi.resetModules();
    const reloaded = await import("../src/work/sandbox-launcher");
    expect(reloaded.sandboxOwnerLabelArgs("openneko-web")).toEqual(current);
  });

  it("deletes only sandboxes owned by a previous boot", async () => {
    const deleted: string[] = [];
    const run = async (args: string[]) => {
      if (args.includes("list")) return JSON.stringify({ sandboxes: [
        { name: "old", labels: { "openneko.owner": "openneko-web", "openneko.boot": "previous" } },
        { name: "current", labels: { "openneko.owner": "openneko-web", "openneko.boot": "current" } },
      ], next_page_token: "" });
      deleted.push(args.at(-1)!);
      return "";
    };
    expect(await reapStrandedSandboxes(run, "openneko-web", "current")).toEqual(["old"]);
    expect(deleted).toEqual(["old"]);
  });

  it("reports failed deletion so the periodic pass can retry", async () => {
    const run = async (args: string[]) => {
      if (args.includes("list")) return JSON.stringify({ sandboxes: [{ name: "old", labels: { "openneko.boot": "previous" } }], next_page_token: "" });
      throw new Error("gateway unavailable");
    };
    await expect(reapStrandedSandboxes(run, "openneko-web", "current")).rejects.toThrow("could not delete 1 stranded sandboxes");
  });
});

describe("sandboxLauncherOptionsFromEnv", () => {
  it("accepts the legacy api_key setting on OpenShell 0.1.2", () => {
    vi.stubEnv("OPENNEKO_AGENT_MODEL_CREDENTIAL", "api_key");
    vi.stubEnv("OPENNEKO_AGENT_MODEL_KEY_ENV", "GEMINI_API_KEY");
    try {
      expect(sandboxLauncherOptionsFromEnv().keyAliases).toEqual([
        { from: "MODEL_API_KEY", to: "GEMINI_API_KEY" },
      ]);
    } finally {
      vi.unstubAllEnvs();
    }
  });

  it("ignores a persisted operator binary and exposes only model hosts", () => {
    vi.stubEnv("OPENNEKO_AGENT_MODEL_HOST", "models.example.com,models-2.example.com");
    vi.stubEnv(
      "OPENNEKO_AGENT_MODEL_BINARY",
      "/usr/local/uv/python/cpython-3.11.16-linux-x86_64-gnu/bin/python3.11",
    );
    try {
      const options = sandboxLauncherOptionsFromEnv();
      expect(options.modelHosts).toEqual([
        { host: "models.example.com" },
        { host: "models-2.example.com" },
      ]);
      expect(options).not.toHaveProperty("modelEgress");
    } finally {
      vi.unstubAllEnvs();
    }
  });
});

describe("sandboxLauncherOptionsFromConfig", () => {
  it("forwards resource limits from the host environment", () => {
    vi.stubEnv("OPENNEKO_AGENT_CPUS", "500m");
    vi.stubEnv("OPENNEKO_AGENT_MEMORY", "2Gi");
    try {
      expect(sandboxLauncherOptionsFromConfig({})).toMatchObject({ cpu: "500m", memory: "2Gi" });
    } finally {
      vi.unstubAllEnvs();
    }
  });

  it("uses the run-local provider snapshot instead of stale process globals", () => {
    vi.stubEnv(
      "OPENNEKO_AGENT_MODEL_HOST",
      "generativelanguage.googleapis.com",
    );
    vi.stubEnv("OPENNEKO_AGENT_MODEL_KEY_ENV", "GEMINI_API_KEY");
    vi.stubEnv("OPENNEKO_AGENT_MODEL_PROVIDER", "stale-gemini-provider");
    try {
      const options = sandboxLauncherOptionsFromConfig({
        modelProvider: "openneko-agent",
        modelHosts: [
          { host: "api.anthropic.com" },
        ],
        keyAliases: [{ from: "api_key", to: "ANTHROPIC_API_KEY" }],
      });

      expect(options).toMatchObject({
        modelProvider: "openneko-agent",
        modelHosts: [
          { host: "api.anthropic.com" },
        ],
        keyAliases: [{ from: "api_key", to: "ANTHROPIC_API_KEY" }],
      });
    } finally {
      vi.unstubAllEnvs();
    }
  });
});

function fakeInput(
  emit: (e: AgentEvent) => Promise<void>,
  backend?: RunAgentBackendInput["backend"],
): RunAgentBackendInput {
  return {
    backend:
      backend ??
      ({ id: "ax", capabilities: { mcpTools: false } } as RunAgentBackendInput["backend"]),
    prompt: "PROMPT",
    userMessage: "hello",
    orgId: "org-1",
    threadId: "thr-1",
    runId: "run-1",
    workspace: fullWorkspace("/tmp/ws/org-1"),
    threadState: undefined,
    pluginActions: [],
    emit,
  };
}

function fakeWorkflowInput(
  emit: (e: AgentEvent) => Promise<void>,
  backend?: RunWorkflowAgentBackendInput["backend"],
  workspaceOverride?: AgentWorkspace,
): RunWorkflowAgentBackendInput {
  return {
    backend:
      backend ??
      ({ id: "ax", capabilities: { mcpTools: false } } as RunWorkflowAgentBackendInput["backend"]),
    prompt: "WORKFLOW PROMPT",
    userMessage: "begin",
    orgId: "org-1",
    threadId: "thr-1",
    runId: "run-1",
    workflowRunId: "workflow-run-1",
    mode: "headless",
    networkHosts: [],
    triggeredByObservationId: "obs-1",
    workspace:
      workspaceOverride ??
      fullWorkspace("/tmp/ws/org-1"),
    emit,
  };
}

function fakeJobInput(
  workspace: AgentWorkspace,
  access: {
    graphjinRead?: boolean;
    graphjinAgent?: boolean;
    memorySearch?: boolean;
  } = {},
) {
  return {
    backend: {
      id: "ax",
      capabilities: { mcpTools: true },
    } as RunAgentBackendInput["backend"],
    orgId: "org-1",
    runId: "job-1",
    workspace,
    run: {
      prompt: "JOB PROMPT",
      timeoutMs: 45_000,
      retries: 0,
      tag: "profile-job",
      workspace,
    },
    access,
    emit: async () => {},
  };
}

function fullWorkspace(orgRoot: string): AgentWorkspace {
  return {
    orgRoot,
    skillsRoot: join(orgRoot, "skills"),
    memoryRoot: join(orgRoot, "memory"),
    knowledgeRoot: join(orgRoot, "knowledge"),
    uploadsRoot: join(orgRoot, "uploads"),
    runsRoot: join(orgRoot, "runs"),
    threadUploadsRoot: join(orgRoot, "uploads", "thr-1"),
    runRoot: join(orgRoot, "runs", "run-1"),
    artifactRoot: join(orgRoot, "runs", "run-1", "artifacts"),
    binRoot: join(orgRoot, "runs", "run-1", "bin"),
  };
}

describe("buildModelEgressArgs", () => {
  it("returns null with no egress", () => {
    expect(buildModelEgressArgs("s", [])).toBeNull();
  });
  it("emits per-host endpoints + a binary scope + all-path allows", () => {
    expect(
      buildModelEgressArgs("s", [
        { host: "generativelanguage.googleapis.com", binary: "/usr/local/uv/python/x/bin/python3.11" },
      ]),
    ).toEqual([
      "policy",
      "update",
      "s",
      "--add-endpoint",
      "generativelanguage.googleapis.com:443:read-write:rest:enforce",
      "--binary",
      "/usr/local/uv/python/x/bin/python3.11",
      "--add-allow",
      "generativelanguage.googleapis.com:443:*:/**",
      "--wait",
      "--timeout",
      "60",
    ]);
  });
  it("honours an explicit non-443 port (the broker channel)", () => {
    expect(
      buildModelEgressArgs("s", [
        { host: "host.openshell.internal", binary: "/usr/local/bin/node", port: 4199 },
      ]),
    ).toEqual([
      "policy",
      "update",
      "s",
      "--add-endpoint",
      "host.openshell.internal:4199:read-write:rest:enforce",
      "--binary",
      "/usr/local/bin/node",
      "--add-allow",
      "host.openshell.internal:4199:*:/**",
      "--wait",
      "--timeout",
      "60",
    ]);
  });

  it("keeps endpoint allowlists separated by executable", () => {
    const updates = buildScopedEgressArgs("s", [
      { host: "models.example.com", binary: "/bin/model" },
      { host: "graphjin.internal", binary: "/bin/graphjin", port: 8080 },
      { host: "broker.internal", binary: "/bin/node", port: 4199 },
    ]);

    expect(updates).toHaveLength(3);
    const model = updates.find((args) => args.includes("/bin/model"))!;
    expect(model.join(" ")).toContain("models.example.com:443");
    expect(model.join(" ")).not.toContain("graphjin.internal");
    expect(model.join(" ")).not.toContain("broker.internal");
  });
});

describe("buildSandboxPolicy", () => {
  it("builds the complete creation policy with executable-scoped endpoints", () => {
    const policy = buildSandboxPolicy([
      { host: "models.example.com", binary: "/bin/model", credentialProvider: "model-provider" },
      { host: "models.example.com", binary: "/bin/model" },
      { host: "graphjin.internal", binary: "/bin/graphjin", port: 8080 },
    ]);

    expect(policy.filesystem_policy.read_write).toContain("/sandbox");
    const entries = Object.values(policy.network_policies);
    expect(entries).toHaveLength(2);
    const model = entries.find((entry) => entry.binaries[0]?.path === "/bin/model")!;
    expect(model.endpoints).toHaveLength(1);
    expect(model.endpoints[0]).toMatchObject({
      host: "models.example.com",
      port: 443,
      protocol: "rest",
      enforcement: "enforce",
      credential_binding: { provider: "model-provider" },
    });
    expect(model.endpoints.some((endpoint) => endpoint.host === "graphjin.internal"))
      .toBe(false);
  });
});

describe("stageSandboxWorkspace", () => {
  it("materializes one cached snapshot without old files or cache metadata", async () => {
    const root = await mkdtemp(join(tmpdir(), "knowledge-stage-test-"));
    try {
      const workspace = fullWorkspace(join(root, "org"));
      await refreshKnowledgeSnapshot({ root: workspace.knowledgeRoot, source: "source", mode: "agentic",
        revision: async () => "r1", build: async dir => {
          await Promise.all(KNOWLEDGE_FILES.map(file => writeFile(join(dir, file), file === "INDEX.md" ? "index" : '{"current":true}')));
          return { ok: true, files: [] };
        },
      });
      await writeFile(join(workspace.knowledgeRoot, "tables.json"), '{"obsolete":true}');
      const staged = await stageSandboxWorkspace(workspace, join(root, "stage"));
      expect((await readdir(staged.workspace.knowledgeRoot)).sort()).toEqual([...KNOWLEDGE_FILES].sort());
      expect(await readFile(join(staged.workspace.knowledgeRoot, "tables.json"), "utf8")).toBe('{"current":true}');
    } finally { await rm(root, { recursive: true, force: true }); }
  });
  it("exposes only the current run, current thread uploads, knowledge, and skill overrides", async () => {
    const root = await mkdtemp(join(tmpdir(), "sandbox-stage-source-"));
    const stage = await mkdtemp(join(tmpdir(), "sandbox-stage-dest-"));
    try {
      const workspace = fullWorkspace(root);
      await Promise.all([
        mkdir(workspace.knowledgeRoot, { recursive: true }),
        mkdir(workspace.threadUploadsRoot, { recursive: true }),
        mkdir(workspace.runRoot, { recursive: true }),
        mkdir(join(workspace.uploadsRoot, "other-thread"), { recursive: true }),
        mkdir(join(workspace.runsRoot, "other-run"), { recursive: true }),
        mkdir(workspace.memoryRoot, { recursive: true }),
        mkdir(join(workspace.skillsRoot, "quarterly-review"), { recursive: true }),
      ]);
      await cp(
        join(process.cwd(), "assets", "builtin-skills", "records"),
        join(workspace.skillsRoot, "records"),
        { recursive: true },
      );
      await Promise.all([
        writeFile(join(workspace.knowledgeRoot, "schema.json"), "{}"),
        writeFile(join(workspace.threadUploadsRoot, "current.csv"), "current"),
        writeFile(join(workspace.runRoot, "current.txt"), "current"),
        writeFile(join(workspace.uploadsRoot, "other-thread", "private.txt"), "private"),
        writeFile(join(workspace.runsRoot, "other-run", "secret.txt"), "secret"),
        writeFile(join(workspace.memoryRoot, "member-memory.txt"), "private memory"),
        writeFile(
          join(workspace.skillsRoot, "quarterly-review", "SKILL.md"),
          "---\nname: quarterly-review\ndescription: Review quarters\n---\n",
        ),
        // Simulate a durable workspace left behind by an older release.
        writeFile(
          join(workspace.skillsRoot, "records", "SKILL.md"),
          "---\nname: records\ndescription: stale system skill\n---\n",
        ),
      ]);

      const staged = await stageSandboxWorkspace(workspace, stage, {
        requiredSkillNames: ["records"],
      });
      expect(staged.skillOverrides).toEqual(["quarterly-review", "records"]);
      await expect(
        access(join(staged.workspace.skillsRoot, "records", "SKILL.md")),
      ).resolves.toBeUndefined();
      expect(
        await readFile(join(staged.workspace.skillsRoot, "records", "SKILL.md"), "utf8"),
      ).toContain("objective urgency cannot be determined");
      expect(await readFile(join(staged.workspace.knowledgeRoot, "schema.json"), "utf8"))
        .toBe("{}");
      expect(await readFile(join(staged.workspace.threadUploadsRoot, "current.csv"), "utf8"))
        .toBe("current");
      expect(await readFile(join(staged.workspace.runRoot, "current.txt"), "utf8"))
        .toBe("current");
      await expect(
        access(join(staged.workspace.uploadsRoot, "other-thread", "private.txt")),
      ).rejects.toThrow();
      await expect(
        access(join(staged.workspace.runsRoot, "other-run", "secret.txt")),
      ).rejects.toThrow();
      await expect(
        access(join(staged.workspace.memoryRoot, "member-memory.txt")),
      ).rejects.toThrow();
    } finally {
      await Promise.all([
        rm(root, { recursive: true, force: true }),
        rm(stage, { recursive: true, force: true }),
      ]);
    }
  });
});

describe("makeSandboxRunCore", () => {
  beforeEach(() => {
    h.calls.length = 0;
    h.state.holdExec = false;
    h.state.deleteMissing = false;
    h.state.collideOnNextCreate = false;
    h.state.execLines = undefined;
    jobCapture.jobs.length = 0;
    jobCapture.policies.length = 0;
  });
  afterEach(async () => { await closeSandboxReapers(); vi.restoreAllMocks(); });

  it("prepares no idle sandbox at startup and runs each turn in a fresh one", async () => {
    const logs: string[] = [];
    const options = { agentImage: "startup-test", onLog: (line: string) => logs.push(line) };
    await prepareSandboxCapacity(options);
    expect(h.calls.filter(call => call.args.includes("create"))).toHaveLength(0);
    await makeSandboxRunCore(options)(fakeInput(async () => {}));
    expect(h.calls.filter(call => call.args.includes("create"))).toHaveLength(1);
    expect(logs.some(line => line.includes('"phase":"create"'))).toBe(true);
  });

  it("tolerates cleanup when the sandbox was already deleted", async () => {
    h.state.deleteMissing = true;
    const core = makeSandboxRunCore({ agentImage: "test", onLog: () => {} });
    await expect(core(fakeInput(async () => {}))).resolves.toMatchObject({ status: "completed" });
  });

  it("carries the Ax route, not a separate model field, into the sandbox job", async () => {
    const runCore = makeSandboxRunCore({
      agentImage: "ghcr.io/open-neko/agent:test",
      onLog: () => {},
    });
    const ax = { route: { provider: "google-gemini", model: "gemini-3.6-flash", keyEnv: "GEMINI_API_KEY" } };
    await runCore(
      fakeInput(async () => {}, {
        id: "ax",
        ax,
        configuredIdentity: { provider: "google-gemini", model: "gemini-3.6-flash" },
        model: "gemini-3.6-flash",
        capabilities: { mcpTools: true, sessionResume: false },
        run: async () => ({ finalText: "", status: "completed" }),
      } as unknown as RunAgentBackendInput["backend"]),
    );

    expect(jobCapture.jobs.at(-1)).toMatchObject({ backendId: "ax", ax });
    expect(jobCapture.jobs.at(-1)?.model).toBeUndefined();
  });

  it("carries the per-run GraphJin policy into the OpenShell job", async () => {
    const runCore = makeSandboxRunCore({
        agentImage: "ghcr.io/open-neko/agent:test",
      onLog: () => {},
    });
    await runCore({
      ...fakeInput(async () => {}),
      graphjinToolPolicy: GRAPHJIN_DIRECT_GOVERNED_POLICY,
    });
    expect(jobCapture.jobs.at(-1)?.graphjinToolPolicy).toEqual({
      mode: "direct-governed",
    });
  });

  it("never injects direct GraphJin credentials into records turns", async () => {
    const runCore = makeSandboxRunCore({
        agentImage: "ghcr.io/open-neko/agent:test",
      onLog: () => {},
    });
    await runCore({
      ...fakeInput(async () => {}),
      dataSurface: "records",
    });
    expect(jobCapture.jobs.at(-1)).toMatchObject({
      kind: "work",
      dataSurface: "records",
    });
    expect(jobCapture.jobs.at(-1)).not.toHaveProperty("graphjinEnabled");
    expect(jobCapture.jobs.at(-1)).not.toHaveProperty("graphjinDenied");
    expect(jobCapture.jobs.at(-1)).not.toHaveProperty("graphjinServerUrl");
    expect(jobCapture.jobs.at(-1)).not.toHaveProperty("graphjinClientConfig");
  });

  it.each([
    ["false", false],
    ["true", true],
    ["null", true],
  ])("downloads only when artifacts are present or unknown (%s)", async (hint, download) => {
    h.state.execLines = [
      '__openneko_agent_result__{"status":"completed","finalText":"done"}\n',
      `__openneko_artifacts__${hint}\n`,
    ];
    const logs: string[] = [];
    await makeSandboxRunCore({ agentImage: "test", onLog: (line) => logs.push(line) })(fakeInput(async () => {}));
    expect(h.calls.some((call) => call.args.includes("download"))).toBe(download);
    expect(h.calls.at(-1)?.args).toContain("delete");
    const phases = logs.filter((line) => line.startsWith("{")).map((line) => JSON.parse(line));
    expect(phases.map((p) => p.phase)).toEqual([
      "stage", "create", "workspace_upload", "exec", ...(download ? ["download"] : []), "delete", "total",
    ]);
    expect(phases.every((p) => p.durationMs >= 0)).toBe(true);
  });

  it("creates a running sandbox before uploading its workspace", async () => {
    await makeSandboxRunCore({ agentImage: "test", onLog: () => {} })(fakeInput(async () => {}));
    const createIndex = h.calls.findIndex(call => call.args.includes("create"));
    const uploadIndex = h.calls.findIndex(call => call.args.includes("upload"));
    expect(createIndex).toBeGreaterThanOrEqual(0);
    expect(uploadIndex).toBeGreaterThan(createIndex);
    expect(h.calls[createIndex]?.args).toEqual(expect.arrayContaining(["--", "sleep", "infinity"]));
    expect(h.calls[createIndex]?.args).not.toContain("--upload");
    expect(h.calls[uploadIndex]?.args).toContain("--no-git-ignore");
  });

  it("recovers partial artifacts when the agent fails without a filesystem hint", async () => {
    h.state.execLines = [];
    const error = await makeSandboxRunCore({ agentImage: "test", onLog: () => {} })(fakeInput(async () => {})).catch((e: unknown) => e);
    expect(error).toBeInstanceOf(SandboxFailure);
    expect(error).toMatchObject({ code: "sandbox.no_result" });
    expect(h.calls.some((call) => call.args.includes("download"))).toBe(true);
    expect(h.calls.at(-1)?.args).toContain("delete");
  });

  it("applies configurable resource limits and rejects invalid quantities before spawning", async () => {
    await makeSandboxRunCore({ agentImage: "test", cpu: "500m", memory: "2Gi", onLog: () => {} })(fakeInput(async () => {}));
    expect(h.calls[0]?.args).toEqual(expect.arrayContaining(["--cpu", "500m", "--memory", "2Gi"]));
    for (const cpu of ["0", "0.0", "-1", "unlimited", "1;exit"]) {
      expect(() => makeSandboxRunCore({ agentImage: "test", cpu })).toThrow(/CPU limit/);
    }
    for (const memory of ["0", "-1Gi", "unlimited", "4Gi;exit"]) {
      expect(() => makeSandboxRunCore({ agentImage: "test", memory })).toThrow(/memory limit/);
    }
  });

  it("creates, uploads, exec-streams, returns the result, and deletes", async () => {
    const events: AgentEvent[] = [];
    const runCore = makeSandboxRunCore({
        agentImage: "ghcr.io/open-neko/agent:test",
      modelHosts: [{ host: "m.example.com" }],
      keyAliases: [{ from: "api_key", to: "GEMINI_API_KEY" }],
      onLog: () => {},
    });

    const result = await runCore(fakeInput(async (e) => void events.push(e)));

    const verbs = h.calls.map((c) => c.args.find((a) => ["create", "update", "upload", "exec", "download", "delete"].includes(a)));
    // artifacts are pulled back from the box (download) before it's deleted
    expect(verbs).toEqual(["create", "upload", "exec", "download", "delete"]);
    const download = h.calls.find((c) => c.args.includes("download"));
    expect(download?.args.at(-1)).toBe(fakeInput().workspace.artifactRoot);
    // streamed event reached emit:
    expect(events.filter((event) => event.type === "message")).toEqual([
      expect.objectContaining({ type: "message", content: "hi" }),
    ]);
    expect(events.filter((event) => event.type === "status").at(-1)).toEqual({
      type: "status",
      message: "Agent is working…",
    });
    // result parsed from the RESULT line:
    expect(result).toEqual({ status: "completed", finalText: "hi there", threadState: { t: 1 } });
    // The detached main process keeps the box ready; exec runs the bundle:
    expect(h.calls[0]?.args).toContain("ghcr.io/open-neko/agent:test");
    expect(h.calls[0]?.args).toContain("--policy");
    expect(h.calls[0]?.args).not.toContain("--upload");
    const modelPolicy = Object.values(
      (jobCapture.policies.at(-1)?.network_policies ?? {}) as Record<
        string,
        { binaries: Array<{ path: string }>; endpoints: Array<{ host: string }> }
      >,
    ).find((policy) => policy.endpoints.some((endpoint) => endpoint.host === "m.example.com"));
    expect(modelPolicy?.binaries).toEqual([{ path: "/usr/local/bin/ax-harness" }]);
    expect(h.calls[0]?.args.slice(-2)).toEqual(["sleep", "infinity"]);
    expect(h.calls[0]?.args).toContain("--detach");
    expect(h.calls[0]?.args).toContain("--cpu");
    expect(h.calls[0]?.args).toContain("--memory");
    expect(h.calls[0]?.args).toContain("1Gi");
    const execCall = h.calls.find((c) => c.args.includes("exec"));
    const execCommand = execCall?.args.join(" ") ?? "";
    expect(execCommand).toContain(
      "node /app/entry.js",
    );
    // The agent self-resolves immutable image assets; the OpenShell command
    // carries no ambient image environment across the security boundary.
    expect(execCommand).not.toContain("OPENNEKO_BUILTIN_SKILLS_ROOT");
    expect(execCommand).not.toContain("OPENNEKO_MCP_BRIDGE");
    // the credential alias references the OpenShell-injected var at runtime, never a value:
    expect(execCommand).toContain('export GEMINI_API_KEY="$api_key"');
  });

  it("serializes pack actions into the isolated Work job", async () => {
    const runCore = makeSandboxRunCore({
        agentImage: "ghcr.io/open-neko/agent:test",
      onLog: () => {},
    });
    const input = fakeInput(async () => {});
    input.packActions = [{
      kind: "magento.manage_catalog",
      description: "Change Magento catalog data.",
      scope: "external",
      default_mode: "ask",
    }];

    await runCore(input);

    expect(jobCapture.jobs.at(-1)).toMatchObject({
      kind: "work",
      packActions: input.packActions,
      pluginActions: [],
    });
  });

  it("serializes and drains streamed events before accepting the result", async () => {
    h.state.execLines = [
      `__openneko_event__${JSON.stringify({ type: "message", role: "assistant", content: "first" })}\n`,
      `__openneko_event__${JSON.stringify({ type: "message", role: "assistant", content: "second" })}\n`,
      `__openneko_agent_result__${JSON.stringify({ status: "completed", finalText: "firstsecond" })}\n`,
    ];
    const events: string[] = [];
    let activeEmits = 0;
    let maxActiveEmits = 0;
    const runCore = makeSandboxRunCore({
        agentImage: "ghcr.io/open-neko/agent:test",
      onLog: () => {},
    });

    const result = await runCore(
      fakeInput(async (event) => {
        if (event.type !== "message") return;
        activeEmits += 1;
        maxActiveEmits = Math.max(maxActiveEmits, activeEmits);
        if (event.content === "first") {
          await new Promise((resolve) => setTimeout(resolve, 20));
        }
        events.push(event.content);
        activeEmits -= 1;
      }),
    );

    expect(result.status).toBe("completed");
    expect(events).toEqual(["first", "second"]);
    expect(maxActiveEmits).toBe(1);
  });

  it("rejects an empty completed result with no answer event", async () => {
    h.state.execLines = [
      `__openneko_agent_result__${JSON.stringify({ status: "completed", finalText: "", rawText: "" })}\n`,
    ];
    const runCore = makeSandboxRunCore({
        agentImage: "ghcr.io/open-neko/agent:test",
      onLog: () => {},
    });

    await expect(runCore(fakeInput(async () => {}))).rejects.toThrow(
      /completed without assistant output or surface/,
    );
  });

  describe("skill capture", () => {
    const hashExecs = () => h.calls.filter((call) => call.args.includes("exec") && call.args.includes("/usr/bin/python3"));
    const runWith = async (lines: string[]) => {
      h.state.execLines = [
        ...lines,
        `__openneko_agent_result__${JSON.stringify({ status: "completed", finalText: "done" })}\n`,
      ];
      const logs: string[] = [];
      runActor.actor = { userId: null, role: null };
      try {
        await makeSandboxRunCore({ agentImage: "test", onLog: (line) => logs.push(line) })(fakeInput(async () => {}));
      } finally {
        runActor.actor = null;
      }
      return logs.filter((line) => line.includes('"phase":"skill_capture"')).map((line) => JSON.parse(line));
    };
    const tool = (name: string, input: unknown) =>
      `__openneko_event__${JSON.stringify({ type: "tool_start", id: name, name, input })}\n`;

    it("skips the in-box hash when no tool wrote under the skills root", async () => {
      const phases = await runWith([tool("file_write", { path: "runs/run-1/artifacts/a.md", content: "x" })]);
      expect(hashExecs()).toHaveLength(0);
      expect(phases).toEqual([expect.objectContaining({ skipped: true, reason: "no_skill_writes", durationMs: 0 })]);
    });

    it("hashes the box once after a terminal command", async () => {
      const phases = await runWith([tool("terminal", { command: "touch x" })]);
      expect(hashExecs()).toHaveLength(1);
      expect(phases).toEqual([expect.objectContaining({ reason: "shell" })]);
      expect(phases[0].skipped).toBeUndefined();
    });

    it("captures when an event line cannot be read", async () => {
      const phases = await runWith(["__openneko_event__{broken\n"]);
      expect(hashExecs()).toHaveLength(1);
      expect(phases).toEqual([expect.objectContaining({ reason: "evidence_missing" })]);
    });
  });

  it("rejects the run when ordered event delivery fails", async () => {
    const runCore = makeSandboxRunCore({
        agentImage: "ghcr.io/open-neko/agent:test",
      onLog: () => {},
    });

    await expect(
      runCore(
        fakeInput(async (event) => {
          if (event.type === "message") throw new Error("database unavailable");
        }),
      ),
    ).rejects.toThrow(/agent event delivery failed: database unavailable/);
  });

  it("accepts a surface-only completed result", async () => {
    h.state.execLines = [
      `__openneko_event__${JSON.stringify({ type: "surface", messages: [{ version: "v1.0" }] })}\n`,
      `__openneko_agent_result__${JSON.stringify({ status: "completed", finalText: "", rawText: "" })}\n`,
    ];
    const events: AgentEvent[] = [];
    const runCore = makeSandboxRunCore({
        agentImage: "ghcr.io/open-neko/agent:test",
      onLog: () => {},
    });

    const result = await runCore(
      fakeInput(async (event) => void events.push(event)),
    );

    expect(result.status).toBe("completed");
    expect(events.filter((event) => event.type === "surface")).toEqual([
      expect.objectContaining({ type: "surface" }),
    ]);
  });

  it("replaces an orphaned sandbox when a durable retry collides on the run name", async () => {
    h.state.collideOnNextCreate = true;
    const runCore = makeSandboxRunCore({
        agentImage: "ghcr.io/open-neko/agent:test",
      onLog: () => {},
    });

    await runCore(fakeInput(async () => {}));

    const lifecycle = h.calls
      .map((call) =>
        call.args.find((arg) =>
          ["create", "exec", "download", "delete"].includes(arg),
        ),
      )
      .filter(Boolean);
    expect(lifecycle).toEqual([
      "create",
      "delete",
      "create",
      "exec",
      "download",
      "delete",
    ]);
  });

  it("aborts the live exec and deletes the whole sandbox without downloading artifacts", async () => {
    h.state.holdExec = true;
    const controller = new AbortController();
    const runCore = makeSandboxRunCore({
        agentImage: "ghcr.io/open-neko/agent:test",
      onLog: () => {},
    });

    const pending = runCore({
      ...fakeInput(async () => {}),
      signal: controller.signal,
    });
    while (!h.calls.some((call) => call.args.includes("exec"))) {
      await new Promise((resolve) => setTimeout(resolve, 0));
    }
    controller.abort();

    await expect(pending).rejects.toMatchObject({ name: "AbortError" });
    expect(h.calls.some((call) => call.args.includes("delete"))).toBe(true);
    expect(h.calls.some((call) => call.args.includes("download"))).toBe(false);
  });

  it("uses one OpenShell sandbox for a native-delegation-capable backend", async () => {
    const runCore = makeSandboxRunCore({
        agentImage: "ghcr.io/open-neko/agent:test",
      onLog: () => {},
    });

    await runCore(
      fakeInput(async () => {}, {
        id: "ax",
        capabilities: {
          mcpTools: false,
          nativeDelegation: "ax-worker",
        },
      } as RunAgentBackendInput["backend"]),
    );

    const calls = h.calls.map((c) => c.args);
    expect(calls.filter((args) => args.includes("create"))).toHaveLength(1);
    expect(calls.filter((args) => args.includes("exec"))).toHaveLength(1);
    expect(calls.filter((args) => args.includes("delete"))).toHaveLength(1);
  });

  it("serializes workflow context into the sandbox job", async () => {
    const runCore = makeSandboxWorkflowRunCore({
      agentImage: "ghcr.io/open-neko/agent:test",
      onLog: () => {},
    });

    await runCore({ ...fakeWorkflowInput(async () => {}), timeoutMs: 1_800_000 });

    const job = jobCapture.jobs.at(-1);
    expect(job).toMatchObject({
      kind: "workflow",
      orgId: "org-1",
      threadId: "thr-1",
      runId: "run-1",
      workflowRunId: "workflow-run-1",
      mode: "headless",
      triggeredByObservationId: "obs-1",
      networkHosts: [],
      backendId: "ax",
      message: "begin",
      agentRun: { timeoutMs: 1_800_000 },
    });
    const execArgs = h.calls.find((call) => call.args.includes("exec"))?.args ?? [];
    expect(execArgs[execArgs.indexOf("--timeout") + 1]).toBe("1920");
    expect(job).not.toHaveProperty("model");
    expect(job).not.toHaveProperty("threadState");
    expect(job).not.toHaveProperty("pluginActions");
    expect(h.calls.filter((c) => c.args.includes("create"))).toHaveLength(1);
    expect(h.calls.filter((c) => c.args.includes("exec"))).toHaveLength(1);
    expect(h.calls.filter((c) => c.args.includes("delete"))).toHaveLength(1);
  });

  it("carries script steps, input, and the run budget into the sandbox job", async () => {
    const runCore = makeSandboxWorkflowRunCore({
      agentImage: "ghcr.io/open-neko/agent:test",
      onLog: () => {},
    });
    const steps = [
      { id: "plan", description: "agent step" },
      {
        id: "union",
        description: "build the union",
        script: { skill: "daily-lead-union", command: ["python3", "run.py"], timeoutSeconds: 600 },
      },
    ];

    await runCore({
      ...fakeWorkflowInput(async () => {}),
      timeoutMs: 900_000,
      steps,
      input: { date: "2026-09-29" },
      reasoningEffort: "medium",
      maxToolIterations: 150,
      maxContinuations: 2,
    });

    expect(jobCapture.jobs.at(-1)).toMatchObject({
      steps,
      input: { date: "2026-09-29" },
      maxContinuations: 2,
      agentRun: { timeoutMs: 900_000, reasoningEffort: "medium", maxToolIterations: 150 },
    });
    const execArgs = h.calls.find((call) => call.args.includes("exec"))?.args ?? [];
    // 600s of steps + 3 turns of 900s + 120s margin.
    expect(execArgs[execArgs.indexOf("--timeout") + 1]).toBe("3420");
  });

  it("sizes the exec budget for steps and continuations", () => {
    const input = fakeWorkflowInput(async () => {});
    expect(workflowExecBudgetMs({ ...input, timeoutMs: 60_000 })).toBe(60_000);
    expect(
      workflowExecBudgetMs({
        ...input,
        timeoutMs: 60_000,
        maxContinuations: 1,
        steps: [{ id: "s", description: "d", script: { skill: "x", command: ["true"] } }],
      }),
    ).toBe(1_200_000 + 120_000);
  });

  it("pins Ax model and workflow egress to the harness binary and passes its route", async () => {
    const runCore = makeSandboxWorkflowRunCore({
      agentImage: "ghcr.io/open-neko/agent:test",
      modelHosts: [{ host: "api.anthropic.com" }],
      onLog: () => {},
    });
    const ax = { route: { provider: "anthropic", model: "claude-sonnet-5-5", keyEnv: "ANTHROPIC_API_KEY" } };
    const input = fakeWorkflowInput(async () => {}, {
      id: "ax",
      ax,
      capabilities: { mcpTools: true, sessionResume: false },
      run: async () => ({ finalText: "", status: "completed" }),
    } as unknown as RunWorkflowAgentBackendInput["backend"]);
    input.networkHosts = ["helpx.adobe.com"];

    await runCore(input);

    const policies = Object.values(
      (jobCapture.policies.at(-1)?.network_policies ?? {}) as Record<
        string,
        { binaries: Array<{ path: string }>; endpoints: Array<{ host: string }> }
      >,
    );
    const hosts = policies
      .filter((policy) => policy.binaries[0]?.path === "/usr/local/bin/ax-harness")
      .flatMap((policy) => policy.endpoints.map((endpoint) => endpoint.host));
    expect(hosts.sort()).toEqual(["api.anthropic.com", "helpx.adobe.com"]);
    expect(jobCapture.jobs.at(-1)).toMatchObject({ backendId: "ax", ax });
  });

  it("adds pack-declared workflow hosts to the OpenShell policy", async () => {
    const runCore = makeSandboxWorkflowRunCore({
      agentImage: "ghcr.io/open-neko/agent:test",
      onLog: () => {},
    });
    const input = fakeWorkflowInput(async () => {});
    input.networkHosts = ["helpx.adobe.com", "experienceleague.adobe.com"];

    await runCore(input);

    const policies = Object.values(
      (jobCapture.policies.at(-1)?.network_policies ?? {}) as Record<
        string,
        { binaries: Array<{ path: string }>; endpoints: Array<{ host: string }> }
      >,
    );
    const workflowPolicy = policies.find(
      (policy) => policy.binaries[0]?.path === "/usr/local/bin/ax-harness",
    );
    expect(workflowPolicy?.endpoints.map((endpoint) => endpoint.host)).toEqual([
      "experienceleague.adobe.com",
      "helpx.adobe.com",
    ]);
  });

  it("does not serialize GraphJin credentials into workflow sandbox jobs", async () => {
    const orgRoot = await mkdtemp(join(tmpdir(), "ws-"));
    const workspace = fullWorkspace(orgRoot);
    const clientDir = join(workspace.runRoot, "gj-auth", "graphjin");
    await mkdir(clientDir, { recursive: true });
    await writeFile(
      join(clientDir, "client.json"),
      JSON.stringify({
        server: "http://localhost:8080/api/v1/mcp",
        token: "run-token",
        subject: "service",
      }),
    );
    const runCore = makeSandboxWorkflowRunCore({
      agentImage: "ghcr.io/open-neko/agent:test",
      onLog: () => {},
    });

    await runCore(fakeWorkflowInput(async () => {}, undefined, workspace));

    expect(jobCapture.jobs.at(-1)).not.toHaveProperty("graphjinClientConfig");
    expect(jobCapture.jobs.at(-1)).not.toHaveProperty("graphjinServerUrl");
  });

  it("gives model-only jobs no GraphJin capability or artifact persistence", async () => {
    const orgRoot = await mkdtemp(join(tmpdir(), "job-ws-"));
    const workspace = fullWorkspace(orgRoot);
    await mkdir(workspace.artifactRoot, { recursive: true });
    const runCore = makeSandboxJobRunCore({
      agentImage: "ghcr.io/open-neko/agent:test",
      onLog: () => {},
    });

    await runCore(fakeJobInput(workspace));

    expect(jobCapture.jobs.at(-1)).toMatchObject({
      kind: "agent-job",
      agentAccess: {},
      agentRun: {
        timeoutMs: 45_000,
        retries: 0,
        tag: "profile-job",
      },
    });
    expect(jobCapture.jobs.at(-1)).not.toHaveProperty("graphjinServerUrl");
    expect(jobCapture.jobs.at(-1)).not.toHaveProperty("graphjinClientConfig");
    expect(h.calls.some((c) => c.args.includes("download"))).toBe(false);
  });

  it("gives GraphJin jobs a broker capability without source egress or tokens", async () => {
    const orgRoot = await mkdtemp(join(tmpdir(), "job-ws-"));
    const workspace = fullWorkspace(orgRoot);
    const clientDir = join(workspace.runRoot, "gj-auth", "graphjin");
    await mkdir(clientDir, { recursive: true });
    await writeFile(
      join(clientDir, "client.json"),
      JSON.stringify({
        server: "http://localhost:8080/api/v1/mcp",
        token: "ephemeral-job-token",
        subject: "service",
      }),
    );
    const runCore = makeSandboxJobRunCore({
      agentImage: "ghcr.io/open-neko/agent:test",
      onLog: () => {},
    });

    await runCore(fakeJobInput(workspace, { graphjinRead: true }));

    const job = jobCapture.jobs.at(-1);
    expect(job).toMatchObject({
      kind: "agent-job",
      agentAccess: { graphjinRead: true },
    });
    expect(job).not.toHaveProperty("graphjinWriteGrants");
    expect(job).not.toHaveProperty("graphjinServerUrl");
    expect(job).not.toHaveProperty("graphjinClientConfig");

    await runCore(fakeJobInput(workspace, { graphjinAgent: true }));
    expect(jobCapture.jobs.at(-1)).toMatchObject({
      kind: "agent-job",
      agentAccess: { graphjinAgent: true },
    });
  });

  it("scopes broker egress to node, injects url+token, and releases on finish", async () => {
    const released: string[] = [];
    const runCore = makeSandboxRunCore({
        agentImage: "ghcr.io/open-neko/agent:test",
      brokerUrl: "http://host.openshell.internal:4199",
      brokerTokenFor: ({ runId, orgId }) => `tok-${orgId}-${runId}`,
      brokerRelease: (runId) => released.push(runId),
      onLog: () => {},
    });

    await runCore(fakeInput(async () => {}));

    // the creation policy opens the broker host:port for the node binary only:
    const policies = Object.values(
      (jobCapture.policies.at(-1)?.network_policies ?? {}) as Record<
        string,
        { binaries: Array<{ path: string }>; endpoints: Array<{ host: string; port: number }> }
      >,
    );
    const nodePolicy = policies.find(
      (policy) => policy.binaries[0]?.path === "/usr/local/bin/node",
    );
    expect(nodePolicy?.endpoints).toContainEqual(
      expect.objectContaining({ host: "host.openshell.internal", port: 4199 }),
    );
    // the box gets the broker url + a run-bound bearer token — never a raw secret:
    const execCall = h.calls.find((c) => c.args.includes("exec"));
    expect(execCall?.args.join(" ")).toContain(
      "OPENNEKO_BROKER_URL='http://host.openshell.internal:4199'",
    );
    expect(execCall?.args.join(" ")).toContain("OPENNEKO_BROKER_TOKEN='tok-org-1-run-1'");
    // the token is dropped when the run ends:
    expect(released).toEqual(["run-1"]);
  });

  it("omits broker env when no broker is wired", async () => {
    const runCore = makeSandboxRunCore({
        agentImage: "ghcr.io/open-neko/agent:test",
      onLog: () => {},
    });
    await runCore(fakeInput(async () => {}));
    const execCall = h.calls.find((c) => c.args.includes("exec"));
    expect(execCall?.args.join(" ")).not.toContain("OPENNEKO_BROKER_URL");
    expect(execCall?.args.join(" ")).not.toContain("OPENNEKO_BROKER_TOKEN");
  });

  it("creates a live sandbox before its single workspace upload and never ships a key", async () => {
    const runCore = makeSandboxRunCore({
      agentImage: "ghcr.io/open-neko/agent:test",
      keyAliases: [{ from: "api_key", to: "GEMINI_API_KEY" }],
      onLog: () => {},
    });
    await runCore(fakeInput(async () => {}));

    // OpenShell 0.1.2 requires a live sandbox before workspace upload.
    expect(h.calls.filter((c) => c.args.includes("upload"))).toHaveLength(1);
    const create = h.calls.find((c) => c.args.includes("create"));
    expect(create?.args).not.toContain("--upload");
    expect(create?.args).toContain("--detach");
    expect(create?.args.slice(-2)).toEqual(["sleep", "infinity"]);
    expect(create?.args[create.args.indexOf("--name") + 1]).toMatch(/^neko-w-[0-9a-f]{12}$/);
    expect(
      JSON.stringify({
        calls: h.calls,
        jobs: jobCapture.jobs,
        policies: jobCapture.policies,
      }),
    ).not.toContain("REAL_SECRET");
  });
});

describe("ensureOpenShellProvider", () => {
  beforeEach(() => {
    h.calls.length = 0;
    h.state.failProviderReconcile = false;
    h.state.legacyProvider = false;
    h.state.missingProvider = false;
    h.state.providerNames = [];
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("leaves an existing versioned provider alone, so runs in flight keep their credential", async () => {
    h.state.providerNames = ["org-x-aaaa1111"];
    await ensureOpenShellProvider({ providerName: "org-x-aaaa1111", apiKey: "SECRET-KEY", family: "org-x" });
    const lines = h.calls.map((c) => c.args.join(" ")).filter((l) => !l.includes("profile import"));
    expect(lines).toEqual(["provider list --names"]);
  });

  it("creates a new versioned provider and deletes the replaced ones only after the delay", async () => {
    vi.useFakeTimers();
    h.state.providerNames = ["org-x", "org-x-aaaa1111", "org-y-cccc3333"];
    await ensureOpenShellProvider({ providerName: "org-x-bbbb2222", apiKey: "SECRET-KEY", family: "org-x", staleDelayMs: 1_000 });
    const lines = () => h.calls.map((c) => c.args.join(" ")).filter((l) => !l.includes("profile import"));
    expect(lines()).toEqual([
      "provider list --names",
      "provider create --name org-x-bbbb2222 --type openneko-agent --credential MODEL_API_KEY=SECRET-KEY",
    ]);
    await vi.advanceTimersByTimeAsync(1_000);
    expect(lines().slice(2).sort()).toEqual(["provider delete org-x", "provider delete org-x-aaaa1111"]);
  });

  it("recreates a provider whose legacy credential key OpenShell 0.1.2 cannot update", async () => {
    h.state.legacyProvider = true;
    await ensureOpenShellProvider({ providerName: "org-x", apiKey: "SECRET-KEY" });
    const lines = h.calls.map((c) => c.args.join(" ")).filter((l) => !l.includes("profile import"));
    expect(lines).toEqual([
      "provider update org-x --credential MODEL_API_KEY=SECRET-KEY",
      "provider delete org-x",
      "provider create --name org-x --type openneko-agent --credential MODEL_API_KEY=SECRET-KEY",
    ]);
  });

  it("refreshes an existing provider's key without a create attempt", async () => {
    await ensureOpenShellProvider({ providerName: "org-x", apiKey: "SECRET-KEY" });
    const lines = h.calls.map((c) => c.args.join(" ")).filter((l) => !l.includes("profile import"));
    expect(lines).toEqual(["provider update org-x --credential MODEL_API_KEY=SECRET-KEY"]);
  });

  it("registers the generic profile and creates the provider with the key", async () => {
    h.state.missingProvider = true;
    await ensureOpenShellProvider({ providerName: "org-x", apiKey: "SECRET-KEY" });
    const lines = h.calls.map((c) => c.args.join(" "));
    // generic profile imported (idempotent):
    expect(lines.some((l) => l.startsWith("provider profile import --file") && l.endsWith(".yaml"))).toBe(true);
    // provider created from the generic type, holding the key:
    const create = lines.find((l) => l.startsWith("provider create"));
    expect(create).toContain("--name org-x");
    expect(create).toContain("--type openneko-agent");
    expect(create).toContain("--credential MODEL_API_KEY=SECRET-KEY");
  });

  it("reports the update rejection even if profile import also failed, without leaking the key", async () => {
    h.state.failProviderReconcile = true;
    await expect(ensureOpenShellProvider({ providerName: "org-x", apiKey: "SECRET-KEY" }))
      .rejects.toThrow(/update: .*INVALID_ARGUMENT update rejected/);
    await expect(ensureOpenShellProvider({ providerName: "org-x", apiKey: "SECRET-KEY" }))
      .rejects.not.toThrow(/SECRET-KEY/);
    expect(h.calls.some((call) => call.args.includes("update"))).toBe(true);
  });
});

describe("deleteOpenShellProvider", () => {
  beforeEach(() => {
    h.calls.length = 0;
  });

  it("deletes a provider family, versioned names included", async () => {
    h.state.providerNames = ["openneko-agent-eval-org", "openneko-agent-eval-org-aaaa1111", "openneko-agent-other"];
    await deleteOpenShellProvider({ providerName: "openneko-agent-eval-org", family: true });
    expect(h.calls.map((c) => c.args.join(" ")).filter((l) => l.includes("delete"))).toEqual([
      "provider delete openneko-agent-eval-org",
      "provider delete openneko-agent-eval-org-aaaa1111",
    ]);
    h.state.providerNames = [];
  });

  it("deletes only the explicitly named provider", async () => {
    await deleteOpenShellProvider({
      providerName: "openneko-agent-eval-org",
      gatewayName: "openneko",
    });
    expect(h.calls).toEqual([
      {
        args: [
          "--gateway",
          "openneko",
          "provider",
          "delete",
          "openneko-agent-eval-org",
        ],
      },
    ]);
  });
});

describe("verifyOpenShellGateway", () => {
  beforeEach(() => {
    h.calls.length = 0;
  });

  it("uses a read-only gateway RPC with the configured registration", async () => {
    await verifyOpenShellGateway({ gatewayName: "openneko" });
    expect(h.calls).toEqual([
      { args: ["--gateway", "openneko", "provider", "list", "--names"] },
    ]);
  });
});
