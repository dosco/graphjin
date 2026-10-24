// Verifies that runChatTurn extracts ```neko_memory fences from the
// agent's final response, persists each {save:...} via rememberWorkMemory,
// and strips the fence from the user-visible assistant message.
//
// This is the end-to-end equivalent of the unit tests in
// packages/llm/test/memory-fence.test.ts (parser only) — here we hit a
// real Postgres so the parser-to-DB plumbing actually runs.

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
  isNull,
  pool,
  work_memory,
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
  runChatTurn,
  type RunChatTurnDeps,
} from "@neko/llm/work";
import type { AgentEvent } from "@neko/llm";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[work-run-memory-fence] skipping: Postgres unreachable.");
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
    .values({ org_id: orgId, title: "Memory fence test" })
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

describeIfDb("runChatTurn — neko_memory fence persistence", () => {
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
    orgId = uniqueOrgId("fence-persist");
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

  it("persists each save op from the fence and strips the fence from the assistant message", async () => {
    const thread = await insertThread(orgId);
    const run = await insertRun(orgId, thread.id);

    const fenced = [
      "Got it. I'll remember those.",
      "",
      "```neko_memory",
      JSON.stringify([
        { save: { text: "Always cite the table name in your reasoning" } },
        { save: { text: "TTM windows must anchor on max(date)", scope: "global" } },
      ]),
      "```",
      "",
      "Anything else you want me to note?",
    ].join("\n");

    mockBackendRun.mockResolvedValue({
      finalText: fenced,
      status: "completed",
      backendState: {},
    });

    const events: AgentEvent[] = [];
    await runChatTurn(
      {
        orgId,
        threadId: thread.id,
        runId: run.id,
        message: "remember these two rules please",
        emit: async (e) => {
          events.push(e);
        },
      },
      makeDeps(),
    );

    // Both fence saves landed in work_memory.
    const rows = await db()
      .select({ text: work_memory.text, scope: work_memory.scope, kind: work_memory.kind, pinned: work_memory.pinned })
      .from(work_memory)
      .where(and(eq(work_memory.org_id, orgId), isNull(work_memory.archived_at)));
    const texts = rows.map((r) => r.text).sort();
    expect(texts).toEqual([
      "Always cite the table name in your reasoning",
      "TTM windows must anchor on max(date)",
    ]);
    // Defaults from the fence parser → rememberWorkMemory shaping.
    for (const r of rows) {
      expect(r.scope).toBe("global");
      expect(r.kind).toBe("business_rule");
      expect(r.pinned).toBe(true);
    }

    // The persisted assistant message is the prose around the fence,
    // with the fence body stripped out.
    const messages = await db()
      .select({ role: work_message.role, content: work_message.content })
      .from(work_message)
      .where(and(eq(work_message.run_id, run.id), eq(work_message.role, "assistant")));
    expect(messages).toHaveLength(1);
    expect(messages[0].content).toContain("Got it.");
    expect(messages[0].content).toContain("Anything else");
    expect(messages[0].content).not.toContain("neko_memory");
    expect(messages[0].content).not.toContain('"save"');
  });

  it("does nothing memory-side when the agent response has no fence", async () => {
    const thread = await insertThread(orgId);
    const run = await insertRun(orgId, thread.id);

    mockBackendRun.mockResolvedValue({
      finalText: "Plain answer, no fence here.",
      status: "completed",
      backendState: {},
    });

    await runChatTurn(
      {
        orgId,
        threadId: thread.id,
        runId: run.id,
        message: "tell me something",
        emit: async () => {},
      },
      makeDeps(),
    );

    const rows = await db()
      .select({ id: work_memory.id })
      .from(work_memory)
      .where(eq(work_memory.org_id, orgId));
    expect(rows).toHaveLength(0);
  });

  it("doesn't tank the run if a fence save fails — assistant message still persists", async () => {
    const thread = await insertThread(orgId);
    const run = await insertRun(orgId, thread.id);

    // Malformed fence body → parser drops the op silently. We still
    // expect the run to complete and the (stripped) prose to land.
    const fenced = [
      "Note this:",
      "```neko_memory",
      "{ this is not valid json",
      "```",
      "Done.",
    ].join("\n");

    mockBackendRun.mockResolvedValue({
      finalText: fenced,
      status: "completed",
      backendState: {},
    });

    await runChatTurn(
      {
        orgId,
        threadId: thread.id,
        runId: run.id,
        message: "try the broken fence",
        emit: async () => {},
      },
      makeDeps(),
    );

    const memRows = await db()
      .select({ id: work_memory.id })
      .from(work_memory)
      .where(eq(work_memory.org_id, orgId));
    expect(memRows).toHaveLength(0);

    const messages = await db()
      .select({ content: work_message.content })
      .from(work_message)
      .where(and(eq(work_message.run_id, run.id), eq(work_message.role, "assistant")));
    expect(messages[0].content).toContain("Note this:");
    expect(messages[0].content).toContain("Done.");
    expect(messages[0].content).not.toContain("neko_memory");
  });
});
