import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  assertAdditiveRecordsSchemaSql,
  RecordsGraphjinSchemaCli,
  normalizeRecordsGraphjinSchemaPreview,
  recordsSchemaApprovalHash,
  writeRecordsGraphjinDdl,
  type RecordsGraphjinSchemaCommand,
} from "../src/graphjin/schema";

describe("records GraphJin schema boundary", () => {
  it("binds the exact desired revision, catalog, DDL, and SQL bytes", () => {
    const input = {
      desiredRevision: "7",
      catalogRevision: "a".repeat(64),
      graphjinDdl: "type example { id: Text! @id }\n",
      previewSql: "CREATE TABLE example (id TEXT PRIMARY KEY);",
    };
    const hash = recordsSchemaApprovalHash(input);
    expect(hash).toMatch(/^[0-9a-f]{64}$/);
    expect(recordsSchemaApprovalHash(input)).toBe(hash);
    expect(recordsSchemaApprovalHash({ ...input, previewSql: `${input.previewSql}\n` })).not.toBe(
      hash,
    );
  });

  it("refuses non-additive technical previews", () => {
    expect(() => assertAdditiveRecordsSchemaSql("CREATE TABLE safe (id TEXT);")).not.toThrow();
    expect(() => assertAdditiveRecordsSchemaSql("DROP TABLE unsafe;")).toThrow(/non-additive/);
    expect(() =>
      assertAdditiveRecordsSchemaSql("ALTER TABLE safe ALTER COLUMN id TYPE BIGINT;"),
    ).toThrow(/non-additive/);
  });

  it("removes path-dependent CLI diagnostics from approval bytes", () => {
    expect(
      normalizeRecordsGraphjinSchemaPreview(
        "\u001b[35mDEBUG\u001b[0m\tUsing schema DDL: /tmp/random/db.ddl\nCREATE TABLE safe (id TEXT);\n",
      ),
    ).toBe("CREATE TABLE safe (id TEXT);");
  });

  it("canonicalizes GraphJin statement ordering for stable approvals", () => {
    const account = [
      "-- Create table: crm__account",
      'CREATE TABLE "crm__account" ("id" TEXT PRIMARY KEY);',
    ].join("\n");
    const contact = [
      "-- Create table: crm__contact",
      'CREATE TABLE "crm__contact" ("id" TEXT PRIMARY KEY);',
    ].join("\n");

    expect(
      normalizeRecordsGraphjinSchemaPreview(`${contact}\n\n${account}\n`),
    ).toBe(normalizeRecordsGraphjinSchemaPreview(`${account}\n\n${contact}\n`));
  });

  it("uses only the serve db diff/sync invocation", async () => {
    const directory = await mkdtemp(join(tmpdir(), "records-schema-cli-"));
    await writeFile(join(directory, "dev.yml"), "database: {}\n", { mode: 0o600 });
    await writeRecordsGraphjinDdl(directory, "type example { id: Text! @id }\n");
    const calls: string[][] = [];
    const command: RecordsGraphjinSchemaCommand = async (_binary, args) => {
      calls.push(args);
      if (args.includes("diff")) {
        return { stdout: "CREATE TABLE example (id TEXT PRIMARY KEY);\n", stderr: "" };
      }
      return { stdout: "applied\n", stderr: "" };
    };
    const cli = new RecordsGraphjinSchemaCli({ command });

    await expect(cli.diff(directory)).resolves.toBe(
      "CREATE TABLE example (id TEXT PRIMARY KEY);",
    );
    await expect(cli.sync(directory)).resolves.toBeUndefined();
    expect(calls).toEqual([
      ["serve", "db", "diff", "--format", "sql", "--path", directory],
      ["serve", "db", "sync", "--yes", "--path", directory],
    ]);
  });
});
