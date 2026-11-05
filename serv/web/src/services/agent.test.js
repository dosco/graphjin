import { afterEach, describe, expect, it, vi } from "vitest";

// The console reads and writes memories, skills, profiles and threads through
// GraphQL. These tests pin the requests it sends and the row shapes it hands
// pages, so the pages keep working without any agent REST CRUD endpoint.

function respond(routes) {
  const calls = [];
  global.fetch = vi.fn(async (url, options = {}) => {
    const body = options.body ? JSON.parse(options.body) : null;
    calls.push({ url, body });
    const handler = routes.find(([match]) => (typeof match === "function" ? match(url, body) : url === match));
    const payload = handler ? handler[1](body) : { errors: [{ message: `unrouted ${url}` }] };
    return new Response(JSON.stringify(payload), { status: 200, headers: { "Content-Type": "application/json" } });
  });
  return calls;
}

const isGraphQL = (needle) => (url, body) => url.includes("/graphql") && body?.query?.includes(needle);

afterEach(() => {
  vi.resetModules();
  delete global.fetch;
});

describe("graph-backed agent services", () => {
  it("lists memories from gj_artifacts and marks the caller's own", async () => {
    respond([
      ["/api/v1/console/bootstrap", () => ({ identity: { user_ref: "sha256:me" } })],
      [isGraphQL("gj_artifacts"), () => ({
        data: {
          gj_artifacts: [
            { id: "memory:1", content: "Report in EUR", tier: "observed", owner_ref: "sha256:me" },
            { id: "memory:2", content: "FY starts April", tier: "approved", owner_ref: "sha256:teammate" },
          ],
        },
      })],
    ]);
    const { listMemories } = await import("./agent");
    const rows = await listMemories();
    expect(rows.map((row) => [row.id, row.scope, row.mine])).toEqual([
      ["memory:1", "personal", true],
      ["memory:2", "team", false],
    ]);
  });

  it("creates a skill as a gj_artifacts insert", async () => {
    const calls = respond([
      ["/api/v1/console/bootstrap", () => ({ identity: { user_ref: "sha256:me" } })],
      [isGraphQL("insert"), (body) => ({ data: { gj_artifacts: { id: "skill:1", ...body.variables.input, tier: "observed", owner_ref: "sha256:me" } } })],
    ]);
    const { addSkill } = await import("./agent");
    const skill = await addSkill({ name: "churn review", description: "How we report churn", content: "Count and MRR lost." });
    const insert = calls.find((call) => call.body?.query?.includes("insert"));
    expect(insert.body.variables.input).toEqual({ kind: "skill", name: "churn review", description: "How we report churn", content: "Count and MRR lost." });
    expect(skill.mine).toBe(true);
  });

  it("maps profile instructions to the content column both ways", async () => {
    const calls = respond([
      ["/api/v1/console/bootstrap", () => ({ identity: { user_ref: "sha256:me" } })],
      [isGraphQL("update: $input"), (body) => ({ data: { gj_artifacts: { id: body.variables.id, name: "Ops", content: body.variables.input.content, tier: "observed", owner_ref: "sha256:me" } } })],
    ]);
    const { updateProfile } = await import("./agent");
    const profile = await updateProfile("agent_profile:1", { instructions: "Focus on late shipments." });
    const update = calls.find((call) => call.body?.query?.includes("update: $input"));
    expect(update.body.variables.input).toEqual({ content: "Focus on late shipments." });
    expect(profile.instructions).toBe("Focus on late shipments.");
  });

  it("reads a thread with its messages and marks it read through gj_thread", async () => {
    const calls = respond([
      [isGraphQL("gj_thread_message"), () => ({
        data: {
          gj_thread: [{ id: "th_1", title: "Churn", unread: true }],
          gj_thread_message: [{ id: "m1", seq: 1, role: "user", content: "hi" }],
        },
      })],
      [isGraphQL("update: $input"), (body) => ({ data: { gj_thread: { id: body.variables.id, unread: false } } })],
    ]);
    const { fetchThread, markThreadSeen } = await import("./agent");
    const thread = await fetchThread("th_1");
    expect(thread.messages).toHaveLength(1);
    await markThreadSeen("th_1");
    const update = calls.find((call) => call.body?.query?.includes("update: $input"));
    expect(update.body.variables.input).toEqual({ last_seen_at: "now" });
    expect(calls.some((call) => call.url.includes("/api/v1/agent/threads"))).toBe(false);
  });
});
