import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it, vi } from "vitest";
import { parse } from "yaml";
import {
  builtinGroupId,
  createUserGroup,
  db,
  eq,
  getPreviousReadModes,
  grantItem,
  organization,
  pool,
  revokeItem,
  setGroupGrantsEnabled,
  upsertDataAccessRule,
} from "@neko/db";
import { applyGroupGrants, disableGroupGrants, enableGroupGrants, scheduleGroupGrantsApply } from "../src/graphjin/group-grants-service";

const reachable = await pool().query("select 1").then(() => true, () => false);

const CONFIG = `identity:
  role_claims: [role]
roles:
  - name: member
sources:
  - name: shop
    kind: database
    access:
      read: account
`;

describe("scheduleGroupGrantsApply", () => {
  it("runs one apply after the last change", async () => {
    vi.useFakeTimers();
    const run = vi.fn(async () => undefined);
    scheduleGroupGrantsApply("org-a", 1000, run);
    vi.advanceTimersByTime(600);
    scheduleGroupGrantsApply("org-a", 1000, run);
    vi.advanceTimersByTime(999);
    expect(run).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(run).toHaveBeenCalledTimes(1);
    vi.useRealTimers();
  });
});

(reachable ? describe : describe.skip)("applyGroupGrants", () => {
  it("does nothing while off, then writes grants once and applies them live only on change", async () => {
    const orgId = `grants-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`;
    const dir = await mkdtemp(join(tmpdir(), "gj-grants-"));
    const configFile = join(dir, "config.yml");
    await writeFile(configFile, CONFIG);
    await db().insert(organization).values({ id: orgId, name: "Grants" });
    const applyLive = vi.fn(async (_update: Record<string, unknown>) => undefined);
    try {
      expect(await applyGroupGrants(orgId, { configFile, applyLive })).toEqual({ enabled: false, changed: false, roles: [] });
      expect(await readFile(configFile, "utf8")).toBe(CONFIG);

      const finance = await createUserGroup(orgId, { name: "Finance" });
      await revokeItem(orgId, { groupId: await builtinGroupId(orgId, "everyone"), itemType: "data_source", itemId: "*" });
      await grantItem(orgId, { groupId: finance.id, itemType: "data_source", itemId: "shop" });
      await upsertDataAccessRule(orgId, { groupId: finance.id, source: "shop", tableName: "orders", columns: ["id"], rowFilter: { column: "region", op: "eq", value: "emea" } });
      await setGroupGrantsEnabled(orgId, true, null);

      expect(await applyGroupGrants(orgId, { configFile, applyLive })).toEqual({ enabled: true, changed: true, roles: ["og_finance"] });
      const config = parse(await readFile(configFile, "utf8"));
      expect(config.identity.role_mode).toBe("union");
      expect(config.sources[0].access).toEqual({
        read: "admin",
        grants: [{ role: "og_finance", tables: [{ name: "orders", columns: ["id"], filter: '{ and: [{ account_id: { eq: $account_id } }, { region: { eq: "emea" } }] }' }] }],
      });
      expect(await getPreviousReadModes(orgId)).toEqual({ shop: "account" });
      expect(applyLive).toHaveBeenCalledTimes(1);
      expect(applyLive.mock.calls[0]![0]).toMatchObject({
        identity: { role_mode: "union" },
        roles: [{ name: "og_finance" }],
        source_patches: [{ name: "shop", access: { read: "admin", grants: [{ role: "og_finance" }] } }],
      });

      expect((await applyGroupGrants(orgId, { configFile, applyLive })).changed).toBe(false);
      expect(applyLive).toHaveBeenCalledTimes(1);
    } finally {
      await db().delete(organization).where(eq(organization.id, orgId));
      await rm(dir, { recursive: true, force: true });
    }
  });

  it("seeds Everyone with today's access on enable and restores the read policy on disable", async () => {
    const orgId = `grants-on-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`;
    const dir = await mkdtemp(join(tmpdir(), "gj-grants-on-"));
    const configFile = join(dir, "config.yml");
    await writeFile(configFile, CONFIG);
    await db().insert(organization).values({ id: orgId, name: "Grants" });
    const applyLive = vi.fn(async (_update: Record<string, unknown>) => undefined);
    const catalog = vi.fn(async () => new Map([["shop", [
      { schema: "public", table: "orders", columns: ["id", "amount"] },
      { schema: "public", table: "customers", columns: ["id", "email"] },
    ]]]));
    try {
      const enabled = await enableGroupGrants(orgId, null, { configFile, applyLive, catalog });
      expect(enabled).toMatchObject({ enabled: true, changed: true, seededRules: 2, roles: ["og_everyone"] });
      expect(catalog).toHaveBeenCalledWith(["shop"]);
      const on = parse(await readFile(configFile, "utf8"));
      expect(on.sources[0].access.read).toBe("admin");
      expect(on.sources[0].access.grants).toEqual([{ role: "og_everyone", tables: [
        { name: "public.customers", columns: ["id", "email"], filter: "{ account_id: { eq: $account_id } }" },
        { name: "public.orders", columns: ["id", "amount"], filter: "{ account_id: { eq: $account_id } }" },
      ] }]);
      expect((await enableGroupGrants(orgId, null, { configFile, applyLive, catalog })).seededRules).toBe(0);

      expect(await disableGroupGrants(orgId, null, { configFile, applyLive, catalog })).toEqual({ changed: true });
      const off = parse(await readFile(configFile, "utf8"));
      expect(off.sources[0].access).toEqual({ read: "account" });
      expect(off.identity.role_mode).toBe("first");
      expect(applyLive).toHaveBeenCalledTimes(2);
    } finally {
      await db().delete(organization).where(eq(organization.id, orgId));
      await rm(dir, { recursive: true, force: true });
    }
  });
});
