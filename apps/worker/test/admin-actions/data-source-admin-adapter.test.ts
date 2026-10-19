// ADM2 — the data_source_admin adapter manages the registry once an
// admin approves the chat-proposed change. Registration is a disabled
// placeholder: connection details only ever arrive via the settings
// form, never chat.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import { and, data_source, db, eq, pool } from "@neko/db";
import {
  createActionRequest,
  executeApprovedActionRequest,
} from "@neko/llm/workflows";
import { inProcessControlPlane } from "@neko/llm/work";
import { registerDataSourceAdminAdapter } from "../../src/admin-actions/adapters.js";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[data-source-admin-adapter] skipping: Postgres unreachable.");
}

describeIfDb("data_source_admin adapter (ADM2)", () => {
  const orgId = uniqueOrgId("dsadm");

  beforeAll(async () => {
    registerDataSourceAdminAdapter();
    await createTestOrg(orgId);
    await db().insert(data_source).values({
      org_id: orgId,
      kind: "graphjin",
      graphql_url: "http://gj:8080/api/v1/graphql",
      name: "default",
      is_default: true,
    });
  });

  afterAll(async () => {
    await deleteTestOrg(orgId);
    await pool().end();
  });

  async function runAction(payload: Record<string, unknown>) {
    const request = await createActionRequest({
      orgId,
      scope: "internal",
      kind: "data_source_admin",
      target: String(payload.name ?? ""),
      payload,
      riskLevel: "high",
      status: "approved",
      summary: "test",
    });
    return executeApprovedActionRequest(orgId, request.id);
  }

  async function source(name: string) {
    const [row] = await db()
      .select()
      .from(data_source)
      .where(and(eq(data_source.org_id, orgId), eq(data_source.name, name)));
    return row;
  }

  it("register accepts a typed source kind (OL5 source graph)", async () => {
    const result = await runAction({
      action: "register",
      name: "billing-api",
      sourceKind: "api",
    });
    expect(result.ok).toBe(true);
    expect((await source("billing-api")).kind).toBe("api");
    // Unknown kinds fall back to graphjin rather than polluting the registry.
    await runAction({ action: "register", name: "weird", sourceKind: "blockchain" });
    expect((await source("weird")).kind).toBe("graphjin");
  });

  it("register creates a disabled placeholder with no connection details", async () => {
    const result = await runAction({
      action: "register",
      name: "warehouse",
      label: "Snowflake warehouse",
    });
    expect(result.ok).toBe(true);
    const row = await source("warehouse");
    expect(row).toMatchObject({
      enabled: false,
      is_default: false,
      graphql_url: "",
      label: "Snowflake warehouse",
    });
  });

  it("enable, set_default and remove manage the registry", async () => {
    expect((await runAction({ action: "enable", name: "warehouse" })).ok).toBe(true);
    expect((await source("warehouse")).enabled).toBe(true);

    expect((await runAction({ action: "set_default", name: "warehouse" })).ok).toBe(true);
    expect((await source("warehouse")).is_default).toBe(true);
    expect((await source("default")).is_default).toBe(false);

    // The default can't be removed.
    const removeDefault = await runAction({ action: "remove", name: "warehouse" });
    expect(removeDefault.ok).toBe(false);
    expect(removeDefault.error).toMatch(/default/i);

    expect((await runAction({ action: "remove", name: "default" })).ok).toBe(true);
    expect(await source("default")).toBeUndefined();
  });

  it("listDataSources sanitizes to hostnames — no connection strings reach the agent", async () => {
    const { sources } = await inProcessControlPlane.listDataSources({ orgId });
    const warehouse = sources.find((s) => s.name === "warehouse");
    expect(warehouse).toBeTruthy();
    for (const s of sources) {
      expect(JSON.stringify(s)).not.toContain("graphql_url");
      expect(JSON.stringify(s)).not.toContain("://");
    }
  });
});
