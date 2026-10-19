import {
  afterAll,
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import {
  and,
  db,
  eq,
  pool,
  work_message,
  work_run,
  work_thread,
} from "@neko/db";

// Mock the embedding service so rememberWorkMemory inserts complete
// without pulling the 22MB transformers.js model. The mock is applied
// before any module that depends on it is imported.
vi.mock("@neko/llm/work", async (orig) => {
  return await orig();
});
vi.mock("../../../../packages/llm/src/embedding", () => ({
  EMBEDDING_DIM: 384,
  embedText: vi.fn(async () => Array(384).fill(0)),
  vectorLiteral: (vec: number[]) => `[${vec.join(",")}]`,
}));

import {
  appendWorkRunEvent,
  runChatTurn,
  type RunChatTurnDeps,
} from "@neko/llm/work";
import type { AgentEvent } from "@neko/llm";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[work-run-turn-summary] skipping: Postgres unreachable.");
}

const FAKE_WORKSPACE = {
  orgRoot: "/tmp/wrf/org",
  skillsRoot: "/tmp/wrf/skills",
  memoryRoot: "/tmp/wrf/memory",
  knowledgeRoot: "/tmp/wrf/knowledge",
  uploadsRoot: "/tmp/wrf/uploads",
  runsRoot: "/tmp/wrf/runs",
  threadUploadsRoot: "/tmp/wrf/uploads/t1",
  runRoot: "/tmp/wrf/runs/r1",
  artifactRoot: "/tmp/wrf/runs/r1/artifacts",
  binRoot: "/tmp/wrf/runs/r1/bin",
};

const AX_CAPABILITIES = {
  mcpTools: true,
  sessionResume: false,
  nativeDelegation: "ax-worker",
} as const;

async function insertThread(orgId: string) {
  const ins = await db()
    .insert(work_thread)
    .values({ org_id: orgId, title: "Turn summary test" })
    .returning();
  return ins[0]!;
}

async function insertRun(orgId: string, threadId: string) {
  const ins = await db()
    .insert(work_run)
    .values({ org_id: orgId, thread_id: threadId, backend: "ax", status: "queued" })
    .returning();
  return ins[0]!;
}

describeIfDb("runChatTurn — answers without final text", () => {
  let orgId: string;
  const mockBackendRun = vi.fn();

  function makeDeps(): Partial<RunChatTurnDeps> {
    return {
      resolveAgentBackend: vi.fn(async () => ({
        id: "ax" as const,
        capabilities: AX_CAPABILITIES,
        run: mockBackendRun,
      })),
      ensureWorkWorkspace: vi.fn(async () => FAKE_WORKSPACE),
      formatWorkMemoryPromptContext: vi.fn(async () => ""),
      listInstalledSkills: vi.fn(async () => []),
      prefetchKnowledgeForOrg: vi.fn(async () => ({ ok: true as const, files: [], mode: "legacy" as const })),
    };
  }

  beforeEach(async () => {
    orgId = uniqueOrgId("turn-summary");
    await createTestOrg(orgId);
    mockBackendRun.mockReset();
  });

  afterEach(async () => {
    await deleteTestOrg(orgId);
    vi.clearAllMocks();
  });

  afterAll(async () => {
    await pool().end();
  });

  async function savedAnswer(runId: string) {
    return db()
      .select({ content: work_message.content })
      .from(work_message)
      .where(and(eq(work_message.run_id, runId), eq(work_message.role, "assistant")));
  }

  it("saves cards posted through the broker and streamed text for the next turn", async () => {
    const thread = await insertThread(orgId);
    const run = await insertRun(orgId, thread.id);
    mockBackendRun.mockImplementation(async () => {
      const event = (event: AgentEvent) => appendWorkRunEvent({ orgId, threadId: thread.id, runId: run.id, event });
      await event({
        type: "surface",
        messages: [{
          version: "v1.0",
          createSurface: {
            surfaceId: "tools",
            components: [
              { id: "root", component: "Answer", title: "Tool number to order relationship", children: ["c"] },
              { id: "c", component: "Callout", mood: "good", text: "One tool number serves many orders." },
            ],
          },
        }],
      });
      await event({ type: "message", role: "assistant", content: "That is confirmed above." });
      return { finalText: "", status: "completed", backendState: {} };
    });

    await runChatTurn({ orgId, threadId: thread.id, runId: run.id, message: "how do tools relate to orders?", emit: async () => {} }, makeDeps());

    const [answer] = await savedAnswer(run.id);
    expect(answer.content).toContain("That is confirmed above.");
    expect(answer.content).toContain("Tool number to order relationship");
    expect(answer.content).toContain("One tool number serves many orders.");
  });

  it("records the last usage snapshot when a cancelled turn reports none", async () => {
    const thread = await insertThread(orgId);
    const run = await insertRun(orgId, thread.id);
    mockBackendRun.mockImplementation(async (opts: { onEvent?: (event: AgentEvent) => Promise<void> }) => {
      await opts.onEvent?.({
        type: "tool_start",
        id: "t1",
        name: "terminal",
        input: {},
        usageSnapshot: { inputTokens: 900, outputTokens: 40, estimatedCostUsd: 0.42, costStatus: "estimated", coverage: "complete" },
      });
      return { finalText: "", status: "cancelled", backendState: {} };
    });
    const events: AgentEvent[] = [];

    await runChatTurn({ orgId, threadId: thread.id, runId: run.id, message: "stop", emit: async (event) => { events.push(event); } }, makeDeps());

    expect(events.filter((event) => event.type === "usage")).toEqual([
      expect.objectContaining({
        source: "outer",
        usage: expect.objectContaining({ estimatedCostUsd: 0.42, coverage: "partial" }),
      }),
    ]);
  });

  it("saves no answer when the turn left nothing to carry forward", async () => {
    const thread = await insertThread(orgId);
    const run = await insertRun(orgId, thread.id);
    mockBackendRun.mockResolvedValue({ finalText: "", status: "completed", backendState: {} });

    await runChatTurn({ orgId, threadId: thread.id, runId: run.id, message: "hello", emit: async () => {} }, makeDeps());

    expect(await savedAnswer(run.id)).toHaveLength(0);
  });
});
