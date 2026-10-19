// Model calls outside agent turns go through openneko serve when it runs.
// With OPENNEKO_TEST_BINARY naming a built openneko binary the test talks to
// a real serve; otherwise to a stand-in that speaks the same frames.

import { spawn, type ChildProcess } from "node:child_process";
import { existsSync } from "node:fs";
import { mkdtemp, rm } from "node:fs/promises";
import { createServer as createHttpServer, type Server as HttpServer } from "node:http";
import { createServer, type Server } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createInterface } from "node:readline";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { gatewayFetch } from "../src/model-gateway";
import { testPrimaryProvider } from "../src/llm";

const binary = process.env.OPENNEKO_TEST_BINARY;

/** Serve's gateway, as gateway.go speaks it. */
function standInServe(socket: string, seen: Array<Record<string, unknown>>): Promise<Server> {
  const server = createServer((conn) => {
    const lines = createInterface({ input: conn });
    lines.once("line", async (line) => {
      const frame = JSON.parse(line) as { type: string; request: { url: string; method: string; headers: Record<string, string>; body: string } };
      seen.push(frame);
      const write = (f: unknown) => conn.write(`${JSON.stringify(f)}\n`);
      try {
        const { url, method, headers, body } = frame.request;
        const res = await fetch(url, { method, headers, body: body || undefined });
        write({ type: "response", status: res.status, headers: Object.fromEntries(res.headers) });
        for await (const chunk of res.body as unknown as AsyncIterable<Uint8Array>) {
          write({ type: "chunk", data: Buffer.from(chunk).toString("base64") });
        }
        write({ type: "exit", code: 0 });
      } catch (error) {
        write({ type: "exit", code: 1, error: String(error) });
      }
      conn.end();
    });
  });
  return new Promise((done) => server.listen(socket, () => done(server)));
}

describe("model gateway", () => {
  let dir: string;
  let socket: string;
  let model: HttpServer;
  let modelUrl: string;
  let serve: Server | undefined;
  let child: ChildProcess | undefined;
  const frames: Array<Record<string, unknown>> = [];
  const modelRequests: Array<{ path: string; auth?: string; body: string }> = [];

  beforeAll(async () => {
    dir = await mkdtemp(join(tmpdir(), "neko-gateway-"));
    socket = join(dir, "serve.sock");
    model = createHttpServer((req, res) => {
      let body = "";
      req.on("data", (c) => (body += c));
      req.on("end", () => {
        modelRequests.push({ path: req.url ?? "", auth: req.headers.authorization, body });
        if (req.url === "/slow") {
          res.writeHead(200, { "content-type": "text/plain" });
          res.write("first ");
          setTimeout(() => res.end("second"), 50);
          return;
        }
        if (req.url === "/fail") {
          res.writeHead(503, { "content-type": "application/json" });
          res.end(JSON.stringify({ error: "overloaded" }));
          return;
        }
        res.setHeader("content-type", "application/json");
        res.end(JSON.stringify({
          id: "c1", object: "chat.completion", created: 0, model: "fixture",
          choices: [{ index: 0, message: { role: "assistant", content: "READY" }, finish_reason: "stop" }],
          usage: { prompt_tokens: 3, completion_tokens: 1, total_tokens: 4 },
        }));
      });
    });
    await new Promise<void>((done) => model.listen(0, "127.0.0.1", done));
    modelUrl = `http://127.0.0.1:${(model.address() as { port: number }).port}`;

    if (binary && existsSync(binary)) {
      child = spawn(binary, ["serve", "--socket", socket], { stdio: "ignore", env: { PATH: process.env.PATH ?? "" } });
      for (let i = 0; i < 200 && !existsSync(socket); i++) await new Promise((r) => setTimeout(r, 25));
    } else {
      serve = await standInServe(socket, frames);
    }
  });

  afterAll(async () => {
    vi.unstubAllEnvs();
    child?.kill();
    await new Promise<void>((done) => (serve ? serve.close(() => done()) : done()));
    await new Promise<void>((done) => model.close(() => done()));
    await rm(dir, { recursive: true, force: true });
  });

  it("makes the request through serve and streams the response back", async () => {
    const res = await gatewayFetch(socket)(`${modelUrl}/slow`, {
      method: "POST",
      headers: { authorization: "Bearer org-key", "content-type": "application/json" },
      body: JSON.stringify({ q: 1 }),
    });
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toBe("text/plain");
    expect(await res.text()).toBe("first second");
    expect(modelRequests.at(-1)).toEqual({ path: "/slow", auth: "Bearer org-key", body: '{"q":1}' });
  });

  it("hands a provider's error status back as a response", async () => {
    const res = await gatewayFetch(socket)(`${modelUrl}/fail`);
    expect(res.status).toBe(503);
    expect(await res.json()).toEqual({ error: "overloaded" });
  });

  it("fails like fetch when serve cannot reach the provider or is down", async () => {
    await expect(gatewayFetch(socket)("http://127.0.0.1:1/v1")).rejects.toThrow(TypeError);
    await expect(gatewayFetch(join(dir, "missing.sock"))(`${modelUrl}/fail`)).rejects.toThrow(/serve unavailable/);
  });

  it("stops on abort", async () => {
    const controller = new AbortController();
    controller.abort(new Error("stopped"));
    await expect(gatewayFetch(socket)(`${modelUrl}/slow`, { signal: controller.signal })).rejects.toThrow("stopped");
  });

  it("sends the host's own model calls through serve when it runs", async () => {
    vi.stubEnv("OPENNEKO_SERVE_SOCKET", socket);
    const before = modelRequests.length;
    const result = await testPrimaryProvider(undefined, {
      scope: "primary", provider: "ollama", model: "fixture", enabled: true, config: { url: `${modelUrl}/v1` }, secrets: {},
    });
    expect(result).toMatchObject({ provider: "ollama", model: "fixture" });
    expect(modelRequests.length).toBe(before + 1);
    expect(modelRequests.at(-1)!.body).toContain("Reply with READY only.");
    if (serve) expect(frames.at(-1)).toMatchObject({ type: "fetch", request: { method: "POST" } });
  });
});
