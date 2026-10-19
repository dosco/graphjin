// The Ax backend running a turn in a real `openneko serve`: the model key
// travels out of band, the worker's tools cross serve's socket, and the
// answer comes back as harness events.

import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { createServer, type Server as HttpServer } from "node:http";
import { mkdir, mkdtemp, rm } from "node:fs/promises";
import { existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { CallToolRequestSchema, ListToolsRequestSchema } from "@modelcontextprotocol/sdk/types.js";
import { AxBackend } from "../src/agent-backends/ax";
import { registerServeToolBridge } from "../src/agent-backends/harness-turn";

function hasGo(): boolean {
  try {
    execFileSync("go", ["version"], { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
}

const binary = process.env.OPENNEKO_TEST_BINARY;
// Opt in: it builds the openneko binary unless OPENNEKO_TEST_BINARY names one.
const runnable = Boolean(binary) || (process.env.OPENNEKO_SERVE_IT === "1" && hasGo());

(runnable ? describe : describe.skip)("Ax backend in openneko serve", () => {
  let dir: string;
  let serve: ChildProcess;
  let model: HttpServer;
  let modelUrl: string;
  const keys: string[] = [];
  const toolLists: string[][] = [];

  beforeAll(async () => {
    dir = await mkdtemp(join(tmpdir(), "neko-serve-"));
    let bin = binary;
    if (!bin) {
      bin = join(dir, "openneko");
      execFileSync("go", ["build", "-o", bin, "./cmd/openneko"], {
        cwd: resolve(__dirname, "../../../apps/openneko"),
        stdio: "inherit",
      });
    }
    const responses = [
      `{"javascriptCode":"final('Find reference', {})"}`,
      `{"javascriptCode":"final('Report', {reference:'REF-42'});"}`,
      "Answer: REF-42",
    ];
    model = createServer((req, res) => {
      let body = "";
      req.on("data", (c) => (body += c));
      req.on("end", () => {
        keys.push(String(req.headers.authorization));
        const content = responses[Math.min(keys.length - 1, responses.length - 1)];
        res.setHeader("content-type", "application/json");
        res.end(JSON.stringify({ choices: [{ message: { role: "assistant", content }, finish_reason: "stop" }] }));
      });
    });
    await new Promise<void>((done) => model.listen(0, "127.0.0.1", done));
    modelUrl = `http://127.0.0.1:${(model.address() as { port: number }).port}`;

    const socket = join(dir, "serve.sock");
    serve = spawn(bin, ["serve", "--socket", socket], { stdio: ["ignore", "ignore", "pipe"], env: { PATH: process.env.PATH ?? "" } });
    for (let i = 0; i < 200 && !existsSync(socket); i++) await new Promise((r) => setTimeout(r, 25));
    if (!existsSync(socket)) throw new Error("openneko serve did not start");
    vi.stubEnv("OPENNEKO_SERVE_SOCKET", socket);
    vi.stubEnv("OPENAI_API_KEY", "org-stored-key");

    registerServeToolBridge(async (servers, env) => {
      const server = new Server({ name: "worker", version: "1" }, { capabilities: { tools: {} } });
      server.setRequestHandler(ListToolsRequestSchema, async () => {
        toolLists.push(servers);
        expect(env.OPENNEKO_MCP_ORG_ID).toBe("org-1");
        return { tools: [{ name: "memory_search", description: "Search memory", inputSchema: { type: "object" } }] };
      });
      server.setRequestHandler(CallToolRequestSchema, async () => ({ content: [] }));
      return server;
    });
  }, 300_000);

  afterAll(async () => {
    registerServeToolBridge(undefined);
    vi.unstubAllEnvs();
    serve?.kill("SIGTERM");
    await new Promise<void>((done) => (model ? model.close(() => done()) : done()));
    if (dir) await rm(dir, { recursive: true, force: true });
  });

  it("answers, with the key and tools crossing the socket", async () => {
    const backend = new AxBackend({
      route: { provider: "openai-compatible", model: "fixture", url: modelUrl, keyEnv: "OPENAI_API_KEY" },
    });
    const workspace = await mkdtemp(join(dir, "ws-"));
    await mkdir(join(workspace, "skills"), { recursive: true });
    const result = await backend.run({
      prompt: "Find reference",
      orgId: "org-1",
      tag: "run-serve-1",
      workspace: {
        orgRoot: workspace,
        skillsRoot: join(workspace, "skills"),
        memoryRoot: join(workspace, "memory"),
        knowledgeRoot: join(workspace, "knowledge"),
        uploadsRoot: join(workspace, "uploads"),
        runsRoot: join(workspace, "runs"),
        runRoot: join(workspace, "runs", "r1"),
        artifactRoot: join(workspace, "runs", "r1", "artifacts"),
        binRoot: join(workspace, "runs", "r1", "bin"),
      },
      mcpServers: { neko_memory: {} },
      mcpBridgeEnv: { OPENNEKO_MCP_ORG_ID: "org-1", OPENNEKO_MCP_THREAD_ID: "thread-1", OPENNEKO_MCP_RUN_ID: "run-serve-1" },
      nativeDelegation: "disabled",
    });
    expect(result.status, result.error).toBe("completed");
    expect(result.finalText).toContain("REF-42");
    expect(keys.length).toBeGreaterThan(0);
    expect(new Set(keys)).toEqual(new Set(["Bearer org-stored-key"]));
    expect(toolLists).toEqual([["neko_memory"]]);
  });
});
