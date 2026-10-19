import { execFile } from "node:child_process";
import { createHash, randomUUID } from "node:crypto";
import { mkdir, open, rename, unlink } from "node:fs/promises";
import { join } from "node:path";
import { RECORDS_GRAPHJIN_CONFIG_FILENAME } from "./config";

export const RECORDS_GRAPHJIN_DDL_FILENAME = "db.ddl";

const FILE_MODE = 0o600;
const DIRECTORY_MODE = 0o700;
const COMMAND_TIMEOUT_MS = 60_000;

export type RecordsSchemaApprovalArtifacts = {
  desiredRevision: string;
  catalogRevision: string;
  graphjinDdl: string;
  previewSql: string;
};

export type RecordsGraphjinSchemaCommand = (
  binary: string,
  args: string[],
  options: { timeoutMs: number },
) => Promise<{ stdout: string; stderr: string }>;

function defaultCommand(
  binary: string,
  args: string[],
  options: { timeoutMs: number },
): Promise<{ stdout: string; stderr: string }> {
  return new Promise((resolve, reject) => {
    execFile(
      binary,
      args,
      {
        timeout: options.timeoutMs,
        maxBuffer: 8 * 1024 * 1024,
        env: {
          ...process.env,
          GO_ENV: "development",
          NO_COLOR: "1",
        },
      },
      (error, stdout, stderr) => {
        if (error) {
          reject(new Error(`GraphJin schema command failed: ${binary} ${args.join(" ")}`));
          return;
        }
        resolve({ stdout: String(stdout), stderr: String(stderr) });
      },
    );
  });
}

export function recordsSchemaApprovalHash(input: RecordsSchemaApprovalArtifacts): string {
  if (!/^\d+$/.test(input.desiredRevision) || input.desiredRevision === "0") {
    throw new Error("records schema desired revision must be a positive integer");
  }
  if (!/^[0-9a-f]{64}$/.test(input.catalogRevision)) {
    throw new Error("records schema catalog revision must be a SHA-256 hash");
  }
  if (!input.graphjinDdl.trim()) throw new Error("records schema approval requires GraphJin DDL");
  const artifact = JSON.stringify({
    format: "openneko.records.schema-approval.v1",
    desiredRevision: input.desiredRevision,
    catalogRevision: input.catalogRevision,
    graphjinDdl: input.graphjinDdl,
    previewSql: input.previewSql,
  });
  return createHash("sha256").update(artifact, "utf8").digest("hex");
}

/** Reject any future GraphJin regression that widens the additive-only gate. */
export function assertAdditiveRecordsSchemaSql(sql: string): void {
  const forbidden = [
    /\bdrop\b/i,
    /\btruncate\b/i,
    /\brename\b/i,
    /\balter\s+(?:table\s+)?[^;]+\btype\b/i,
    /\balter\s+(?:table\s+)?[^;]+\bset\s+not\s+null\b/i,
    /\bdelete\s+from\b/i,
    /\bupdate\s+[^;]+\bset\b/i,
    /\binsert\s+into\b/i,
  ];
  if (forbidden.some((pattern) => pattern.test(sql))) {
    throw new Error("records GraphJin schema preview contains a non-additive statement");
  }
}

/**
 * Remove CLI diagnostics and canonicalize independent statement blocks.
 * GraphJin may emit the same additive diff in a different table order across
 * consecutive invocations; approval bytes must represent the statement set,
 * not Go map iteration order.
 */
export function normalizeRecordsGraphjinSchemaPreview(output: string): string {
  const normalized = output
    .replace(/\u001b\[[0-9;]*m/g, "")
    .split(/\r?\n/)
    .filter((line) => !/^DEBUG\s+Using schema DDL: /.test(line))
    .join("\n")
    .trim();
  return normalized
    .split(/\n\s*\n+/)
    .map((block) => block.trim())
    .filter(Boolean)
    .sort((left, right) => left.localeCompare(right))
    .join("\n\n");
}

/** Atomically persist the exact DDL bytes consumed by diff and sync. */
export async function writeRecordsGraphjinDdl(
  configDirectory: string,
  ddl: string,
): Promise<{ file: string; sha256: string }> {
  if (!ddl.trim()) throw new Error("records GraphJin DDL cannot be empty");
  await mkdir(configDirectory, { recursive: true, mode: DIRECTORY_MODE });
  const file = join(configDirectory, RECORDS_GRAPHJIN_DDL_FILENAME);
  const staging = join(configDirectory, `.db-${randomUUID()}.ddl`);
  const handle = await open(staging, "wx", FILE_MODE);
  try {
    await handle.writeFile(ddl, "utf8");
    await handle.sync();
  } finally {
    await handle.close();
  }
  try {
    await rename(staging, file);
  } catch (error) {
    await unlink(staging).catch(() => undefined);
    throw error;
  }
  return {
    file,
    sha256: createHash("sha256").update(ddl, "utf8").digest("hex"),
  };
}

// The graphjin binary is the openneko binary's graphjin role, built from the
// same commit as this package, so no version check is needed.
export class RecordsGraphjinSchemaCli {
  constructor(
    private readonly options: {
      binary?: string;
      timeoutMs?: number;
      command?: RecordsGraphjinSchemaCommand;
    } = {},
  ) {}

  private async command(args: string[]): Promise<{ stdout: string; stderr: string }> {
    return (this.options.command ?? defaultCommand)(
      this.options.binary ?? "graphjin",
      args,
      { timeoutMs: this.options.timeoutMs ?? COMMAND_TIMEOUT_MS },
    );
  }

  private async verifyDirectory(configDirectory: string): Promise<void> {
    const configFile = join(configDirectory, RECORDS_GRAPHJIN_CONFIG_FILENAME);
    const ddlFile = join(configDirectory, RECORDS_GRAPHJIN_DDL_FILENAME);
    const missing = await Promise.all(
      [configFile, ddlFile].map(async (file) => {
        try {
          const handle = await open(file, "r");
          await handle.close();
          return null;
        } catch {
          return file;
        }
      }),
    );
    const missingFiles = missing.filter((file): file is string => file !== null);
    if (missingFiles.length > 0) {
      throw new Error(`records GraphJin schema directory is incomplete: ${missingFiles.join(", ")}`);
    }
  }

  async diff(configDirectory: string): Promise<string> {
    await this.verifyDirectory(configDirectory);
    // In 3.18.42 lifecycle commands moved below `serve`; keep this invocation
    // centralized because GraphJin has moved it between releases before.
    const result = await this.command([
      "serve",
      "db",
      "diff",
      "--format",
      "sql",
      "--path",
      configDirectory,
    ]);
    const sql = normalizeRecordsGraphjinSchemaPreview(result.stdout);
    assertAdditiveRecordsSchemaSql(sql);
    return sql;
  }

  async sync(configDirectory: string): Promise<void> {
    await this.verifyDirectory(configDirectory);
    await this.command([
      "serve",
      "db",
      "sync",
      "--yes",
      "--path",
      configDirectory,
    ]);
  }
}
