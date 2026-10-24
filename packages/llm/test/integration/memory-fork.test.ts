// CV2 — memory fork overlay. Personal layers are copy-on-write over the
// team layer: members shadow (edit) or suppress (hide) team memories
// without touching them, and admins promote personal memories back into
// the team layer with lineage.

import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { app_user, db, eq, pool, work_memory } from "@neko/db";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";

vi.mock("../../src/embedding", async (importOriginal) => {
  const { fakeEmbedding } = await import("../helpers/fake-embedding");
  return fakeEmbedding(await importOriginal());
});

import {
  archiveWorkMemory,
  listWorkMemories,
  memoryLayerForActor,
  overrideWorkMemoryForUser,
  promoteWorkMemoryToOrg,
  rememberWorkMemory,
  searchWorkMemoryByContext,
  updateWorkMemoryForRun,
} from "../../src/work/memory";
import { createWorkRun, createWorkThread } from "../../src/work/store";
import { drainEmbeddingBacklog } from "../helpers/fake-embedding";
import { setSemanticSearchEnabledForTests } from "../../src/semantic-search";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[memory-fork] skipping: Postgres unreachable.");
}

describeIfDb("CV2 memory fork overlay", () => {
  const orgId = uniqueOrgId("memfork");
  let ada: string;
  let bob: string;

  beforeAll(async () => {
    await createTestOrg(orgId);
    ada = `${orgId}-ada`;
    bob = `${orgId}-bob`;
    await db()
      .insert(app_user)
      .values([
        { id: ada, email: "ada@example.com", org_id: orgId },
        { id: bob, email: "bob@example.com", org_id: orgId },
      ]);
  });

  afterAll(async () => {
    await deleteTestOrg(orgId);
    await pool().end();
    vi.unstubAllEnvs();
  });

  it("memoryLayerForActor maps members to their layer, everyone else to team", () => {
    expect(memoryLayerForActor({ userId: ada, role: "member" })).toBe(ada);
    expect(memoryLayerForActor({ userId: ada, role: "admin" })).toBeNull();
    expect(memoryLayerForActor({ userId: null, role: "member" })).toBeNull();
    expect(memoryLayerForActor({ userId: null, role: "service" })).toBeNull();
  });

  it("solo/admin writes land in the team layer with origin self-link", async () => {
    const memory = await rememberWorkMemory({
      orgId,
      text: "Team rule: fiscal year starts in July",
      kind: "business_rule",
      scope: "global",
    });
    expect(memory.userId).toBeNull();
    expect(memory.originId).toBe(memory.id);

    const teamView = await listWorkMemories(orgId);
    expect(teamView.map((m) => m.id)).toContain(memory.id);
  });

  it("a member run's save lands in their personal layer (resolved from the K1 actor)", async () => {
    const thread = await createWorkThread(orgId, "fork-thread", "web", ada);
    const run = await createWorkRun(orgId, thread.id, "ax", {
      userId: ada,
      role: "member",
    });
    const memory = await rememberWorkMemory({
      orgId,
      threadId: thread.id,
      runId: run.id,
      text: "Ada-only: I prefer weekly granularity",
      kind: "preference",
      scope: "global",
    });
    expect(memory.userId).toBe(ada);
    expect(memory.originId).toBe(memory.id);

    // Personal rows never leak into the team/solo view or another member's.
    const teamView = await listWorkMemories(orgId);
    expect(teamView.map((m) => m.id)).not.toContain(memory.id);
    const bobView = await listWorkMemories(orgId, { userId: bob });
    expect(bobView.map((m) => m.id)).not.toContain(memory.id);
    const adaView = await listWorkMemories(orgId, { userId: ada });
    expect(adaView.map((m) => m.id)).toContain(memory.id);
  });

  it("editing a team memory copies it into the member's layer and shadows the original", async () => {
    const team = await rememberWorkMemory({
      orgId,
      text: "Revenue means gross revenue",
      kind: "metric_definition",
      scope: "global",
    });
    const override = await overrideWorkMemoryForUser({
      orgId,
      userId: ada,
      memoryId: team.id,
      text: "Revenue means net revenue after refunds",
    });
    expect(override.userId).toBe(ada);
    expect(override.overridesOriginId).toBe(team.originId);

    const adaView = await listWorkMemories(orgId, { userId: ada });
    const adaIds = adaView.map((m) => m.id);
    expect(adaIds).toContain(override.id);
    expect(adaIds).not.toContain(team.id);

    // Team row untouched: team and other members still see the original.
    const teamView = await listWorkMemories(orgId);
    expect(teamView.map((m) => m.id)).toContain(team.id);
    expect(teamView.map((m) => m.id)).not.toContain(override.id);
    const bobView = await listWorkMemories(orgId, { userId: bob });
    expect(bobView.map((m) => m.id)).toContain(team.id);

    // Editing again updates the same override row instead of stacking.
    const second = await overrideWorkMemoryForUser({
      orgId,
      userId: ada,
      memoryId: team.id,
      text: "Revenue means net revenue, refunds and credits excluded",
    });
    expect(second.id).toBe(override.id);
  });

  it("suppressing a team memory hides it for that member only", async () => {
    const team = await rememberWorkMemory({
      orgId,
      text: "Always include the EMEA region split",
      kind: "business_rule",
      scope: "global",
    });
    const tombstone = await overrideWorkMemoryForUser({
      orgId,
      userId: ada,
      memoryId: team.id,
      suppress: true,
    });
    expect(tombstone.suppressed).toBe(true);

    const adaIds = (await listWorkMemories(orgId, { userId: ada })).map((m) => m.id);
    expect(adaIds).not.toContain(team.id);
    expect(adaIds).not.toContain(tombstone.id);

    const bobIds = (await listWorkMemories(orgId, { userId: bob })).map((m) => m.id);
    expect(bobIds).toContain(team.id);
    expect((await listWorkMemories(orgId)).map((m) => m.id)).toContain(team.id);
  });

  it("members cannot archive team rows, and cannot touch another member's rows", async () => {
    const team = await rememberWorkMemory({
      orgId,
      text: "Quarterly targets are set by finance",
      kind: "business_rule",
      scope: "global",
    });
    expect(await archiveWorkMemory(orgId, team.id, { userId: ada })).toBe(false);

    const personal = await rememberWorkMemory({
      orgId,
      userId: ada,
      text: "Ada personal note to archive",
      kind: "thread_note",
      scope: "global",
    });
    await expect(
      overrideWorkMemoryForUser({
        orgId,
        userId: bob,
        memoryId: personal.id,
        text: "bob hijack",
      }),
    ).rejects.toThrow(/not found/i);
    expect(await archiveWorkMemory(orgId, personal.id, { userId: bob })).toBe(false);
    expect(await archiveWorkMemory(orgId, personal.id, { userId: ada })).toBe(true);
  });

  it("promote pulls a personal original into the team layer with lineage", async () => {
    const personal = await rememberWorkMemory({
      orgId,
      userId: ada,
      text: "Churn is measured on a 90-day window",
      kind: "metric_definition",
      scope: "global",
    });
    const promoted = await promoteWorkMemoryToOrg({
      orgId,
      memoryId: personal.id,
      promotedBy: "admin-1",
    });
    expect(promoted.userId).toBeNull();
    expect(promoted.originId).toBe(personal.originId);
    expect(promoted.promotedFromId).toBe(personal.id);
    expect(promoted.promotedBy).toBe("admin-1");
    expect(promoted.promotedAt).toBeTruthy();

    // Everyone sees the team row now; the personal source is archived.
    for (const layer of [undefined, ada, bob]) {
      const ids = (await listWorkMemories(orgId, { userId: layer })).map((m) => m.id);
      expect(ids).toContain(promoted.id);
      expect(ids).not.toContain(personal.id);
    }
  });

  it("promoting an override replaces the team row; other members' overrides keep shadowing", async () => {
    const team = await rememberWorkMemory({
      orgId,
      text: "Default currency is USD",
      kind: "business_rule",
      scope: "global",
    });
    const adaOverride = await overrideWorkMemoryForUser({
      orgId,
      userId: ada,
      memoryId: team.id,
      text: "Default currency is EUR",
    });
    const bobOverride = await overrideWorkMemoryForUser({
      orgId,
      userId: bob,
      memoryId: team.id,
      text: "Default currency is GBP",
    });

    const promoted = await promoteWorkMemoryToOrg({
      orgId,
      memoryId: adaOverride.id,
      promotedBy: "admin-1",
    });
    expect(promoted.originId).toBe(team.originId);
    expect(promoted.text).toBe("Default currency is EUR");

    const teamIds = (await listWorkMemories(orgId)).map((m) => m.id);
    expect(teamIds).toContain(promoted.id);
    expect(teamIds).not.toContain(team.id);

    // Ada's override was consumed by the promote — she sees the team row.
    const adaIds = (await listWorkMemories(orgId, { userId: ada })).map((m) => m.id);
    expect(adaIds).toContain(promoted.id);
    expect(adaIds).not.toContain(adaOverride.id);

    // Bob's override still shadows the (new) team row for him.
    const bobIds = (await listWorkMemories(orgId, { userId: bob })).map((m) => m.id);
    expect(bobIds).toContain(bobOverride.id);
    expect(bobIds).not.toContain(promoted.id);
  });

  it("suppressed memories cannot be promoted", async () => {
    const team = await rememberWorkMemory({
      orgId,
      text: "Weekly sync notes go to the wiki",
      kind: "business_rule",
      scope: "global",
    });
    const tombstone = await overrideWorkMemoryForUser({
      orgId,
      userId: bob,
      memoryId: team.id,
      suppress: true,
    });
    await expect(
      promoteWorkMemoryToOrg({
        orgId,
        memoryId: tombstone.id,
        promotedBy: "admin-1",
      }),
    ).rejects.toThrow(/suppressed/i);
  });

  it("context search respects the layer", async () => {
    const team = await rememberWorkMemory({
      orgId,
      text: "Searchable team fact about llamas",
      kind: "company_context",
      scope: "global",
    });
    const personal = await rememberWorkMemory({
      orgId,
      userId: ada,
      text: "Searchable ada fact about alpacas",
      kind: "company_context",
      scope: "global",
    });
    setSemanticSearchEnabledForTests(true);
    await drainEmbeddingBacklog();

    const teamResults = await searchWorkMemoryByContext({
      orgId,
      query: "searchable fact",
      limit: 20,
    });
    const teamIds = teamResults.map((r) => r.memory.id);
    expect(teamIds).toContain(team.id);
    expect(teamIds).not.toContain(personal.id);

    const adaResults = await searchWorkMemoryByContext({
      orgId,
      query: "searchable fact",
      limit: 20,
      userId: ada,
    });
    const adaIds = adaResults.map((r) => r.memory.id);
    expect(adaIds).toContain(team.id);
    expect(adaIds).toContain(personal.id);
    setSemanticSearchEnabledForTests(null);
  });

  it("an agent edit rewrites a team row for a team run and forks it for a member run", async () => {
    const team = await rememberWorkMemory({
      orgId,
      text: "12x18 sheets use the small press",
      kind: "business_rule",
      scope: "global",
    });
    const updated = await updateWorkMemoryForRun({
      orgId,
      runId: null,
      memoryId: team.id,
      text: "12x18 and 18x12 sheets use the small press",
    });
    expect(updated.id).toBe(team.id);
    expect(updated.text).toBe("12x18 and 18x12 sheets use the small press");
    expect(updated.integrityOk).toBe(true);

    const thread = await createWorkThread(orgId, "edit-thread", "web", ada);
    const run = await createWorkRun(orgId, thread.id, "ax", { userId: ada, role: "member" });
    const forked = await updateWorkMemoryForRun({
      orgId,
      runId: run.id,
      threadId: thread.id,
      memoryId: team.id,
      text: "Ada: all 12x18 sheets use the large press",
    });
    expect(forked.userId).toBe(ada);
    expect(forked.overridesOriginId).toBe(team.originId);
    expect((await listWorkMemories(orgId)).find((m) => m.id === team.id)?.text).toBe(
      "12x18 and 18x12 sheets use the small press",
    );
  });

  it("an agent cannot edit another member's memory", async () => {
    const bobs = await rememberWorkMemory({
      orgId,
      userId: bob,
      text: "Bob prefers monthly totals",
      kind: "preference",
      scope: "global",
    });
    const thread = await createWorkThread(orgId, "edit-guard", "web", ada);
    const run = await createWorkRun(orgId, thread.id, "ax", { userId: ada, role: "member" });
    await expect(
      updateWorkMemoryForRun({ orgId, runId: run.id, memoryId: bobs.id, text: "Ada rewrote this" }),
    ).rejects.toThrow(/not found/);
    await expect(
      updateWorkMemoryForRun({ orgId, runId: null, memoryId: bobs.id, text: "Team rewrote this" }),
    ).rejects.toThrow(/not found/);
  });

  it("existing rows are unchanged for solo orgs (backfill self-link)", async () => {
    // Simulate a pre-CV2 row: origin_id backfilled to id by the migration.
    const memory = await rememberWorkMemory({
      orgId,
      text: "Legacy-shaped row",
      kind: "other",
      scope: "global",
    });
    const [row] = await db()
      .select({ origin: work_memory.origin_id, user: work_memory.user_id })
      .from(work_memory)
      .where(eq(work_memory.id, memory.id));
    expect(row).toEqual({ origin: memory.id, user: null });
  });
});
