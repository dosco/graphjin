/**
 * The agent runtime end to end, with no model provider: the launcher stages a
 * run and starts the bundled runner, the runner sends the turn to a real
 * `openneko serve`, and serve sends the terminal command to a real
 * `openneko sandbox`, which runs it as the run's own user.
 *
 * Opt in: OPENNEKO_RUNTIME_E2E=1, as root (the sandbox switches users), with
 * OPENNEKO_TEST_BINARY naming a built openneko binary and
 * OPENNEKO_AGENT_ENTRY the bundled runner (Dockerfile agent-deploy stage).
 * The runs folder sits under /var/lib so run users can cross into it.
 */

import { spawn, type ChildProcess } from "node:child_process";
import { createServer, type Server } from "node:http";
import { existsSync } from "node:fs";
import { chmod, mkdir, mkdtemp, rm } from "node:fs/promises";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import type { AgentEvent } from "@neko/llm";
import { makeAgentBackend } from "@neko/llm/agent-runtime";
import { makeSandboxJobRunCore } from "@neko/llm/work/sandbox-launcher";

const binary = process.env.OPENNEKO_TEST_BINARY;
const entry = process.env.OPENNEKO_AGENT_ENTRY;
const runnable =
  process.env.OPENNEKO_RUNTIME_E2E === "1" && process.getuid?.() === 0 && Boolean(binary && entry && existsSync(binary) && existsSync(entry));

(runnable ? describe : describe.skip)("agent runtime chain", () => {
  let root: string;
  let model: Server;
  let modelUrl: string;
  const children: ChildProcess[] = [];

  beforeAll(async () => {
    await mkdir("/var/lib/openneko-e2e", { recursive: true });
    await chmod("/var/lib/openneko-e2e", 0o755);
    root = await mkdtemp("/var/lib/openneko-e2e/run-");
    await chmod(root, 0o755);
    const runs = join(root, "runs");
    const sockets = join(root, "sock");
    await mkdir(runs, { recursive: true });
    await mkdir(sockets, { recursive: true });

    // The actor runs one terminal command, then the responder answers. The
    // responder streams, as it does whenever a run has an event sink.
    const actor = [
      `{"javascriptCode":"final('Run the command',{});"}`,
      `{"javascriptCode":"const r=terminal({command:'id -u; env | grep -c API_KEY; mkdir -p \\"$ARTIFACT_DIR\\" && echo hello > \\"$ARTIFACT_DIR/report.txt\\" && echo wrote'}); console.log(JSON.stringify(r));"}`,
      `{"javascriptCode":"final('Report',{});"}`,
    ];
    let calls = 0;
    model = createServer((req, res) => {
      let body = "";
      req.on("data", (chunk) => (body += chunk));
      req.on("end", () => {
        const content = body.includes("Javascript Code") ? actor[Math.min(calls++, actor.length - 1)] : JSON.stringify({ answer: "done" });
        if (JSON.parse(body).stream) {
          res.setHeader("content-type", "text/event-stream");
          res.write(`data: ${JSON.stringify({ choices: [{ index: 0, delta: { role: "assistant", content } }] })}\n\n`);
          res.write(`data: ${JSON.stringify({ choices: [{ index: 0, delta: {}, finish_reason: "stop" }] })}\n\n`);
          res.end("data: [DONE]\n\n");
          return;
        }
        res.setHeader("content-type", "application/json");
        res.end(JSON.stringify({ choices: [{ message: { role: "assistant", content }, finish_reason: "stop" }] }));
      });
    });
    await new Promise<void>((done) => model.listen(0, "127.0.0.1", done));
    modelUrl = `http://127.0.0.1:${(model.address() as { port: number }).port}`;

    const sandboxSocket = join(sockets, "sandbox.sock");
    const serveSocket = join(sockets, "serve.sock");
    children.push(spawn(binary!, ["sandbox", "--socket", sandboxSocket], {
      stdio: "ignore",
      env: { PATH: process.env.PATH ?? "", OPENNEKO_SANDBOX_WORKSPACES: runs, OPENNEKO_SANDBOX_HOMES: join(root, "homes"), OPENNEKO_SANDBOX_GID: "0" },
    }));
    children.push(spawn(binary!, ["serve", "--socket", serveSocket], {
      stdio: "ignore",
      env: { PATH: process.env.PATH ?? "", OPENNEKO_SANDBOX_SOCKET: sandboxSocket },
    }));
    for (let i = 0; i < 200 && !(existsSync(serveSocket) && existsSync(sandboxSocket)); i++) {
      await new Promise((r) => setTimeout(r, 25));
    }
    process.env.OPENNEKO_SERVE_SOCKET = serveSocket;
    process.env.OPENNEKO_AGENT_RUNS_DIR = runs;
  }, 60_000);

  afterAll(async () => {
    for (const child of children) child.kill();
    await new Promise<void>((done) => (model ? model.close(() => done()) : done()));
    delete process.env.OPENNEKO_SERVE_SOCKET;
    delete process.env.OPENNEKO_AGENT_RUNS_DIR;
    if (root) await rm(root, { recursive: true, force: true });
  });

  it("runs a turn whose terminal command runs as the run's user with no key", async () => {
    const org = join(root, "host", "org-1");
    const workspace = {
      orgRoot: org,
      skillsRoot: join(org, "skills"),
      memoryRoot: join(org, "memory"),
      knowledgeRoot: join(org, "knowledge"),
      uploadsRoot: join(org, "uploads"),
      runsRoot: join(org, "runs"),
      threadUploadsRoot: join(org, "uploads", "t"),
      runRoot: join(org, "runs", "run-1"),
      artifactRoot: join(org, "runs", "run-1", "artifacts"),
      binRoot: join(org, "runs", "run-1", "bin"),
    };
    for (const dir of Object.values(workspace)) await mkdir(dir, { recursive: true });
    const backend = makeAgentBackend({
      id: "ax",
      ax: { route: { provider: "openai-compatible", model: "fixture", url: modelUrl, keyEnv: "OPENAI_API_KEY" } },
    });
    const events: AgentEvent[] = [];
    const result = await makeSandboxJobRunCore({
      modelKeys: { OPENAI_API_KEY: "org-key", MODEL_API_KEY: "org-key" },
      onLog: () => {},
    })({
      backend,
      orgId: "org-1",
      runId: "run-1",
      workspace,
      access: {},
      run: { prompt: "Run the command", tag: "run-1", nativeDelegation: "disabled" },
      emit: async (event) => void events.push(event),
    });

    expect(result).toMatchObject({ status: "completed", finalText: "done" });
    const terminal = events.find((event): event is Extract<AgentEvent, { type: "tool_end" }> => event.type === "tool_end");
    const output = String((terminal?.result as { output?: string } | undefined)?.output ?? "");
    const [uid, keys, wrote] = output.trim().split("\n");
    expect(Number(uid)).toBeGreaterThanOrEqual(20000);
    expect(keys).toBe("0");
    expect(wrote).toBe("wrote");
  }, 120_000);
});
