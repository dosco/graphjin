#!/usr/bin/env tsx
/**
 * Loads the AdventureWorks 2014 OLTP sample into adventureworks-db.
 *
 * Pipeline (faithful to lorint/AdventureWorks-for-Postgres, with TS
 * standing in for the original update_csvs.rb):
 *   1. download Microsoft's AdventureWorks-oltp-install-script.zip into
 *      a cached working dir
 *   2. unzip
 *   3. convert each BCP-formatted CSV to tab-delimited (port of
 *      update_csvs.rb)
 *   4. createdb adventureworks; psql -f db/seeds/dev/adventureworks-install.sql
 *      with cwd set to the CSV dir so install.sql's relative \copy paths
 *      resolve
 *
 * Idempotent: a completion marker table (stamped only after install.sql
 * finishes) short-circuits re-runs; pre-marker installs are recognized by
 * schema+data presence and back-stamped.
 *
 * Env:
 *   PGHOST, PGPORT, PGUSER, PGPASSWORD     — adventureworks-db connection
 *   ADVENTUREWORKS_DB                      — db name (default: adventureworks)
 *   ADVENTUREWORKS_CACHE_DIR               — working dir (default: /cache)
 *   ADVENTUREWORKS_INSTALL_SQL             — install.sql (default: ../../../db/seeds/dev/adventureworks-install.sql)
 */

import { spawnSync } from "node:child_process";
import {
  existsSync,
  mkdirSync,
  readdirSync,
  readFileSync,
  renameSync,
  writeFileSync,
} from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { Client } from "pg";

const __dirname = dirname(fileURLToPath(import.meta.url));

const PGHOST = process.env.PGHOST ?? "localhost";
const PGPORT = Number(process.env.PGPORT ?? 5432);
const PGUSER = process.env.PGUSER ?? "postgres";
const PGPASSWORD = process.env.PGPASSWORD ?? "postgres";
const ADVENTUREWORKS_DB = process.env.ADVENTUREWORKS_DB ?? "adventureworks";
const CACHE_DIR = process.env.ADVENTUREWORKS_CACHE_DIR ?? "/cache";
const INSTALL_SQL_PATH =
  process.env.ADVENTUREWORKS_INSTALL_SQL ??
  resolve(__dirname, "../../../db/seeds/dev/adventureworks-install.sql");

const MS_ZIP_URL =
  "https://github.com/Microsoft/sql-server-samples/releases/download/adventureworks/AdventureWorks-oltp-install-script.zip";
const WORK_DIR = join(CACHE_DIR, "adventureworks");
const ZIP_PATH = join(WORK_DIR, "ms.zip");

const NUL = String.fromCharCode(0);
const CR = String.fromCharCode(13);
const LF = "\n";
const TAB = "\t";

function log(msg: string): void {
  console.log(`[adventureworks] ${msg}`);
}

function run(
  cmd: string,
  args: string[],
  opts: { cwd?: string; env?: NodeJS.ProcessEnv } = {},
): void {
  const result = spawnSync(cmd, args, {
    stdio: "inherit",
    cwd: opts.cwd,
    env: opts.env,
  });
  if (result.status !== 0) {
    throw new Error(`${cmd} ${args.join(" ")} exited with ${result.status}`);
  }
}

async function connect(database: string): Promise<Client> {
  const client = new Client({
    host: PGHOST,
    port: PGPORT,
    user: PGUSER,
    password: PGPASSWORD,
    database,
  });
  await client.connect();
  return client;
}

async function isAlreadyLoaded(): Promise<boolean> {
  const probe = await connect("postgres");
  try {
    const exists = await probe.query<{ exists: boolean }>(
      "select exists(select 1 from pg_database where datname = $1) as exists",
      [ADVENTUREWORKS_DB],
    );
    if (!exists.rows[0]?.exists) return false;
  } finally {
    await probe.end();
  }
  const client = await connect(ADVENTUREWORKS_DB);
  try {
    // Authoritative: the marker written after install.sql completed. A raw
    // table count turned true as soon as the DDL committed — an interrupted
    // load (container kill, upgrade --force-recreate mid-\copy) then skipped
    // forever, leaving AdventureWorks structurally present but empty.
    const marker = await client.query<{ exists: boolean }>(
      `select exists(
         select 1 from information_schema.tables
          where table_schema = 'public' and table_name = '_openneko_aw_load_complete'
       ) as exists`,
    );
    if (marker.rows[0]?.exists) return true;
    // Pre-marker installs: only a schema WITH data counts as loaded (and
    // gets the marker backfilled so the next run takes the fast path).
    const res = await client.query<{ count: string }>(
      `select count(*)::text as count
         from information_schema.tables
        where table_schema in ('person','humanresources','production','purchasing','sales')`,
    );
    if (Number(res.rows[0]?.count ?? "0") < 50) return false;
    const data = await client.query<{ count: string }>(
      "select count(*)::text as count from sales.salesorderheader",
    );
    if (Number(data.rows[0]?.count ?? "0") === 0) return false;
    await markLoadComplete(client);
    return true;
  } catch {
    return false;
  } finally {
    await client.end();
  }
}

async function markLoadComplete(client: Client): Promise<void> {
  await client.query(
    `create table if not exists public._openneko_aw_load_complete (
       loaded_at timestamptz not null default now()
     )`,
  );
  await client.query("insert into public._openneko_aw_load_complete default values");
}

async function createDatabase(): Promise<void> {
  const client = await connect("postgres");
  try {
    const res = await client.query<{ exists: boolean }>(
      "select exists(select 1 from pg_database where datname = $1) as exists",
      [ADVENTUREWORKS_DB],
    );
    if (!res.rows[0]?.exists) {
      await client.query(`create database "${ADVENTUREWORKS_DB}"`);
    }
  } finally {
    await client.end();
  }
}

function ensureMsZip(): void {
  if (existsSync(ZIP_PATH)) {
    log(`re-using cached MS zip at ${ZIP_PATH}`);
    return;
  }
  mkdirSync(WORK_DIR, { recursive: true });
  log(`downloading ${MS_ZIP_URL}`);
  // The whole demo stack (worker and web included) gates on this one-shot,
  // so a transient network blip must not take the product down with it.
  // Download to a temp name and rename only on success, so an interrupted
  // transfer can never be mistaken for a cached zip on the next run.
  const partial = `${ZIP_PATH}.partial`;
  run("curl", [
    "-sSL",
    "--fail",
    "--retry", "5",
    "--retry-delay", "3",
    "--retry-all-errors",
    "-o", partial,
    MS_ZIP_URL,
  ]);
  renameSync(partial, ZIP_PATH);
}

function ensureExtracted(): void {
  // A representative file from the zip — if present, assume already extracted.
  if (existsSync(join(WORK_DIR, "BusinessEntity.csv"))) {
    log(`re-using extracted CSVs in ${WORK_DIR}`);
    return;
  }
  log(`extracting ${ZIP_PATH}`);
  run("unzip", ["-o", "-q", ZIP_PATH, "-d", WORK_DIR]);
}

/**
 * Convert lorint's BCP-formatted CSV to the tab-delimited form install.sql
 * expects. Direct port of update_csvs.rb but tolerant of UTF-8 input
 * (Microsoft's current distribution dropped the UTF-16LE BOM that
 * update_csvs.rb relied on for detection).
 *
 * BCP format produced by SQL Server's bcp utility:
 *   - field separator: "+|"
 *   - record terminator: "&|\n"
 *   - no field-level quoting
 *
 * Output: tab-delimited; fields containing a tab or shaped like XML get
 * double-quoted so Postgres COPY in CSV mode preserves them.
 */
function convertBcpFile(path: string): void {
  const raw = readFileSync(path);
  // Detect UTF-16LE BOM and strip; treat everything else as UTF-8.
  const decoded =
    raw.length >= 2 && raw[0] === 0xff && raw[1] === 0xfe
      ? raw.subarray(2).toString("utf16le")
      : raw[0] === 0xef && raw[1] === 0xbb && raw[2] === 0xbf
        ? raw.subarray(3).toString("utf8")
        : raw.toString("utf8");

  // Normalize universally:
  //   - drop NUL bytes (BCP encodes empty text fields as a literal NUL;
  //     Postgres text/varchar can't store them)
  //   - strip CR (CRLF -> LF; lone CRs would otherwise trip COPY's
  //     "unquoted carriage return found in data" rule)
  const text = decoded.split(NUL).join("").split(CR).join("");

  // BCP detection: if the file contains "+|" anywhere in the first 1KB
  // it's BCP-formatted; otherwise treat as already tab-delimited.
  const head = text.slice(0, 1024);
  const isBcp = head.includes("+|");

  // Convert binary-marker prefixes to Postgres bytea hex literals. Lorint
  // applies each one in only one branch; we apply both in both branches
  // because Microsoft has shuffled which tables use which format.
  const bytea = (s: string): string =>
    s
      .replace(/\|474946383961/g, "|\\\\x474946383961") // GIF photo data
      .replace(/\tE6100000010C/g, "\t\\\\xE6100000010C"); // geospatial

  // Literal "\n" (backslash + n) used to encode an embedded line break
  // inside a field within the converted output.
  const ESCAPED_NEWLINE = "\\n";

  const out: string[] = [];

  if (isBcp) {
    // Lines may not align with records — accumulate until we see "&|"
    // as the record terminator, then emit one tab-delimited row.
    let acc = "";
    for (const line of text.split(LF)) {
      const munged = bytea(line).replace(/"/g, '""');
      if (munged.endsWith("&|")) {
        acc += munged;
        const record = acc.slice(0, -2); // drop the trailing "&|"
        const fields = record.split("+|").map((part) => {
          // Lorint's heuristic: XML-shaped (<...>) or tab-containing
          // fields get double-quoted so COPY-CSV preserves them.
          if (
            part.length >= 2 &&
            part[0] === "<" &&
            part[part.length - 1] === ">"
          ) {
            return `"${part}"`;
          }
          if (part.includes(TAB)) {
            return `"${part}"`;
          }
          return part;
        });
        out.push(fields.join(TAB));
        acc = "";
      } else {
        // Multi-line BCP record: an original line break inside a field
        // becomes a literal "\n" (backslash + n) in the accumulated
        // value — same trick as update_csvs.rb's gsub("\r\n", "\\n").
        acc += munged + ESCAPED_NEWLINE;
      }
    }
  } else {
    // Already tab-delimited. The legacy distribution used "&|" as a
    // record terminator at end-of-line; the newer one drops it but the
    // strip is still safe.
    for (const line of text.split(LF)) {
      const munged = bytea(line).replace(/"/g, '""').replace(/&\|$/, "");
      out.push(munged);
    }
  }

  writeFileSync(path, out.join(LF));
}

function convertAllCsvs(): void {
  log("converting CSVs to tab-delimited form");
  let count = 0;
  for (const name of readdirSync(WORK_DIR)) {
    if (!name.endsWith(".csv")) continue;
    convertBcpFile(join(WORK_DIR, name));
    count++;
  }
  log(`converted ${count} CSV files`);
}

function runInstallSql(): void {
  log(`running install.sql against '${ADVENTUREWORKS_DB}'`);
  run(
    "psql",
    [
      "-v",
      "ON_ERROR_STOP=1",
      "-h",
      PGHOST,
      "-p",
      String(PGPORT),
      "-U",
      PGUSER,
      "-d",
      ADVENTUREWORKS_DB,
      "-f",
      INSTALL_SQL_PATH,
    ],
    {
      // psql resolves the relative \copy paths in install.sql against cwd.
      cwd: WORK_DIR,
      env: { ...process.env, PGPASSWORD },
    },
  );
}

async function main(): Promise<void> {
  if (await isAlreadyLoaded()) {
    log(`'${ADVENTUREWORKS_DB}' already populated; skipping load`);
    return;
  }

  ensureMsZip();
  ensureExtracted();
  convertAllCsvs();
  await createDatabase();
  runInstallSql();
  {
    // Stamp completion only after install.sql (DDL + \copy data) finished.
    const client = await connect(ADVENTUREWORKS_DB);
    try {
      await markLoadComplete(client);
    } finally {
      await client.end();
    }
  }
  log("done");
}

main().catch((err) => {
  console.error("[adventureworks] load failed:", err);
  process.exit(1);
});
