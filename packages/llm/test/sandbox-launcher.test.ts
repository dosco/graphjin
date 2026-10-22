// The agent run launcher: stage the workspace, run the agent runner as a
// child process with a minimal environment, stream its events, copy its
// artifacts back and remove the stage. The runner here is a small script that
// stands in for apps/worker/src/agent-sandbox/entry.ts.

import {
  access,
  cp,
  mkdir,
  mkdtemp,
  readFile,
  readdir,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentEvent, AgentWorkspace } from "../src/agent-backend";
import type { RunAgentBackendInput } from "../src/work/agent-core";
import { KNOWLEDGE_FILES, refreshKnowledgeSnapshot } from "../src/knowledge-cache";
import type { RunWorkflowAgentBackendInput } from "../src/workflows/agent-core";

vi.mock("../src/work/personas", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../src/work/personas")>()),
  getWorkRunActor: async () => {
    throw new Error("no database");
  },
}));

const {
  makeSandboxRunCore,
  makeSandboxWorkflowRunCore,
  reapStrandedRuns,
  sandboxLauncherOptionsFromEnv,
  stageSandboxWorkspace,
  workflowExecBudgetMs,
} = await import("../src/work/sandbox-launcher");

const RUNNER = `
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
const job = JSON.parse(readFileSync(process.env.OPENNEKO_RUN_JOB_FILE, "utf8"));
writeFileSync(process.env.CAPTURE, JSON.stringify({ job, env: process.env, pid: process.pid }));
const say = (marker, value) => process.stdout.write("\\n" + marker + JSON.stringify(value) + "\\n");
const mode = process.env.FAKE_MODE ?? "answer";
if (mode === "hold") {
  setInterval(() => {}, 1000);
} else {
  mkdirSync(job.workspace.artifactRoot, { recursive: true });
  writeFileSync(join(job.workspace.artifactRoot, "report.txt"), "made in the run");
  if (mode === "answer") say("__openneko_event__", { type: "message", role: "assistant", content: "hi" });
  say("__openneko_agent_result__", mode === "empty"
    ? { status: "completed", finalText: "" }
    : { status: "completed", finalText: "hi there", threadState: { t: 1 } });
}
`;

let dir: string;
let runner: string;
let runs: string;

beforeAll(async () => {
  dir = await mkdtemp(join(tmpdir(), "agent-launcher-test-"));
  runner = join(dir, "runner.mjs");
  runs = join(dir, "runs");
  await writeFile(runner, RUNNER);
  vi.stubEnv("OPENNEKO_AGENT_ENTRY", runner);
  vi.stubEnv("OPENNEKO_AGENT_RUNS_DIR", runs);
  vi.stubEnv("OPENNEKO_SERVE_SOCKET", "/run/openneko/serve.sock");
  vi.stubEnv("NEKO_PG_PASSWORD", "must-not-reach-the-runner");
});

afterAll(async () => {
  vi.unstubAllEnvs();
  await rm(dir, { recursive: true, force: true });
});

async function hostWorkspace(): Promise<AgentWorkspace> {
  const workspace = fullWorkspace(await mkdtemp(join(dir, "org-")));
  await Promise.all(Object.values(workspace).map((d) => mkdir(d, { recursive: true })));
  return workspace;
}

function input(
  workspace: AgentWorkspace,
  emit: (e: AgentEvent) => Promise<void> = async () => {},
  signal?: AbortSignal,
): RunAgentBackendInput {
  return {
    backend: { id: "ax", capabilities: { mcpTools: false }, ax: { route: { provider: "anthropic", model: "m", keyEnv: "ANTHROPIC_API_KEY" } } } as unknown as RunAgentBackendInput["backend"],
    prompt: `Work in ${workspace.orgRoot}/runs/run-1`,
    userMessage: "hello",
    orgId: "org-1",
    threadId: "thr-1",
    runId: "run-1",
    workspace,
    threadState: undefined,
    pluginActions: [],
    packActions: [{ kind: "slack.message", description: "Post", pack: "slack" }] as unknown as RunAgentBackendInput["packActions"],
    emit,
    ...(signal ? { signal } : {}),
  };
}

type Capture = { job: Record<string, unknown> & { workspace: AgentWorkspace; prompt: string }; env: Record<string, string>; pid: number };

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
        join(process.cwd(), "assets", "builtin-skills", "pdf"),
        join(workspace.skillsRoot, "pdf"),
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
          join(workspace.skillsRoot, "pdf", "SKILL.md"),
          "---\nname: pdf\ndescription: stale system skill\n---\n",
        ),
      ]);

      const staged = await stageSandboxWorkspace(workspace, stage, {
        requiredSkillNames: ["pdf"],
      });
      expect(staged.skillOverrides).toEqual(["pdf", "quarterly-review"]);
      await expect(
        access(join(staged.workspace.skillsRoot, "pdf", "SKILL.md")),
      ).resolves.toBeUndefined();
      expect(
        await readFile(join(staged.workspace.skillsRoot, "pdf", "SKILL.md"), "utf8"),
      ).toBe(await readFile(join(process.cwd(), "assets", "builtin-skills", "pdf", "SKILL.md"), "utf8"));
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
  let capture: string;
  beforeEach(() => {
    capture = join(dir, `capture-${Math.random().toString(36).slice(2)}.json`);
  });
  const opts = (mode = "answer") => ({
    modelKeys: { MODEL_API_KEY: "org-key", ANTHROPIC_API_KEY: "org-key" },
    env: { CAPTURE: capture, FAKE_MODE: mode },
    brokerUrl: "http://127.0.0.1:4200",
    brokerTokenFor: () => "run-token",
    brokerRelease: vi.fn(),
    onLog: () => {},
  });

  it("runs the runner on a staged workspace, streams events, copies artifacts back and removes the stage", async () => {
    const workspace = await hostWorkspace();
    const events: AgentEvent[] = [];
    const options = opts();
    const result = await makeSandboxRunCore(options)(input(workspace, async (e) => void events.push(e)));

    expect(result).toEqual({ status: "completed", finalText: "hi there", threadState: { t: 1 } });
    expect(events.filter((e) => e.type === "message")).toEqual([expect.objectContaining({ content: "hi" })]);
    expect(events.filter((e) => e.type === "status").at(-1)).toEqual({ type: "status", message: "Agent is working…" });
    expect(await readFile(join(workspace.artifactRoot, "report.txt"), "utf8")).toBe("made in the run");
    expect(options.brokerRelease).toHaveBeenCalledWith("run-1");

    const { job, env } = JSON.parse(await readFile(capture, "utf8")) as Capture;
    // The run works on a stage under the runs root, and the prompt follows it.
    expect(job.workspace.orgRoot.startsWith(runs)).toBe(true);
    expect(job.prompt).toBe(`Work in ${job.workspace.orgRoot}/runs/run-1`);
    expect(job).toMatchObject({ kind: "work", orgId: "org-1", threadId: "thr-1", runId: "run-1", backendId: "ax" });
    expect(job.ax).toEqual({ route: { provider: "anthropic", model: "m", keyEnv: "ANTHROPIC_API_KEY" } });
    expect(job.packActions).toEqual([{ kind: "slack.message", description: "Post", pack: "slack" }]);
    // The runner gets the broker binding, serve's socket and the org's key, and
    // nothing else of this host's environment.
    expect(env).toMatchObject({
      OPENNEKO_BROKER_URL: "http://127.0.0.1:4200",
      OPENNEKO_BROKER_TOKEN: "run-token",
      OPENNEKO_SERVE_SOCKET: "/run/openneko/serve.sock",
      ANTHROPIC_API_KEY: "org-key",
      MODEL_API_KEY: "org-key",
    });
    expect(env.NEKO_PG_PASSWORD).toBeUndefined();
    // The org's terminal setting rides with the run; on by default.
    expect(env.OPENNEKO_AGENT_TERMINAL).toBe("on");
    // The stage is gone once the run ends.
    await expect(access(job.workspace.orgRoot)).rejects.toThrow();
  });

  it("rejects an empty completed result with no answer event", async () => {
    const workspace = await hostWorkspace();
    await expect(makeSandboxRunCore(opts("empty"))(input(workspace))).rejects.toThrow(
      /completed without assistant output/,
    );
  });

  it("kills the runner's process group and removes the stage when the run is aborted", async () => {
    const workspace = await hostWorkspace();
    const controller = new AbortController();
    const running = makeSandboxRunCore(opts("hold"))(input(workspace, async () => {}, controller.signal));
    let captured: Capture | undefined;
    for (let i = 0; i < 200 && !captured; i++) {
      await new Promise((r) => setTimeout(r, 25));
      captured = await readFile(capture, "utf8").then((t) => JSON.parse(t) as Capture).catch(() => undefined);
    }
    expect(captured).toBeDefined();
    // Serve and the sandbox's run user share the stage through the group;
    // others get nothing.
    const mode = (await stat(captured!.job.workspace.skillsRoot)).mode & 0o077;
    expect(mode).toBe(0o070);
    controller.abort();
    await expect(running).rejects.toThrow(/aborted/);
    expect(() => process.kill(captured!.pid, 0)).toThrow();
    await expect(access(captured!.job.workspace.orgRoot)).rejects.toThrow();
  });

  it("carries workflow context and the run budget into the job", async () => {
    const workspace = await hostWorkspace();
    const workflow: RunWorkflowAgentBackendInput = {
      ...(input(workspace) as unknown as RunWorkflowAgentBackendInput),
      workflowRunId: "workflow-run-1",
      mode: "headless",
      networkHosts: ["api.example.com"],
      triggeredByObservationId: "obs-1",
      steps: [{ id: "s1", description: "Step one" }],
      input: { region: "west" },
      maxContinuations: 2,
      timeoutMs: 60_000,
    };
    await makeSandboxWorkflowRunCore(opts())(workflow);
    const { job } = JSON.parse(await readFile(capture, "utf8")) as Capture;
    expect(job).toMatchObject({
      kind: "workflow",
      workflowRunId: "workflow-run-1",
      networkHosts: ["api.example.com"],
      triggeredByObservationId: "obs-1",
      steps: [{ id: "s1", description: "Step one" }],
      input: { region: "west" },
      maxContinuations: 2,
      agentRun: { timeoutMs: 60_000 },
    });
  });

  it("sizes the exec budget for steps and continuations", () => {
    expect(workflowExecBudgetMs({
      timeoutMs: 1_000,
      maxContinuations: 2,
      steps: [{ id: "s", description: "d", script: { path: "x.py", timeoutSeconds: 5 } }],
    } as unknown as RunWorkflowAgentBackendInput)).toBe(5_000 + 3_000);
  });
});

describe("reapStrandedRuns", () => {
  it("deletes only this owner's run folders from earlier boots", async () => {
    const root = await mkdtemp(join(dir, "reap-"));
    await Promise.all([
      mkdir(join(root, "openneko.old-boot.abc")),
      mkdir(join(root, "openneko.current.def")),
      mkdir(join(root, "other-host.old-boot.ghi")),
    ]);
    expect(await reapStrandedRuns(root, "openneko", "current")).toEqual(["openneko.old-boot.abc"]);
    expect((await readdir(root)).sort()).toEqual(["openneko.current.def", "other-host.old-boot.ghi"]);
  });
});

describe("sandboxLauncherOptionsFromEnv", () => {
  it("passes a key from the environment under its own name and MODEL_API_KEY", () => {
    vi.stubEnv("OPENNEKO_AGENT_MODEL_KEY_ENV", "GEMINI_API_KEY");
    vi.stubEnv("GEMINI_API_KEY", "dev-key");
    try {
      expect(sandboxLauncherOptionsFromEnv().modelKeys).toEqual({ MODEL_API_KEY: "dev-key", GEMINI_API_KEY: "dev-key" });
    } finally {
      vi.stubEnv("OPENNEKO_AGENT_MODEL_KEY_ENV", "");
      vi.stubEnv("GEMINI_API_KEY", "");
    }
  });
});

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

