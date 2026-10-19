// The delegated data path: GraphJin's agent streams its steps to the run, and
// a thread's earlier data questions go with each follow-up as history.

import { createServer, type Server } from "node:http";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { createTestOrg, dbReachable, deleteTestOrg, seedDataSource, uniqueOrgId } from "@neko/db/test-helpers";
import { inProcessControlPlane } from "../../src/work/control-plane";
import { createWorkRun, createWorkThread } from "../../src/work/store";
import { dataAgentStepMessage } from "../../src/work/broker";

const reachable = await dbReachable();

(reachable ? describe : describe.skip)("delegated data agent", () => {
  let server: Server;
  let url: string;
  const requests: Array<{ instruction: string; history?: Array<{ role: string; content: string }> }> = [];
  const accepts: string[] = [];

  beforeAll(async () => {
    // GraphJin's agent routes, speaking its exact SSE framing.
    server = createServer((req, res) => {
      if (req.url?.endsWith("/agent/status")) {
        res.setHeader("content-type", "application/json");
        res.end(JSON.stringify({ status: "ready", enabled: true, ready: true, read_only: true }));
        return;
      }
      let body = "";
      req.on("data", (c) => (body += c));
      req.on("end", () => {
        requests.push(JSON.parse(body));
        accepts.push(String(req.headers.accept));
        const answer = requests.length === 1 ? "Revenue was 1200." : "Last month it was 900.";
        res.writeHead(200, { "content-type": "text/event-stream" });
        const frame = (event: string, data: unknown) => `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`;
        // Split mid-frame, as a network would.
        const stream =
          frame("action", { index: 1, source: "model", tool: "query_catalog", status: "ok", elapsed_ms: 4 }) +
          frame("action", { index: 2, source: "model", tool: "execute_graphql", status: "ok", elapsed_ms: 9 }) +
          frame("result", { status: "answered", answer }) +
          frame("complete", { status: "answered" });
        res.write(stream.slice(0, 37));
        setTimeout(() => res.end(stream.slice(37)), 20);
      });
    });
    await new Promise<void>((done) => server.listen(0, "127.0.0.1", done));
    url = `http://127.0.0.1:${(server.address() as { port: number }).port}/api/v1/graphql`;
  });

  afterAll(async () => {
    await new Promise<void>((done) => server.close(() => done()));
  });

  it("streams GraphJin's steps and sends the thread's earlier questions as history", async () => {
    const orgId = uniqueOrgId("data-agent");
    await createTestOrg(orgId);
    try {
      await seedDataSource(orgId, { graphqlUrl: url });
      const thread = await createWorkThread(orgId, "t");
      const run = await createWorkRun(orgId, thread.id, "ax");
      const steps: string[] = [];
      const ask = (instruction: string) =>
        inProcessControlPlane.askGraphjinDataAgent({
          orgId,
          runId: run.id,
          threadId: thread.id,
          instruction,
          onAction: (event) => steps.push(dataAgentStepMessage(event)),
        });

      const first = await ask("What was revenue?");
      expect(first.response?.answer).toBe("Revenue was 1200.");
      expect(steps).toEqual(["Data agent: query catalog", "Data agent: execute graphql"]);
      expect(accepts[0]).toBe("text/event-stream");
      expect(requests[0]!.history).toBeUndefined();

      const second = await ask("And last month?");
      expect(second.response?.answer).toBe("Last month it was 900.");
      expect(requests[1]!.history).toEqual([
        { role: "user", content: "What was revenue?" },
        { role: "assistant", content: "Revenue was 1200.", status: "answered" },
      ]);

      // Another thread starts with no history.
      const other = await createWorkThread(orgId, "u");
      const otherRun = await createWorkRun(orgId, other.id, "ax");
      await inProcessControlPlane.askGraphjinDataAgent({ orgId, runId: otherRun.id, threadId: other.id, instruction: "Revenue?" });
      expect(requests[2]!.history).toBeUndefined();
      expect(accepts[2]).toBe("application/json");
    } finally {
      await deleteTestOrg(orgId);
    }
  });
});
