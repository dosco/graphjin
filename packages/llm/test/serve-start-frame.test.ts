// What the Ax backend sends openneko serve to start a turn: the run's model
// key out of band, and, when serve hosts GraphJin, the run's GraphJin token.

import { createServer, type Server } from "node:net";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createInterface } from "node:readline";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { AxBackend } from "../src/agent-backends/ax";

describe("serve start frame", () => {
  let dir: string;
  let server: Server;
  const starts: Array<Record<string, unknown>> = [];

  beforeAll(async () => {
    dir = await mkdtemp(join(tmpdir(), "neko-serve-frame-"));
    const socket = join(dir, "serve.sock");
    server = createServer((conn) => {
      const lines = createInterface({ input: conn });
      lines.once("line", (line) => {
        const start = JSON.parse(line) as Record<string, unknown> & { spec: { run_id: string } };
        starts.push(start);
        const event = { version: 1, run_id: start.spec.run_id, sequence: 1, type: "run.finished", result: { status: "completed", kind: "answer", answer: "ok" } };
        conn.write(`${JSON.stringify({ type: "event", event })}\n${JSON.stringify({ type: "exit", code: 0 })}\n`);
      });
    });
    await new Promise<void>((done) => server.listen(socket, done));
    vi.stubEnv("OPENNEKO_SERVE_SOCKET", socket);
    vi.stubEnv("ANTHROPIC_API_KEY", "org-key");
  });

  afterAll(async () => {
    vi.unstubAllEnvs();
    await new Promise<void>((done) => server.close(() => done()));
    await rm(dir, { recursive: true, force: true });
  });

  it("carries the run's GraphJin token only when serve hosts GraphJin", async () => {
    const backend = new AxBackend({ route: { provider: "anthropic", model: "m", keyEnv: "ANTHROPIC_API_KEY" } });
    const workspace = {
      orgRoot: dir, skillsRoot: join(dir, "skills"), memoryRoot: join(dir, "m"), knowledgeRoot: join(dir, "k"),
      uploadsRoot: join(dir, "u"), runsRoot: join(dir, "r"), runRoot: join(dir, "r", "1"), artifactRoot: join(dir, "r", "1", "a"), binRoot: join(dir, "r", "1", "b"),
    };
    await backend.run({ prompt: "p", orgId: "org-1", tag: "run-1", workspace, hostedGraphjin: { token: "run-jwt", threadId: "thread-1" } });
    await backend.run({ prompt: "p", orgId: "org-1", tag: "run-2", workspace });

    expect(starts[0]).toMatchObject({
      type: "start",
      model_key_env: "ANTHROPIC_API_KEY",
      model_keys: { ANTHROPIC_API_KEY: "org-key" },
      graphjin: { token: "run-jwt", org_id: "org-1", thread_id: "thread-1" },
    });
    expect(JSON.stringify(starts[0]!.environ)).not.toContain("org-key");
    expect(starts[1]).not.toHaveProperty("graphjin");
  });
});
