import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { mkdtempSync } from "node:fs";
import { rm, writeFile } from "node:fs/promises";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";

const configHome = await vi.hoisted(async () => {
  const [{ mkdtempSync: make }, os, path] = await Promise.all([import("node:fs"), import("node:os"), import("node:path")]);
  const home = make(path.join(os.tmpdir(), "gj-grants-xdg-"));
  process.env.XDG_CONFIG_HOME = home;
  return home;
});

import {
  addLocalGroupMember,
  and,
  app_user,
  builtinGroupId,
  createUserGroup,
  db,
  eq,
  getGroupGrantsEnabled,
  graphjinGroupClaims,
  grantItem,
  organization,
  pool,
  revokeItem,
  upsertDataAccessRule,
  user_group,
} from "@neko/db";
import { applyGraphjinConfigUpdate, graphjinSigningSecretB64, mintGraphjinToken } from "@neko/llm/graphjin";
import { disableGroupGrants, enableGroupGrants, fetchGraphjinColumnCatalog } from "../src/graphjin/group-grants-service";

/**
 * Plan step 3 against a GraphJin binary with source grants (dosco/graphjin#640):
 * database rules, the enable flow, config generation, the live gj_config apply and the run
 * token claims. Set OPENNEKO_TEST_GRAPHJIN_BIN to run it.
 */
const bin = process.env.OPENNEKO_TEST_GRAPHJIN_BIN;
const reachable = bin ? await pool().query("select 1").then(() => true, () => false) : false;

async function freePort(): Promise<number> {
  return new Promise((resolve) => {
    const server = createServer();
    server.listen(0, "127.0.0.1", () => {
      const port = (server.address() as { port: number }).port;
      server.close(() => resolve(port));
    });
  });
}

(reachable ? describe : describe.skip)("group data grants through GraphJin (live)", () => {
  const orgId = `gj-live-${Date.now().toString(36)}`;
  const dir = mkdtempSync(join(tmpdir(), "gj-grants-worker-"));
  const configFile = join(dir, "agentic.yml");
  let child: ChildProcess | null = null;
  let endpoint = "";
  let logs = "";

  async function startGraphjin(): Promise<void> {
    logs = "";
    child = spawn(bin!, ["serve", "--path", dir], { env: { ...process.env, GO_ENV: "agentic" }, stdio: ["ignore", "pipe", "pipe"] });
    child.stdout?.on("data", (chunk) => (logs += String(chunk)));
    child.stderr?.on("data", (chunk) => (logs += String(chunk)));
    const deadline = Date.now() + 60_000;
    for (;;) {
      if (child.exitCode !== null) throw new Error(`GraphJin exited:\n${logs}`);
      const up = await fetch(endpoint, { method: "POST", headers: { "content-type": "application/json" }, body: '{"query":"{ __typename }"}' }).then(() => true, () => false);
      if (up) return;
      if (Date.now() > deadline) throw new Error(`GraphJin did not start:\n${logs}`);
      await new Promise((resolve) => setTimeout(resolve, 250));
    }
  }

  async function stopGraphjin(): Promise<void> {
    const running = child;
    child = null;
    if (!running || running.exitCode !== null) return;
    await new Promise<void>((resolve) => {
      running.once("exit", () => resolve());
      running.kill("SIGTERM");
    });
  }

  // Changes reach the running GraphJin through gj_config; nothing restarts.
  const deps = {
    configFile,
    applyLive: async (update: Record<string, unknown>) => {
      await applyGraphjinConfigUpdate({
        endpoint,
        headers: { authorization: `Bearer ${mintGraphjinToken({ orgId, userId: "group-grants", role: "admin", ttlSeconds: 120 })}` },
        update,
      });
    },
    catalog: (sources: string[]) => fetchGraphjinColumnCatalog(orgId, endpoint, sources),
  };

  async function orders(userId: string, fields: string) {
    const claims = (await getGroupGrantsEnabled(orgId)) ? await graphjinGroupClaims(orgId, userId) : null;
    const token = mintGraphjinToken({ orgId, userId, role: "member", ...(claims ? { groupRoles: claims.roles, groups: claims.groups } : {}) });
    const response = await fetch(endpoint, {
      method: "POST",
      headers: { "content-type": "application/json", authorization: `Bearer ${token}` },
      body: JSON.stringify({ query: `query { orders(order_by: { id: asc }) { ${fields} } }` }),
    });
    const body = (await response.json()) as { data?: { orders?: Array<Record<string, unknown>> }; errors?: Array<{ message: string }> };
    return body.errors?.length ? { error: body.errors.map((e) => e.message).join("; ") } : { rows: body.data?.orders ?? [] };
  }

  beforeAll(async () => {
    const port = await freePort();
    endpoint = `http://127.0.0.1:${port}/api/v1/graphql`;
    const dbPath = join(dir, "shop.db");
    execFileSync("sqlite3", [dbPath, `CREATE TABLE orders (id INTEGER PRIMARY KEY, account_id TEXT NOT NULL, region TEXT NOT NULL, amount INTEGER NOT NULL);
      INSERT INTO orders VALUES (1, '${orgId}', 'emea', 10), (2, '${orgId}', 'apac', 20), (3, 'other', 'emea', 30);`]);
    await writeFile(configFile, `app_name: OpenNeko worker group grants test
mode: agentic
host_port: 127.0.0.1:${port}
production: true
disable_production_security: true
log_level: warn
mcp:
  allow_config_updates: true
artifacts:
  enabled: false
watches:
  enabled: false
  runner: off
tasks:
  enabled: false
system:
  capabilities:
    catalog.read: true
    config.read: true
    config.write: true
  root_access:
    gj_catalog: authenticated
    gj_config: admin
auth:
  type: jwt
  jwt:
    secret: "${graphjinSigningSecretB64(orgId)}"
identity:
  user_id_claim: sub
  role_claims: [role]
  namespace_claim: org_id
  admin_roles: [admin]
roles:
  - name: member
sources:
  - name: shop
    kind: database
    type: sqlite
    path: ${dbPath}
    default: true
    read_only: true
    access:
      read: account
      namespace_column: account_id
`);
    await db().insert(organization).values({ id: orgId, name: "GraphJin live" });
    await db().insert(app_user).values([
      { id: `${orgId}-both`, org_id: orgId, email: "both@x.test" },
      { id: `${orgId}-finance`, org_id: orgId, email: "finance@x.test" },
      { id: `${orgId}-none`, org_id: orgId, email: "none@x.test" },
    ]);
    await startGraphjin();
  }, 120_000);

  afterAll(async () => {
    await stopGraphjin();
    await db().delete(organization).where(eq(organization.id, orgId));
    await rm(dir, { recursive: true, force: true });
    await rm(configHome, { recursive: true, force: true });
  });

  it("keeps today's access when turned on, then applies group rules in union", async () => {
    expect(await orders(`${orgId}-none`, "id amount")).toEqual({ rows: [{ id: 1, amount: 10 }, { id: 2, amount: 20 }] });

    const enabled = await enableGroupGrants(orgId, null, deps);
    expect(enabled).toMatchObject({ enabled: true, changed: true, roles: ["og_everyone"], seededRules: 1 });
    expect(await orders(`${orgId}-none`, "id amount")).toEqual({ rows: [{ id: 1, amount: 10 }, { id: 2, amount: 20 }] });

    const everyone = await builtinGroupId(orgId, "everyone");
    const finance = await createUserGroup(orgId, { name: "Finance" });
    const sales = await createUserGroup(orgId, { name: "Sales" });
    await revokeItem(orgId, { groupId: everyone, itemType: "data_source", itemId: "*" });
    for (const group of [finance, sales]) await grantItem(orgId, { groupId: group.id, itemType: "data_source", itemId: "shop" });
    await upsertDataAccessRule(orgId, { groupId: finance.id, source: "shop", tableName: "orders", columns: ["id", "region", "amount"], rowFilter: { column: "region", op: "eq", value: "emea" } });
    await upsertDataAccessRule(orgId, { groupId: sales.id, source: "shop", tableName: "orders", columns: ["id", "region"], rowFilter: null });
    await addLocalGroupMember(orgId, finance.id, `${orgId}-both`);
    await addLocalGroupMember(orgId, sales.id, `${orgId}-both`);
    await addLocalGroupMember(orgId, finance.id, `${orgId}-finance`);

    const applied = await enableGroupGrants(orgId, null, deps);
    expect(applied.roles).toEqual(["og_finance", "og_sales"]);

    expect(await orders(`${orgId}-finance`, "id amount")).toEqual({ rows: [{ id: 1, amount: 10 }] });
    expect(await orders(`${orgId}-both`, "id region")).toEqual({ rows: [{ id: 1, region: "emea" }, { id: 2, region: "apac" }] });
    expect((await orders(`${orgId}-both`, "id amount")).error).toBeTruthy();
    expect((await orders(`${orgId}-none`, "id")).error).toBeTruthy();
  }, 180_000);

  it("gives a new member access without a GraphJin restart", async () => {
    const pid = child?.pid;
    const [sales] = await db()
      .select({ id: user_group.id })
      .from(user_group)
      .where(and(eq(user_group.org_id, orgId), eq(user_group.slug, "sales")));
    await addLocalGroupMember(orgId, sales!.id, `${orgId}-none`);
    expect(await orders(`${orgId}-none`, "id region")).toEqual({ rows: [{ id: 1, region: "emea" }, { id: 2, region: "apac" }] });
    expect(child?.pid).toBe(pid);
  }, 60_000);

  it("restores the previous read policy when turned off", async () => {
    expect(await disableGroupGrants(orgId, null, deps)).toEqual({ changed: true });
    expect(await orders(`${orgId}-finance`, "id amount")).toEqual({ rows: [{ id: 1, amount: 10 }, { id: 2, amount: 20 }] });
  }, 120_000);
});
