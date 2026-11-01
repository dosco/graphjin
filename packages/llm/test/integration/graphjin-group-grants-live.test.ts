import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { _resetSecretKeyCacheForTesting } from "@neko/secret-crypt";
import { applyGroupGrantsToConfig, buildGroupGrantsModel } from "../../src/graphjin/group-grants";
import { graphjinSigningSecretB64, mintGraphjinToken } from "../../src/graphjin/token";

/**
 * Runs a GraphJin binary that supports source grants (dosco/graphjin#640)
 * with a config written by the OpenNeko generator, and queries it with
 * OpenNeko tokens. Set OPENNEKO_TEST_GRAPHJIN_BIN to run it.
 */
const bin = process.env.OPENNEKO_TEST_GRAPHJIN_BIN;
const ORG = "org-live";

async function freePort(): Promise<number> {
  return new Promise((resolve) => {
    const server = createServer();
    server.listen(0, "127.0.0.1", () => {
      const port = (server.address() as { port: number }).port;
      server.close(() => resolve(port));
    });
  });
}

(bin ? describe : describe.skip)("GraphJin group grants (live)", () => {
  let dir: string;
  let child: ChildProcess;
  let endpoint: string;
  let logs = "";

  beforeAll(async () => {
    dir = await mkdtemp(join(tmpdir(), "gj-grants-live-"));
    process.env.XDG_CONFIG_HOME = dir;
    _resetSecretKeyCacheForTesting();
    const port = await freePort();
    endpoint = `http://127.0.0.1:${port}/api/v1/graphql`;

    const dbPath = join(dir, "shop.db");
    execFileSync("sqlite3", [dbPath, `CREATE TABLE orders (id INTEGER PRIMARY KEY, account_id TEXT NOT NULL, region TEXT NOT NULL, amount INTEGER NOT NULL);
      INSERT INTO orders VALUES (1, '${ORG}', 'emea', 10), (2, '${ORG}', 'apac', 20), (3, 'other', 'emea', 30);`]);

    const base = `app_name: OpenNeko group grants test
mode: agentic
host_port: 127.0.0.1:${port}
production: true
disable_production_security: true
log_level: warn
auth:
  type: jwt
  jwt:
    secret: "${graphjinSigningSecretB64(ORG)}"
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
`;
    const model = buildGroupGrantsModel({
      groups: [{ slug: "finance", name: "Finance" }, { slug: "sales", name: "Sales" }],
      rules: [
        { groupSlug: "finance", groupName: "Finance", source: "shop", tableSchema: "", tableName: "orders", columns: ["id", "region", "amount"], rowFilter: { column: "region", op: "eq", value: "emea" } },
        { groupSlug: "sales", groupName: "Sales", source: "shop", tableSchema: "", tableName: "orders", columns: ["id", "region"], rowFilter: null },
      ],
      apiOperationHolders: new Map(),
    });
    await writeFile(join(dir, "agentic.yml"), applyGroupGrantsToConfig(base, model, { shop: "account" }).content);

    child = spawn(bin!, ["serve", "--path", dir], { env: { ...process.env, GO_ENV: "agentic" }, stdio: ["ignore", "pipe", "pipe"] });
    child.stdout?.on("data", (chunk) => (logs += String(chunk)));
    child.stderr?.on("data", (chunk) => (logs += String(chunk)));
    const deadline = Date.now() + 60_000;
    for (;;) {
      if (child.exitCode !== null) throw new Error(`GraphJin exited:\n${logs}`);
      const ok = await fetch(endpoint, { method: "POST", headers: { "content-type": "application/json" }, body: '{"query":"{ __typename }"}' }).then(() => true, () => false);
      if (ok) break;
      if (Date.now() > deadline) throw new Error(`GraphJin did not start:\n${logs}`);
      await new Promise((resolve) => setTimeout(resolve, 250));
    }
  }, 90_000);

  afterAll(async () => {
    child?.kill("SIGTERM");
    await rm(dir, { recursive: true, force: true });
    _resetSecretKeyCacheForTesting();
  });

  async function query(groupRoles: string[], gql: string, role = "member") {
    const token = mintGraphjinToken({ orgId: ORG, userId: "u-1", role, groupRoles, groups: groupRoles.map((r) => r.replace(/^og_/, "")) });
    const response = await fetch(endpoint, {
      method: "POST",
      headers: { "content-type": "application/json", authorization: `Bearer ${token}` },
      body: JSON.stringify({ query: gql }),
    });
    return (await response.json()) as { data?: Record<string, Array<Record<string, unknown>>>; errors?: Array<{ message: string }> };
  }

  const rows = async (groupRoles: string[], fields: string, role = "member") => {
    const result = await query(groupRoles, `query { orders(order_by: { id: asc }) { ${fields} } }`, role);
    if (result.errors?.length) return { error: result.errors.map((e) => e.message).join("; ") };
    return { rows: result.data?.orders ?? [] };
  };

  it("applies one group's columns, row filter and account filter", async () => {
    expect(await rows(["og_finance"], "id amount")).toEqual({ rows: [{ id: 1, amount: 10 }] });
    expect(await rows(["og_sales"], "id region")).toEqual({ rows: [{ id: 1, region: "emea" }, { id: 2, region: "apac" }] });
    expect((await rows(["og_sales"], "id amount")).error).toBeTruthy();
  });

  it("merges two groups without over-granting", async () => {
    const both = await rows(["og_finance", "og_sales"], "id region");
    expect(both).toEqual({ rows: [{ id: 1, region: "emea" }, { id: 2, region: "apac" }] });
    expect((await rows(["og_finance", "og_sales"], "id amount")).error).toBeTruthy();

    const single = [...((await rows(["og_finance"], "id region")).rows ?? []), ...((await rows(["og_sales"], "id region")).rows ?? [])];
    const allowedCells = new Set(single.flatMap((row) => Object.entries(row).map(([k, v]) => `${row.id}:${k}:${String(v)}`)));
    for (const row of both.rows ?? []) {
      for (const [key, value] of Object.entries(row)) expect(allowedCells.has(`${row.id}:${key}:${String(value)}`)).toBe(true);
    }
  });

  it("denies a member without group roles and keeps admin access", async () => {
    expect((await rows([], "id")).error).toBeTruthy();
    expect(await rows([], "id", "admin")).toEqual({ rows: [{ id: 1 }, { id: 2 }, { id: 3 }] });
  });

  it("accepts the admin catalog filters: one condition bare, two joined with and", async () => {
    const databases = await query([], 'query { gj_catalog(where: { kind: { eq: "database" } }, limit: 10) { name } }', "admin");
    expect(databases.errors).toBeUndefined();
    expect(databases.data?.gj_catalog?.map((d) => d.name)).toContain("shop");
    const columns = await query([], 'query { gj_catalog(where: { and: [{ kind: { eq: "column" } }, { database_name: { eq: "shop" } }] }, limit: 10) { column_name } }', "admin");
    expect(columns.errors).toBeUndefined();
    expect(columns.data?.gj_catalog?.length).toBeGreaterThan(0);
  });

  it("serves the column catalog by source name with offset paging", async () => {
    const page = (offset: number) =>
      query([], `query { gj_catalog(where: { kind: { eq: "column" } }, limit: 2, offset: ${offset}, order_by: { id: asc }) { database_name table_name column_name } }`, "admin");
    const first = await page(0);
    const second = await page(2);
    expect(first.errors).toBeUndefined();
    const all = [...(first.data?.gj_catalog ?? []), ...(second.data?.gj_catalog ?? [])];
    expect(all.length).toBe(4);
    expect(new Set(all.map((c) => c.database_name))).toEqual(new Set(["shop"]));
    expect(all.map((c) => c.column_name).sort()).toEqual(["account_id", "amount", "id", "region"]);
  });
});
