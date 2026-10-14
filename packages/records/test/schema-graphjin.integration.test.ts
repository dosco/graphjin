import pg from "pg";
import { execFile } from "node:child_process";
import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import {
  buildRecordsPoolConfig,
  runRecordsMigrations,
} from "@neko/db/records-migrate";
import { buildRecordsGraphjinConfig } from "../src/graphjin/config";
import {
  assertAdditiveRecordsSchemaSql,
  writeRecordsGraphjinDdl,
} from "../src/graphjin/schema";
import { validateRecordIdentifier } from "../src/naming";
import {
  loadRecordsGraphjinPolicyModel,
  projectRecordsGraphjinRoles,
} from "../src/policy/graphjin";
import {
  buildRecordsGraphjinDdl,
  verifyRecordsGraphjinDdlApplied,
} from "../src/schema/ddl";

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

function runDocker(args: string[]): Promise<{ stdout: string; stderr: string }> {
  return new Promise((resolve, reject) => {
    execFile(
      "docker",
      args,
      {
        timeout: 30_000,
        maxBuffer: 8 * 1024 * 1024,
        env: { ...process.env, NO_COLOR: "1", GO_ENV: "development" },
      },
      (error, stdout, stderr) => {
        if (error) reject(new Error(String(stderr) || error.message));
        else resolve({ stdout: String(stdout), stderr: String(stderr) });
      },
    );
  });
}

const reachable = await recordsDbReachable();
const graphjinImage = process.env.RECORDS_GRAPHJIN_IMAGE;
const describeIfLive = reachable && graphjinImage ? describe : describe.skip;

if (process.env.REQUIRE_GRAPHJIN_INTEGRATION === "1") {
  if (!reachable) throw new Error("required records Postgres is unreachable");
  if (!graphjinImage) throw new Error("required RECORDS_GRAPHJIN_IMAGE is not set");
}

if (!reachable || !graphjinImage) {
  console.warn(
    "[records-schema-graphjin] skipping: records Postgres or RECORDS_GRAPHJIN_IMAGE unavailable.",
  );
}

describeIfLive("records GraphJin live schema integration", () => {
  const database = `records_ddl_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 7)}`;
  let adminPool: pg.Pool;
  let testPool: pg.Pool;

  beforeAll(async () => {
    adminPool = new pg.Pool(buildRecordsPoolConfig());
    await adminPool.query(`create database ${database}`);
    testPool = new pg.Pool(buildRecordsPoolConfig(process.env, { database }));
    await runRecordsMigrations({ pool: testPool });
  });

  afterAll(async () => {
    if (testPool) await testPool.end();
    if (adminPool) {
      await adminPool.query(`drop database if exists ${database}`);
      await adminPool.end();
    }
  });

  it(
    "previews and applies only additive DDL through the pinned CLI",
    async () => {
      const directory = await mkdtemp(join(tmpdir(), "records-live-ddl-"));
      const roles = projectRecordsGraphjinRoles(
        await loadRecordsGraphjinPolicyModel(testPool, "org-a"),
      );
      const poolConfig = buildRecordsPoolConfig(process.env, { database });
      const connection = new URL("postgres://localhost/records");
      connection.hostname = process.env.RECORDS_GRAPHJIN_DB_HOST ?? "host.internal";
      connection.port = String(poolConfig.port);
      connection.username = String(poolConfig.user);
      connection.password = String(poolConfig.password ?? "");
      connection.pathname = `/${database}`;
      connection.searchParams.set("sslmode", "disable");
      const config = buildRecordsGraphjinConfig({
        orgId: "org-a",
        roles,
        database: {
          connectionString: connection.toString(),
        },
        jwt: { secret: "schema-jwt-secret-that-is-at-least-thirty-two-bytes" },
        secretKey: "schema-cursor-secret-that-is-at-least-thirty-two-bytes",
      });
      await writeFile(join(directory, "dev.yml"), config, { mode: 0o600 });
      const desiredObjects = [
        {
          appId: "equipment",
          apiName: validateRecordIdentifier("loan"),
          tableName: validateRecordIdentifier("equipment__loan"),
          nameField: validateRecordIdentifier("name"),
          visibility: "owner",
          fields: [
            { columnName: validateRecordIdentifier("name"), kind: "text", required: true },
            {
              columnName: validateRecordIdentifier("replacement_cost"),
              kind: "currency",
              required: false,
            },
          ],
        },
      ];
      await writeRecordsGraphjinDdl(directory, buildRecordsGraphjinDdl(desiredObjects));

      const baseArgs = [
        "run",
        "--rm",
        "--entrypoint",
        "graphjin",
        "-e",
        "NO_COLOR=1",
        "-e",
        "GO_ENV=development",
        "-v",
        `${directory}:/config:ro`,
        graphjinImage!,
        "serve",
        "db",
      ];
      const preview = await runDocker([
        ...baseArgs,
        "diff",
        "--format",
        "sql",
        "--path",
        "/config",
      ]);
      assertAdditiveRecordsSchemaSql(preview.stdout);
      expect(preview.stdout).toContain('CREATE TABLE "equipment__loan"');
      expect(preview.stdout).not.toMatch(/\bDROP\b/i);

      await runDocker([...baseArgs, "sync", "--yes", "--path", "/config"]);
      const relation = await testPool.query<{
        column_name: string;
        data_type: string;
        is_nullable: string;
      }>(`
        select column_name, data_type, is_nullable
        from information_schema.columns
        where table_schema = 'public' and table_name = 'equipment__loan'
        order by ordinal_position
      `);
      expect(relation.rows.map((row) => row.column_name)).not.toContain("name");
      expect(relation.rows).toEqual(
        expect.arrayContaining([
          { column_name: "id", data_type: "text", is_nullable: "NO" },
          { column_name: "data", data_type: "jsonb", is_nullable: "NO" },
          {
            column_name: "nk_deleted_at",
            data_type: "timestamp with time zone",
            is_nullable: "YES",
          },
        ]),
      );
      await expect(verifyRecordsGraphjinDdlApplied(testPool, desiredObjects)).resolves.toEqual({
        applied: true,
        problems: [],
      });
      const repeated = await runDocker([
        ...baseArgs,
        "diff",
        "--format",
        "sql",
        "--path",
        "/config",
      ]);
      expect(repeated.stdout).not.toContain('CREATE TABLE "equipment__loan"');

      await writeRecordsGraphjinDdl(
        directory,
        "# No live records objects. Archived tables remain physically retained.\n",
      );
      const archived = await runDocker([
        ...baseArgs,
        "diff",
        "--format",
        "sql",
        "--path",
        "/config",
      ]);
      expect(archived.stdout).not.toMatch(/\bDROP\b/i);
    },
    40_000,
  );
});
