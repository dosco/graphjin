import { execFile } from "node:child_process";
import { createHash, randomUUID } from "node:crypto";
import {
  mkdir,
  open,
  readFile,
  rename,
  rm,
  stat,
  unlink,
} from "node:fs/promises";
import { hostname } from "node:os";
import { basename, dirname, join } from "node:path";
import { stringify } from "yaml";
import {
  RECORD_DATA_COLUMN,
  RECORD_GRAPHJIN_ROLES,
  type GraphjinRole,
} from "../policy/graphjin";
import { validateRecordIdentifier } from "../naming";

export const RECORDS_GRAPHJIN_VERSION = "3.21.13";
export const RECORDS_GRAPHJIN_CONFIG_FILENAME = "dev.yml";
export const RECORDS_WATCH_WEBHOOK_SECRET_FILENAME = ".records-watch-webhook-secret";

const CONFIG_MODE = 0o600;
const LOCK_MODE = 0o600;
const DIRECTORY_MODE = 0o700;
const VALIDATION_TIMEOUT_MS = 30_000;
const RECORDS_CONFIG_LOCK_STALE_MS = 10 * 60_000;
const RECORDS_CONFIG_LOCK_WAIT_MS = 60_000;
const RECORDS_CONFIG_LOCK_POLL_MS = 250;

export type RecordsGraphjinDatabaseConfig = {
  connectionString: string;
  statementTimeoutMs?: number;
  poolSize?: number;
  maxConnections?: number;
};

export type RecordsGraphjinJwtConfig = {
  secret: string;
  audience?: string;
  issuer?: string;
};

export type BuildRecordsGraphjinConfigInput = {
  orgId: string;
  roles: GraphjinRole[];
  database: RecordsGraphjinDatabaseConfig;
  jwt: RecordsGraphjinJwtConfig;
  secretKey: string;
  rateLimit?: { rate: number; bucket: number };
  defaultLimit?: number;
};

export type BuildRecordsWatchGraphjinConfigInput = {
  orgId: string;
  database: RecordsGraphjinDatabaseConfig;
  jwt: RecordsGraphjinJwtConfig;
  secretKey: string;
  watchWebhookAllow?: string[];
};

export type RecordsGraphjinConfigValidator = (input: {
  configDirectory: string;
  configFile: string;
}) => Promise<void>;

function validatePositiveInteger(value: number, label: string): number {
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new Error(`${label} must be a positive integer`);
  }
  return value;
}

function validateOrgIdForRoleQuery(orgId: string): string {
  const value = orgId.trim();
  if (!value) throw new Error("records GraphJin config requires an org id");
  if (value !== orgId) {
    throw new Error("records GraphJin org id cannot have leading or trailing whitespace");
  }
  // GraphJin rejects non-ASCII roles_query text. Control characters would
  // also make a generated SQL string ambiguous, so reject rather than encode.
  if (!/^[\x20-\x7e]+$/.test(value)) {
    throw new Error("records GraphJin org id must contain printable ASCII characters");
  }
  return value;
}

function sqlLiteral(value: string): string {
  return `'${value.replaceAll("'", "''")}'`;
}

export function buildRecordsGraphjinRoleQuery(orgId: string): string {
  const safeOrgId = validateOrgIdForRoleQuery(orgId);
  return (
    "SELECT role FROM engine.actor " +
    `WHERE user_id = $user_id AND org_id = ${sqlLiteral(safeOrgId)} ` +
    "AND disabled_at IS NULL"
  );
}

function postgresConnectionConfig(
  connectionString: string,
  timeoutMs: number,
): { connectionString: string; databaseName: string } {
  const value = connectionString.trim();
  if (!value) throw new Error("records GraphJin database connection string is required");
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new Error("records GraphJin database connection string must be a Postgres URL");
  }
  if (url.protocol !== "postgres:" && url.protocol !== "postgresql:") {
    throw new Error("records GraphJin database connection string must use postgres://");
  }
  const databaseName = decodeURIComponent(url.pathname.replace(/^\//, ""));
  if (!databaseName || databaseName.includes("/")) {
    throw new Error("records GraphJin database connection string must name one database");
  }
  const timeout = validatePositiveInteger(timeoutMs, "statement timeout");
  const currentOptions = url.searchParams.get("options")?.trim() ?? "";
  const withoutStatementTimeout = currentOptions
    .split(/\s+-c\s+/)
    .filter((entry) => entry && !entry.startsWith("statement_timeout="))
    .map((entry, index) => (index === 0 && !currentOptions.startsWith("-c ") ? entry : `-c ${entry}`))
    .join(" ")
    .trim();
  url.searchParams.set(
    "options",
    `${withoutStatementTimeout}${withoutStatementTimeout ? " " : ""}-c statement_timeout=${timeout}`,
  );
  return { connectionString: url.toString(), databaseName };
}

function relationKey(table: { schema: string; name: string }): string {
  return `${table.schema}\u0000${table.name}`;
}

function assertExhaustiveRoles(roles: GraphjinRole[]): void {
  const expectedRoles = new Set<string>(RECORD_GRAPHJIN_ROLES);
  const seenRoles = new Set<string>();
  let expectedRelations: string[] | null = null;

  for (const role of roles) {
    if (!expectedRoles.has(role.name) || seenRoles.has(role.name)) {
      throw new Error(`invalid or duplicate records GraphJin role: ${role.name}`);
    }
    seenRoles.add(role.name);
    const expectedMatch =
      role.name === "member" || role.name === "admin" || role.name === "service"
        ? `role = '${role.name}'`
        : undefined;
    if (role.match !== expectedMatch) {
      throw new Error(`records GraphJin role ${role.name} has an invalid match expression`);
    }
    for (const table of role.tables) {
      validateRecordIdentifier(table.schema);
      validateRecordIdentifier(table.name);
    }
    const relations = role.tables.map(relationKey).sort();
    if (new Set(relations).size !== relations.length) {
      throw new Error(`records GraphJin role ${role.name} contains a duplicate table`);
    }
    if (expectedRelations === null) expectedRelations = relations;
    else if (expectedRelations.join("\u0000") !== relations.join("\u0000")) {
      throw new Error(`records GraphJin role ${role.name} does not cover the live catalog`);
    }

    for (const table of role.tables) {
      for (const operation of ["query", "insert", "update", "upsert", "delete"] as const) {
        const rule = table[operation];
        if (!rule || typeof rule.block !== "boolean") {
          throw new Error(
            `records GraphJin role ${role.name} omits ${table.schema}.${table.name}.${operation}`,
          );
        }
        if (!rule.block && (!rule.columns || rule.columns.length === 0)) {
          throw new Error(
            `records GraphJin role ${role.name} has an unrestricted empty allowlist for ${table.schema}.${table.name}.${operation}`,
          );
        }
        for (const column of rule.columns ?? []) validateRecordIdentifier(column);
        for (const column of Object.keys(rule.presets ?? {})) {
          validateRecordIdentifier(column);
        }
      }
    }
  }
  if (seenRoles.size !== expectedRoles.size) {
    const missing = [...expectedRoles].filter((role) => !seenRoles.has(role));
    throw new Error(`records GraphJin config is missing roles: ${missing.join(", ")}`);
  }
}

/** Build the complete standalone records config. It intentionally has no sources key. */
/**
 * Record documents merge on update, so a partial write changes only the keys
 * it sends. Object tables are the ones whose service role writes the
 * document column.
 */
function recordDocumentTables(roles: GraphjinRole[]) {
  const tables = new Map<string, { name: string; schema: string }>();
  for (const role of roles) {
    for (const table of role.tables) {
      if (table.update.columns?.includes(RECORD_DATA_COLUMN)) {
        tables.set(relationKey(table), { name: table.name, schema: table.schema });
      }
    }
  }
  return [...tables.entries()]
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([, table]) => ({
      name: table.name,
      schema: table.schema,
      columns: [{ name: RECORD_DATA_COLUMN, merge_on_update: true }],
    }));
}

export function buildRecordsGraphjinConfig(input: BuildRecordsGraphjinConfigInput): string {
  assertExhaustiveRoles(input.roles);
  const orgId = validateOrgIdForRoleQuery(input.orgId);
  if (input.jwt.secret.length < 32) {
    throw new Error("records GraphJin JWT secret must be at least 32 characters");
  }
  if (input.secretKey.length < 32) {
    throw new Error("records GraphJin cursor secret must be at least 32 characters");
  }

  const statementTimeoutMs = input.database.statementTimeoutMs ?? 5_000;
  const defaultLimit = validatePositiveInteger(input.defaultLimit ?? 100, "default row limit");
  const rate = input.rateLimit?.rate ?? 20;
  const bucket = validatePositiveInteger(input.rateLimit?.bucket ?? 40, "rate-limit bucket");
  if (!Number.isFinite(rate) || rate <= 0) {
    throw new Error("rate limit must be positive");
  }

  const jwt: Record<string, string> = {
    provider: "other",
    secret: input.jwt.secret,
  };
  if (input.jwt.audience) jwt.audience = input.jwt.audience;
  if (input.jwt.issuer) jwt.issuer = input.jwt.issuer;

  const connection = postgresConnectionConfig(
    input.database.connectionString,
    statementTimeoutMs,
  );
  const config = {
    app_name: "OpenNeko Records",
    mode: "prod",
    production: true,
    host_port: "0.0.0.0:8090",
    log_level: "info",
    log_format: "json",
    web_ui: false,
    enable_tracing: false,
    log_vars: false,
    auth_fail_block: true,
    reload_on_config_change: false,
    disable_allow_list: true,
    // Records queries are generated dynamically from registry metadata. In
    // GraphJin, disable_allow_list stops persistence but prodSec still rejects
    // unknown named documents; this explicit opt-out is therefore required.
    // JWT + exhaustive role-table RBAC remain the authorization boundary.
    disable_production_security: true,
    default_block: true,
    analytics_mode: true,
    default_limit: defaultLimit,
    subs_poll_duration: "5s",
    db_schema_poll_duration: "0s",
    // Records GraphJin mounts its generated config volume read-only. GraphJin
    // 3.20's discovery cache writes immutable schema generations below the
    // config path by default, so retain the prior live-discovery behavior.
    discovery_cache: { enabled: false },
    secret_key: input.secretKey,
    rate_limiter: { rate, bucket },
    // Governed record writes may commit while GraphJin returns an empty
    // mutation selection (the update predicate can stop matching after the
    // change). GraphJin cannot derive row refs from that empty response, so
    // its default response cache is not invalidated. Records reads must never
    // serve that stale pre-write snapshot.
    caching: { disable: true },
    mcp: {
      disable: true,
      allow_mutations: false,
      allow_raw_queries: false,
    },
    auth: {
      type: "jwt",
      development: false,
      jwt,
    },
    database: {
      type: "postgres",
      connection_string: connection.connectionString,
      // GraphJin's service layer assigns DBName after parsing the URL, so this
      // duplicate is required or it clears the parsed path and Postgres falls
      // back to a database named after the user.
      dbname: connection.databaseName,
      pool_size: validatePositiveInteger(input.database.poolSize ?? 5, "database pool size"),
      max_connections: validatePositiveInteger(
        input.database.maxConnections ?? 20,
        "database max connections",
      ),
      ping_timeout: "5s",
    },
    tables: recordDocumentTables(input.roles),
    roles_query: buildRecordsGraphjinRoleQuery(orgId),
    roles: input.roles,
  };

  return stringify(config, { lineWidth: 0, sortMapEntries: false });
}

/**
 * Build the internal-only native watch plane. GraphJin source mode owns the
 * artifact/watch store, while the public records data plane retains its
 * exhaustive legacy role-table policy. A separate JWT secret ensures tokens
 * minted for human records reads cannot be replayed against this service.
 */
export function buildRecordsWatchGraphjinConfig(
  input: BuildRecordsWatchGraphjinConfigInput,
): string {
  validateOrgIdForRoleQuery(input.orgId);
  if (input.jwt.secret.length < 32) {
    throw new Error("records watch GraphJin JWT secret must be at least 32 characters");
  }
  if (input.secretKey.length < 32) {
    throw new Error("records watch GraphJin cursor secret must be at least 32 characters");
  }
  const connection = postgresConnectionConfig(
    input.database.connectionString,
    input.database.statementTimeoutMs ?? 5_000,
  );
  const jwt: Record<string, string> = {
    provider: "other",
    secret: input.jwt.secret,
  };
  if (input.jwt.audience) jwt.audience = input.jwt.audience;
  if (input.jwt.issuer) jwt.issuer = input.jwt.issuer;

  const config = {
    app_name: "OpenNeko Records Watches",
    mode: "agentic",
    production: true,
    host_port: "0.0.0.0:8090",
    log_level: "info",
    log_format: "json",
    web_ui: false,
    enable_tracing: false,
    log_vars: false,
    auth_fail_block: true,
    reload_on_config_change: false,
    disable_allow_list: true,
    disable_production_security: true,
    default_block: true,
    // The native gj_watch control store has temporal columns of its own.
    // Analytics mode would require callers (and GraphJin's internal runner)
    // to add time-window predicates when managing those control rows.
    analytics_mode: false,
    default_limit: 500,
    subs_poll_duration: "30s",
    db_schema_poll_duration: "0s",
    // The isolated watch process shares the same read-only generated-config
    // volume as the records query process.
    discovery_cache: { enabled: false },
    secret_key: input.secretKey,
    rate_limiter: { rate: 10, bucket: 20 },
    caching: { disable: true },
    identity: {
      user_id_claim: "sub",
      role_claims: ["role", "roles"],
      namespace_claim: "org_id",
      // Never classify a normal records role as a source-mode admin. GraphJin
      // admins intentionally bypass account filters, while the dedicated
      // watch service must remain scoped to the org_id in its signed token.
      admin_roles: ["records_watch_control_admin"],
    },
    auth: { type: "jwt", development: false, jwt },
    mcp: {
      disable: true,
      allow_config_updates: false,
      allow_mutations: false,
      allow_raw_queries: false,
      allow_workflow_execution: false,
      allow_workflow_updates: false,
      allow_schema_reload: false,
      allow_schema_updates: false,
      allow_dev_tools: false,
    },
    artifacts: {
      enabled: true,
      source: "records",
      schema: "_graphjin",
      auto_init: true,
      poll_seconds: 5,
      locked: ["saved_query", "fragment", "workflow", "runbook"],
    },
    watches: {
      enabled: true,
      runner: "all",
      max_per_owner: 50,
      event_retention_hours: 168,
      max_events_per_watch: 500,
      snapshot_max_bytes: 32768,
      webhook_allow: input.watchWebhookAllow ?? [],
    },
    system: {
      capabilities: {
        "catalog.read": false,
        "security.read": false,
        "config.read": false,
        "config.write": false,
        "runtime.read": false,
        "raw_graphql.query": true,
        "raw_graphql.mutate": true,
        "schema.reload": false,
        "schema.write": false,
        "dev_tools.read": false,
        "legacy_discovery.read": false,
      },
      root_access: {
        gj_catalog: "blocked",
        gj_security: "blocked",
        gj_config: "blocked",
        gj_runtime: "blocked",
        gj_artifacts: "blocked",
        // This process has its own signing key and is only reachable on
        // the private network. Authenticated access lets the service own
        // its watches without granting GraphJin's tenant-filter bypass.
        gj_watch: "authenticated",
        gj_watch_event: "authenticated",
        gj_workflow: "blocked",
        gj_workflow_execution: "blocked",
      },
    },
    sources: [
      {
        name: "records",
        kind: "database",
        default: true,
        type: "postgres",
        connection_string: connection.connectionString,
        dbname: connection.databaseName,
        pool_size: validatePositiveInteger(
          input.database.poolSize ?? 3,
          "records watch database pool size",
        ),
        max_connections: validatePositiveInteger(
          input.database.maxConnections ?? 10,
          "records watch database max connections",
        ),
        ping_timeout: "5s",
        access: {
          read: "account",
          write: "blocked",
          delete: "blocked",
          namespace_column: "org_id",
          owner_column: "owner_user_id",
          missing_namespace_column: "block",
        },
      },
    ],
  };
  return stringify(config, { lineWidth: 0, sortMapEntries: false });
}

function runGraphjin(
  binary: string,
  args: string[],
  options: { cwd?: string; timeoutMs?: number } = {},
): Promise<{ stdout: string; stderr: string }> {
  return new Promise((resolve, reject) => {
    execFile(
      binary,
      args,
      {
        cwd: options.cwd,
        timeout: options.timeoutMs ?? VALIDATION_TIMEOUT_MS,
        maxBuffer: 4 * 1024 * 1024,
        env: { ...process.env, GO_ENV: "development" },
      },
      (error, stdout, stderr) => {
        if (error) {
          // GraphJin diagnostics may echo connection material. Keep the
          // durable error secret-free; operators can run the pinned validator
          // directly inside the worker when detailed output is required.
          reject(new Error(`GraphJin command failed: ${binary} ${args.join(" ")}`));
          return;
        }
        resolve({ stdout: String(stdout), stderr: String(stderr) });
      },
    );
  });
}

export function createRecordsGraphjinConfigValidator(options: {
  binary?: string;
  expectedVersion?: string;
  timeoutMs?: number;
} = {}): RecordsGraphjinConfigValidator {
  const binary = options.binary ?? "graphjin";
  const expectedVersion = options.expectedVersion ?? RECORDS_GRAPHJIN_VERSION;
  const timeoutMs = options.timeoutMs ?? VALIDATION_TIMEOUT_MS;
  return async ({ configDirectory }) => {
    const version = await runGraphjin(binary, ["version"], { timeoutMs });
    if (!version.stdout.includes(`GraphJin ${expectedVersion}`)) {
      const found = /GraphJin \S+/.exec(version.stdout)?.[0] ?? "no version";
      throw new Error(
        `records GraphJin validator must be version ${expectedVersion}; ${binary} reports ${found}`,
      );
    }
    await runGraphjin(
      binary,
      ["serve", "test", "--json", "--path", configDirectory],
      { timeoutMs },
    );
  };
}

/**
 * Cross-process lock dedicated to the complete records config writer. This is
 * deliberately separate from customer-source GraphJin config persistence.
 */
export async function acquireRecordsGraphjinConfigLock(
  configFile: string,
  opts: { waitMs?: number } = {},
): Promise<() => Promise<void>> {
  const directory = dirname(configFile);
  await mkdir(directory, { recursive: true, mode: DIRECTORY_MODE });
  const lockFile = join(directory, `.${basename(configFile)}.records.lock`);
  const token = `${process.pid}@${hostname()}:${randomUUID()}`;
  const deadline = Date.now() + (opts.waitMs ?? RECORDS_CONFIG_LOCK_WAIT_MS);
  let handle;
  for (;;) {
    try {
      handle = await open(lockFile, "wx", LOCK_MODE);
      break;
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "EEXIST") throw error;
    }
    if (await lockOwnerGone(lockFile)) {
      await unlink(lockFile).catch(() => undefined);
      continue;
    }
    if (Date.now() >= deadline) {
      throw new Error("records GraphJin config has another projection in progress");
    }
    await new Promise((resolve) => setTimeout(resolve, RECORDS_CONFIG_LOCK_POLL_MS));
  }
  try {
    await handle.writeFile(token, "utf8");
    await handle.sync();
  } finally {
    await handle.close();
  }

  return async () => {
    const current = await readFile(lockFile, "utf8").catch(() => "");
    if (current === token) await unlink(lockFile).catch(() => undefined);
  };
}

/** A lock whose writer stopped mid-projection: its process is gone, or it is older than any projection. */
async function lockOwnerGone(lockFile: string): Promise<boolean> {
  const held = await stat(lockFile).catch(() => null);
  if (!held) return false;
  if (Date.now() - held.mtimeMs >= RECORDS_CONFIG_LOCK_STALE_MS) return true;
  const owner = /^(\d+)@([^:]+):/.exec(await readFile(lockFile, "utf8").catch(() => ""));
  if (!owner || owner[2] !== hostname()) return false;
  try {
    process.kill(Number(owner[1]), 0);
    return false;
  } catch (error) {
    return (error as NodeJS.ErrnoException).code === "ESRCH";
  }
}

async function fsyncDirectory(directory: string): Promise<void> {
  const handle = await open(directory, "r");
  try {
    await handle.sync();
  } finally {
    await handle.close();
  }
}

export async function writeRecordsWatchWebhookSecret(
  configDirectory: string,
  secret: string,
): Promise<void> {
  if (secret.length < 32 || /[\r\n]/.test(secret)) {
    throw new Error("records watch webhook secret must be one line and at least 32 characters");
  }
  await mkdir(configDirectory, { recursive: true, mode: DIRECTORY_MODE });
  const target = join(configDirectory, RECORDS_WATCH_WEBHOOK_SECRET_FILENAME);
  const staging = join(
    configDirectory,
    `.${RECORDS_WATCH_WEBHOOK_SECRET_FILENAME}.${randomUUID()}.tmp`,
  );
  const handle = await open(staging, "wx", CONFIG_MODE);
  try {
    await handle.writeFile(`${secret}\n`, "utf8");
    await handle.sync();
  } finally {
    await handle.close();
  }
  try {
    await rename(staging, target);
    await fsyncDirectory(configDirectory);
  } catch (error) {
    await unlink(staging).catch(() => undefined);
    throw error;
  }
}

export type WriteRecordsGraphjinConfigResult = {
  sha256: string;
  bytes: number;
};

async function writeGeneratedRecordsGraphjinConfig(input: {
  configFile: string;
  yaml: string;
  reloadRecordsGraphjin: () => Promise<void>;
  validate?: RecordsGraphjinConfigValidator;
  stagingPrefix: string;
}): Promise<WriteRecordsGraphjinConfigResult> {
  if (basename(input.configFile) !== RECORDS_GRAPHJIN_CONFIG_FILENAME) {
    throw new Error(`records GraphJin config must be named ${RECORDS_GRAPHJIN_CONFIG_FILENAME}`);
  }
  if (typeof input.reloadRecordsGraphjin !== "function") {
    throw new Error("records GraphJin config requires an explicit reload callback");
  }

  const directory = dirname(input.configFile);
  const release = await acquireRecordsGraphjinConfigLock(input.configFile);
  const stagingDirectory = join(directory, `.${input.stagingPrefix}-${randomUUID()}`);
  const stagingFile = join(stagingDirectory, RECORDS_GRAPHJIN_CONFIG_FILENAME);

  try {
    await mkdir(stagingDirectory, { mode: DIRECTORY_MODE });
    const handle = await open(stagingFile, "wx", CONFIG_MODE);
    try {
      await handle.writeFile(input.yaml, "utf8");
      await handle.sync();
    } finally {
      await handle.close();
    }

    const validate = input.validate ?? createRecordsGraphjinConfigValidator();
    await validate({ configDirectory: stagingDirectory, configFile: stagingFile });
    await rename(stagingFile, input.configFile);
    await fsyncDirectory(directory);
    await input.reloadRecordsGraphjin();

    return {
      sha256: createHash("sha256").update(input.yaml, "utf8").digest("hex"),
      bytes: Buffer.byteLength(input.yaml, "utf8"),
    };
  } finally {
    await rm(stagingDirectory, { recursive: true, force: true });
    await release();
  }
}

/**
 * Validate a staged complete config, atomically replace the live file, then
 * invoke the caller's records-only reload boundary. Production GraphJin does
 * not file-watch configs, so a reload callback is mandatory.
 */
export async function writeRecordsGraphjinConfig(input: {
  configFile: string;
  config: BuildRecordsGraphjinConfigInput;
  reloadRecordsGraphjin: () => Promise<void>;
  validate?: RecordsGraphjinConfigValidator;
}): Promise<WriteRecordsGraphjinConfigResult> {
  return writeGeneratedRecordsGraphjinConfig({
    configFile: input.configFile,
    yaml: buildRecordsGraphjinConfig(input.config),
    reloadRecordsGraphjin: input.reloadRecordsGraphjin,
    validate: input.validate,
    stagingPrefix: "records-graphjin",
  });
}

export async function writeRecordsWatchGraphjinConfig(input: {
  configFile: string;
  config: BuildRecordsWatchGraphjinConfigInput;
  reloadRecordsGraphjin: () => Promise<void>;
  validate?: RecordsGraphjinConfigValidator;
}): Promise<WriteRecordsGraphjinConfigResult> {
  return writeGeneratedRecordsGraphjinConfig({
    configFile: input.configFile,
    yaml: buildRecordsWatchGraphjinConfig(input.config),
    reloadRecordsGraphjin: input.reloadRecordsGraphjin,
    validate: input.validate,
    stagingPrefix: "records-watch-graphjin",
  });
}

/** Read-only helper for boot drift audits without exposing config contents. */
export async function recordsGraphjinConfigSha256(configFile: string): Promise<string | null> {
  const metadata = await stat(configFile).catch(() => null);
  if (!metadata?.isFile() || metadata.size === 0) return null;
  const content = await readFile(configFile);
  return createHash("sha256").update(content).digest("hex");
}
