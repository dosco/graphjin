import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { createTestOrg, dbReachable, deleteTestOrg, uniqueOrgId } from "@neko/db/test-helpers";
import {
  addLocalGroupMember,
  app_user,
  builtinGroupId,
  createUserGroup,
  db,
  grantItem,
  metric,
  pool,
  revokeItem,
} from "@neko/db";
import { callRoute } from "../_helpers/route";

const state = vi.hoisted(() => ({ orgId: "", userId: "" }));
vi.mock("@/lib/db", async () => ({ ...(await vi.importActual<typeof import("@/lib/db")>("@/lib/db")), getOrgId: async () => state.orgId }));
vi.mock("@/lib/auth", async () => ({
  ...(await vi.importActual<typeof import("@/lib/auth")>("@/lib/auth")),
  getCurrentUser: async () => ({ id: state.userId, email: "ann@example.test", name: "Ann" }),
  getAuthProvider: async () => ({ pluginName: "test" }),
}));

const reachable = await dbReachable();

/**
 * Item coverage: for each item type a member of Finance holds one of two
 * items, and every route that lists or opens that type shows only it.
 */
(reachable ? describe : describe.skip)("item coverage", () => {
  let home: string;
  let workflowHeld: string;
  let workflowOther: string;
  let metricHeld: string;
  let metricOther: string;

  beforeAll(async () => {
    home = await mkdtemp(join(tmpdir(), "item-coverage-home-"));
    process.env.HOME = home;
    state.orgId = uniqueOrgId("coverage");
    state.userId = `${state.orgId}-ann`;
    await createTestOrg(state.orgId);
    await db().insert(app_user).values({ id: state.userId, org_id: state.orgId, email: "ann@example.test" });

    const everyone = await builtinGroupId(state.orgId, "everyone");
    for (const type of ["skill", "workflow", "metric", "dashboard", "team_memory"] as const) {
      await revokeItem(state.orgId, { groupId: everyone, itemType: type, itemId: "*" });
    }
    const finance = await createUserGroup(state.orgId, { name: "Finance" });
    await addLocalGroupMember(state.orgId, finance.id, state.userId);

    const { ensureOrgWorkspace } = await import("@neko/llm/work");
    const workspace = await ensureOrgWorkspace(state.orgId);
    for (const name of ["finance-close", "sales-playbook"]) {
      await mkdir(join(workspace.skillsRoot, name), { recursive: true });
      await writeFile(join(workspace.skillsRoot, name, "SKILL.md"), `---\nname: ${name}\ndescription: ${name}\n---\nBody\n`);
    }
    await grantItem(state.orgId, { groupId: finance.id, itemType: "skill", itemId: "finance-close" });

    const { saveWorkflow } = await import("@neko/llm/workflows");
    workflowHeld = (await saveWorkflow({ orgId: state.orgId, name: "Daily revenue check", steps: [] })).workflow.id;
    workflowOther = (await saveWorkflow({ orgId: state.orgId, name: "Promotions", steps: [] })).workflow.id;
    await grantItem(state.orgId, { groupId: finance.id, itemType: "workflow", itemId: workflowHeld });

    const [held] = await db().insert(metric).values({ org_id: state.orgId, role: "CFO", slug: "revenue", source: "persona", title: "Revenue", why: "" }).returning({ id: metric.id });
    const [other] = await db().insert(metric).values({ org_id: state.orgId, role: "CFO", slug: "refunds", source: "persona", title: "Refunds", why: "" }).returning({ id: metric.id });
    metricHeld = held!.id;
    metricOther = other!.id;
    await grantItem(state.orgId, { groupId: finance.id, itemType: "metric", itemId: metricHeld });
    await grantItem(state.orgId, { groupId: finance.id, itemType: "dashboard", itemId: "CFO" });
  });

  afterAll(async () => {
    await deleteTestOrg(state.orgId);
    await rm(home, { recursive: true, force: true });
    await pool().end();
  });

  it("skills: list and detail", async () => {
    const list = await import("@/app/api/work/skills/route");
    const detail = await import("@/app/api/work/skills/[name]/route");
    const skills = (await callRoute(list.GET)).body as { skills: Array<{ name: string }> };
    expect(skills.skills.map((s) => s.name)).toContain("finance-close");
    expect(skills.skills.map((s) => s.name)).not.toContain("sales-playbook");
    expect((await callRoute((req) => detail.GET(req, { params: Promise.resolve({ name: "finance-close" }) }))).status).toBe(200);
    expect((await callRoute((req) => detail.GET(req, { params: Promise.resolve({ name: "sales-playbook" }) }))).status).toBe(404);
  });

  it("workflows: list, detail and runs", async () => {
    const list = await import("@/app/api/workflows/route");
    const detail = await import("@/app/api/workflows/[workflowId]/route");
    const runs = await import("@/app/api/workflow-runs/route");
    const workflows = (await callRoute(list.GET)).body as { workflows: Array<{ id: string }> };
    expect(workflows.workflows.map((w) => w.id)).toEqual([workflowHeld]);
    expect((await callRoute((req) => detail.GET(req, { params: Promise.resolve({ workflowId: workflowHeld }) }))).status).toBe(200);
    expect((await callRoute((req) => detail.GET(req, { params: Promise.resolve({ workflowId: workflowOther }) }))).status).toBe(404);
    expect((await callRoute(runs.GET)).status).toBe(200);
  });

  it("dashboards and metrics: briefing tiles and metric detail", async () => {
    const briefing = await import("@/app/api/briefing/route");
    const byMetric = await import("@/app/api/briefing/by-metric/route");
    expect((await callRoute(briefing.GET, { query: { role: "COO" } })).status).toBe(404);
    const cfo = await callRoute(briefing.GET, { query: { role: "CFO" } });
    expect(cfo.status).toBe(200);
    const text = JSON.stringify(cfo.body);
    expect(text).toContain(metricHeld);
    expect(text).not.toContain(metricOther);
    expect((await callRoute(byMetric.GET, { query: { metricId: metricOther } })).status).toBe(404);
  });

  it("team memory: list hides team global memories", async () => {
    const { rememberWorkMemory } = await import("@neko/llm/work");
    await rememberWorkMemory({ orgId: state.orgId, userId: null, kind: "business_rule", scope: "global", text: "Team rule" });
    await rememberWorkMemory({ orgId: state.orgId, userId: state.userId, kind: "preference", scope: "global", text: "Ann note" });
    const memories = await import("@/app/api/work/memories/route");
    const body = (await callRoute(memories.GET)).body as { memories: Array<{ text: string }> };
    expect(body.memories.map((m) => m.text)).toEqual(["Ann note"]);
  });
});
