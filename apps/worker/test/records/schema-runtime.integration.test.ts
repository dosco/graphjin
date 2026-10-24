import { mkdtemp, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import pg from "pg";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { buildRecordsPoolConfig } from "@neko/db/records-migrate";
import { RecordsGraphjinSchemaCli } from "@neko/records";
import { RecordsDataPlaneController } from "../../src/records/data-plane-control.js";
import {
  createRecordsSchemaRuntime,
  recordsPostgresConnectionString,
} from "../../src/records/schema-runtime.js";

async function recordsDbReachable(): Promise<boolean> {
  const probe = new pg.Pool(buildRecordsPoolConfig());
  try {
    await probe.query("select 1");
    return true;
  } catch {
    return false;
  } finally {
    await probe.end();
  }
}

const reachable = await recordsDbReachable();
const describeIfLive = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[records-schema-runtime] skipping: records Postgres unavailable.");
}

describeIfLive("records schema worker runtime", () => {
  const database = `records_runtime_${Date.now().toString(36)}_${Math.random()
    .toString(36)
    .slice(2, 7)}`;
  let adminPool: pg.Pool;
  let testPool: pg.Pool;

  beforeAll(async () => {
    adminPool = new pg.Pool(buildRecordsPoolConfig());
    await adminPool.query(`create database ${database}`);
    testPool = new pg.Pool(buildRecordsPoolConfig(process.env, { database }));
  });

  afterAll(async () => {
    if (testPool) await testPool.end();
    if (adminPool) {
      await adminPool.query(`drop database if exists ${database}`);
      await adminPool.end();
    }
  });

  it("boots fail closed, applies an approved app, projects policy, and restores service", async () => {
    const configDirectory = await mkdtemp(join(tmpdir(), "records-runtime-config-"));
    let controller: RecordsDataPlaneController;
    controller = new RecordsDataPlaneController({
      configDirectory,
      baseUrl: "http://records-graphjin:8090",
      timeoutMs: 100,
      pollMs: 1,
      fetch: async () => {
        if ((await controller.requestedState()) !== "ready") {
          throw new Error("connection refused");
        }
        return new Response("ok");
      },
    });

    let physicallyApplied = false;
    const cli = new RecordsGraphjinSchemaCli({
      command: async (_binary, args) => {
        if (args[0] === "version") {
          return { stdout: "GraphJin 3.21.16\n", stderr: "" };
        }
        if (args.includes("diff")) {
          return {
            stdout: physicallyApplied
              ? ""
              : 'CREATE TABLE "support__ticket" ("id" TEXT PRIMARY KEY);\n',
            stderr: "",
          };
        }
        if (args.includes("sync")) {
          await testPool.query(`
            create table support__ticket (
              id text primary key,
              org_id text not null,
              owner_user_id text not null,
              data jsonb not null,
              nk_created_at timestamptz not null,
              nk_created_by text not null,
              nk_updated_at timestamptz not null,
              nk_updated_by text not null,
              nk_action_request_id text not null,
              nk_mutation_id text not null,
              nk_deleted_at timestamptz
            );
          `);
          physicallyApplied = true;
          return { stdout: "applied\n", stderr: "" };
        }
        throw new Error(`unexpected GraphJin command: ${args.join(" ")}`);
      },
    });
    const projectMetadata = vi.fn().mockResolvedValue(undefined);
    const runtime = await createRecordsSchemaRuntime({
      pool: testPool,
      orgId: "org-a",
      configDirectory,
      connectionString: `postgresql://amit@127.0.0.1:55436/${database}`,
      controller,
      cli,
      validateConfig: async () => undefined,
      projectMetadata,
    });

    expect(await controller.requestedState()).toBe("ready");
    expect(runtime.reconciliation).toEqual({ projected: [], failed: [] });
    const initialConfig = await readFile(join(configDirectory, "dev.yml"), "utf8");
    expect(initialConfig).toContain("default_block: true");

    const prepared = await runtime.planner.prepare({
      id: "request-support",
      orgId: "org-a",
      kind: "app_create",
      actorUserId: "admin-1",
      actorRole: "admin",
      payload: {
        app: "support",
        label: "Support desk",
        objects: [
          {
            name: "ticket",
            visibility: "owner",
            name_field: "subject",
            fields: [
              { name: "subject", required: true },
              {
                name: "status",
                kind: "picklist",
                picklist_values: ["new", "closed"],
              },
            ],
          },
        ],
      },
    });
    await runtime.saga.approve("request-support", prepared.change.previewHash);
    await expect(runtime.saga.execute("request-support")).resolves.toMatchObject({
      phase: "projected",
      appId: "support",
    });

    expect(await controller.requestedState()).toBe("ready");
    await expect(
      testPool.query(
        "select status, registry_revision::text as revision from engine.record_app where org_id = 'org-a' and app_id = 'support'",
      ),
    ).resolves.toMatchObject({
      rows: [{ status: "active", revision: "1" }],
    });
    expect(projectMetadata).toHaveBeenCalledWith(
      expect.objectContaining({
        orgId: "org-a",
        registryRevision: "1",
        actorUserId: "admin-1",
        definition: expect.objectContaining({ appId: "support" }),
      }),
    );
    const projectedConfig = await readFile(join(configDirectory, "dev.yml"), "utf8");
    expect(projectedConfig).toContain("name: support__ticket");
    expect(projectedConfig).toContain("owner_user_id");
  });

  it("encodes records connection credentials without exposing ambiguous URL bytes", () => {
    expect(
      recordsPostgresConnectionString({
        RECORDS_PG_HOST: "records-db",
        RECORDS_PG_PORT: "5432",
        RECORDS_PG_USER: "records user",
        RECORDS_PG_PASSWORD: "p@ss:/word",
        RECORDS_PG_DATABASE: "records-db",
      }),
    ).toBe(
      "postgresql://records%20user:p%40ss%3A%2Fword@records-db:5432/records-db",
    );
  });
});
