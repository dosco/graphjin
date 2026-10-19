import { readFile, rename, writeFile } from "node:fs/promises";
import { basename, dirname, isAbsolute, join } from "node:path";
import { isMap, isSeq, parseDocument, YAMLSeq } from "yaml";

/**
 * Pure helpers for the GraphJin sources-mode (agentic) config file.
 *
 * Kept isolated from the worker dependency graph on purpose: the one-shot
 * graphjin-secret-init container patches this file BEFORE GraphJin's first
 * start and needs only the YAML parser plus Node built-ins. host-provision.ts
 * re-exports these helpers so existing imports keep working.
 */

export const SOURCES_SECRET_PLACEHOLDER = "REPLACE_WITH_PER_ORG_SECRET_B64";
export const SOURCES_JWT_SECRET_RE =
  /(^auth:\s*\n\s+type:\s*jwt\s*\n\s+jwt:\s*\n(?:\s+#.*\n)*\s+secret:\s*")([^"]*)(")/m;
export const LEGACY_PACKAGED_DEMO_GRAPHJIN_CONFIG = "/graphjin-config/agentic.yml";

const NON_DATABASE_SOURCE_KINDS = new Set(["api", "file", "graphjin"]);
const GRAPHQL_MUTATION_RE = /\bmutation\b/i;

/**
 * Sources-mode GraphJin blocks mutations against a `read_only: true` database
 * in core and pins that flag at startup, so a later config patch cannot lift
 * it. Every source of an unknown kind counts as a database.
 */
export function assertDatabaseSourcesReadOnly(raw: string): void {
  const document = parseDocument(raw);
  if (document.errors.length > 0 || !isMap(document.contents)) {
    throw new Error("GraphJin config is not valid YAML");
  }
  const sources = document.get("sources", true);
  if (sources == null) return;
  if (!isSeq(sources)) {
    throw new Error("GraphJin config sources must be a YAML list");
  }
  for (const item of sources.items) {
    if (!isMap(item)) {
      throw new Error("GraphJin config source entries must be YAML objects");
    }
    const kind = item.get("kind");
    if (typeof kind === "string" && NON_DATABASE_SOURCE_KINDS.has(kind)) {
      continue;
    }
    if (item.get("read_only") !== true) {
      const name = String(item.get("name") ?? "<unnamed>");
      throw new Error(`GraphJin database source "${name}" is not read_only`);
    }
  }
}

/**
 * The trusted host forwards a GraphQL mutation to GraphJin only when the
 * org's config proves every database source is read-only. API sources can
 * then accept governed writes while the customer database never can. A
 * missing or unreadable config fails closed.
 */
export async function assertGraphjinMutationAllowed(
  query: string,
  configPath: string | undefined,
): Promise<void> {
  if (!GRAPHQL_MUTATION_RE.test(query)) return;
  const cfgPath = configPath?.trim();
  if (!cfgPath) {
    throw new Error(
      "GraphJin mutation blocked: OPENNEKO_GRAPHJIN_CONFIG is not set, so the host cannot confirm database sources are read-only",
    );
  }
  let raw: string;
  try {
    const { readFile } = await import("node:fs/promises");
    raw = await readFile(cfgPath, "utf8");
  } catch (e) {
    throw new Error(
      `GraphJin mutation blocked: cannot read ${cfgPath}: ${e instanceof Error ? e.message : String(e)}`,
    );
  }
  try {
    assertDatabaseSourcesReadOnly(raw);
  } catch (e) {
    throw new Error(
      `GraphJin mutation blocked: ${e instanceof Error ? e.message : String(e)}`,
    );
  }
}

/** JWT roles the worker mints. GraphJin generates access blocks only for listed roles. */
export const OPENNEKO_JWT_ROLES = [
  {
    name: "member",
    comment: "OpenNeko organization member (JWT role minted by the worker)",
  },
  {
    name: "service",
    comment: "OpenNeko worker service identity (JWT role minted by the worker)",
  },
] as const;

/**
 * Bring an existing sources-mode config up to the shipped write policy:
 * mutations pass the MCP layer, and the worker's JWT roles exist so every
 * source's access.write block applies to them. Database sources stay
 * read-only through their own `read_only` flag and the host guard. Legacy
 * configs without a `sources` list are left alone.
 */
export function reconcileGraphjinWritePolicy(
  raw: string,
): { content: string; changed: boolean } {
  const document = parseDocument(raw);
  if (document.errors.length > 0) {
    throw new Error(`GraphJin config is not valid YAML: ${document.errors[0].message}`);
  }
  if (!isMap(document.contents)) {
    throw new Error("GraphJin config root must be a YAML object");
  }
  if (!document.has("sources")) return { content: raw, changed: false };

  let changed = false;
  const ensure = (path: string[], value: unknown) => {
    if (document.getIn(path) === value) return;
    document.setIn(path, value);
    changed = true;
  };
  ensure(["mcp", "allow_mutations"], true);
  ensure(["system", "capabilities", "raw_graphql.mutate"], true);

  let roles = document.get("roles", true);
  if (roles == null) {
    roles = new YAMLSeq();
    document.set("roles", roles);
    changed = true;
  }
  if (!isSeq(roles)) throw new Error("GraphJin config roles must be a YAML list");
  const listed = new Set(
    roles.items.flatMap((item) =>
      isMap(item) ? [String(item.get("name") ?? "").trim().toLowerCase()] : [],
    ),
  );
  for (const role of OPENNEKO_JWT_ROLES) {
    if (listed.has(role.name)) continue;
    roles.add(document.createNode({ name: role.name, comment: role.comment }));
    changed = true;
  }
  return { content: changed ? document.toString() : raw, changed };
}

const AGENTIC_SYSTEM_CAPABILITIES = {
  "catalog.read": true,
  "security.read": true,
  "config.read": true,
  "config.write": true,
  "runtime.read": true,
  "raw_graphql.query": true,
  "raw_graphql.mutate": false,
  "schema.reload": false,
  "schema.write": false,
  "dev_tools.read": false,
  "legacy_discovery.read": false,
};

const AGENTIC_SYSTEM_ROOT_ACCESS = {
  gj_catalog: "authenticated",
  gj_config: "admin",
  gj_security: "admin",
  gj_runtime: "admin",
};

/**
 * GraphJin 3.20 moved its catalog/config/security/runtime roots out of the
 * synthetic `sources[].kind: graphjin` provider and into top-level `system`.
 * Config volumes survive image upgrades, so a corrected seed alone cannot
 * repair an existing install. Migrate that one removed provider in place,
 * retaining every real customer source and any already-explicit system value.
 */
export function migrateGraphjinSystemSource(
  raw: string,
): { content: string; changed: boolean } {
  const document = parseDocument(raw);
  if (document.errors.length > 0) {
    throw new Error(`GraphJin config is not valid YAML: ${document.errors[0].message}`);
  }
  if (!isMap(document.contents)) {
    throw new Error("GraphJin config root must be a YAML object");
  }

  const sources = document.get("sources", true);
  if (sources == null) return { content: raw, changed: false };
  if (!isSeq(sources)) {
    throw new Error("GraphJin config sources must be a YAML list");
  }

  const legacySources = sources.items.filter(
    (source) =>
      isMap(source) &&
      String(source.get("kind") ?? "").trim().toLowerCase() === "graphjin",
  );
  if (legacySources.length === 0) return { content: raw, changed: false };

  sources.items = sources.items.filter((source) => !legacySources.includes(source));

  let system = document.get("system", true);
  if (system == null) {
    document.set(
      "system",
      document.createNode({
        capabilities: AGENTIC_SYSTEM_CAPABILITIES,
        root_access: AGENTIC_SYSTEM_ROOT_ACCESS,
      }),
    );
    system = document.get("system", true);
  }
  if (!isMap(system)) {
    throw new Error("GraphJin config system must be a YAML object");
  }

  for (const [sectionName, defaults] of [
    ["capabilities", AGENTIC_SYSTEM_CAPABILITIES],
    ["root_access", AGENTIC_SYSTEM_ROOT_ACCESS],
  ] as const) {
    let section = system.get(sectionName, true);
    if (section == null) {
      system.set(sectionName, document.createNode(defaults));
      section = system.get(sectionName, true);
    }
    if (!isMap(section)) {
      throw new Error(`GraphJin config system.${sectionName} must be a YAML object`);
    }
    for (const [key, value] of Object.entries(defaults)) {
      if (section.get(key, true) == null) section.set(key, value);
    }
  }

  return { content: document.toString(), changed: true };
}

/** Where GraphJin sends watcher events: the worker's admin server. */
export function watcherWebhookUrl(): string {
  return (process.env.OPENNEKO_WATCHER_WEBHOOK_URL ?? "http://worker:4100/admin/events/watcher").trim();
}

/**
 * Turns on GraphJin watches for OpenNeko watchers: GraphJin's managed artifact
 * store that holds them, the runner, a per-owner limit that fits every watcher of
 * the org (they share the service identity), the worker's webhook in the
 * allowlist, and gj_watch for authenticated callers. Values an operator set
 * stay, apart from adding the webhook to the allowlist.
 */
export function patchGraphjinWatches(
  raw: string,
  webhookUrl: string,
): { content: string; changed: boolean } {
  const document = parseDocument(raw);
  if (document.errors.length > 0 || !isMap(document.contents)) {
    throw new Error("GraphJin config is not valid YAML");
  }
  const before = document.toString();
  // GraphJin keeps watches in its managed store only when artifacts.enabled
  // and artifacts.source are both unset; an explicit enable picks a customer
  // source instead, and customer sources are read-only.
  if (document.getIn(["artifacts", "source"]) == null && document.hasIn(["artifacts", "enabled"])) {
    document.deleteIn(["artifacts", "enabled"]);
    const artifacts = document.get("artifacts", true);
    if (isMap(artifacts) && artifacts.items.length === 0) document.delete("artifacts");
  }
  if (document.getIn(["watches", "enabled"]) !== true) document.setIn(["watches", "enabled"], true);
  const runner = document.getIn(["watches", "runner"]);
  if (runner == null || runner === "off") document.setIn(["watches", "runner"], "all");
  const limit = Number(document.getIn(["watches", "max_per_owner"]) ?? 0);
  if (!(limit >= 1000)) document.setIn(["watches", "max_per_owner"], 1000);
  const allow = document.getIn(["watches", "webhook_allow"]);
  const entries = isSeq(allow) ? allow.items.map((item) => String((item as { value?: unknown }).value ?? item)) : [];
  if (!entries.includes(webhookUrl)) document.setIn(["watches", "webhook_allow"], [...entries, webhookUrl]);
  if (document.getIn(["system", "root_access", "gj_watch"]) == null) {
    document.setIn(["system", "root_access", "gj_watch"], "authenticated");
  }
  const content = document.toString();
  return content === before ? { content: raw, changed: false } : { content, changed: true };
}

export const KEYSTORE_KEY_PLACEHOLDER = "REPLACE_WITH_BASE64_32_BYTE_KEY";

/**
 * Sets `secrets.keystore.key` to the key OpenNeko derives from its secret key.
 * When the config held a different key and the keystore may hold values sealed
 * with it (keystoreHasValues), that key moves to `secrets.keystore.previous_keys`,
 * so GraphJin re-seals those values under the derived key at start.
 */
export function patchGraphjinKeystoreKey(
  raw: string,
  key: string,
  keystoreHasValues: boolean,
): { content: string; changed: boolean } {
  const document = parseDocument(raw);
  if (document.errors.length > 0 || !isMap(document.contents)) {
    throw new Error("GraphJin config is not valid YAML");
  }
  const current = document.getIn(["secrets", "keystore", "key"]);
  const currentKey = typeof current === "string" ? current.trim() : "";
  if (currentKey === key) return { content: raw, changed: false };
  if (currentKey && currentKey !== KEYSTORE_KEY_PLACEHOLDER && keystoreHasValues) {
    const previous = document.getIn(["secrets", "keystore", "previous_keys"]);
    const keys = isSeq(previous)
      ? previous.items.map((item) => String((item as { value?: unknown }).value ?? item))
      : [];
    if (!keys.includes(currentKey)) keys.push(currentKey);
    document.setIn(["secrets", "keystore", "previous_keys"], keys);
  }
  document.setIn(["secrets", "keystore", "key"], key);
  return { content: document.toString(), changed: true };
}

/**
 * Whether the GraphJin keystore next to configPath (secrets.keystore.path,
 * default secrets.enc.yml) holds any sealed value.
 */
export async function graphjinKeystoreHasValues(configPath: string, raw: string): Promise<boolean> {
  const document = parseDocument(raw);
  const configured = document.getIn(["secrets", "keystore", "path"]);
  const path = typeof configured === "string" && configured.trim() ? configured.trim() : "secrets.enc.yml";
  try {
    const text = await readFile(isAbsolute(path) ? path : join(dirname(configPath), path), "utf8");
    return text.includes("ciphertext:");
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") return false;
    throw error;
  }
}

export function patchGraphjinSourcesJwtSecret(
  raw: string,
  secret: string,
): { content: string; changed: boolean } {
  let content = raw.replaceAll(SOURCES_SECRET_PLACEHOLDER, secret);
  if (content !== raw) return { content, changed: true };

  const match = SOURCES_JWT_SECRET_RE.exec(raw);
  if (!match || match[2] === secret) return { content: raw, changed: false };

  content = raw.replace(SOURCES_JWT_SECRET_RE, `$1${secret}$3`);
  return { content, changed: content !== raw };
}

/**
 * Existing v2.25 demo installs do not carry OPENNEKO_STACK_MODE because their
 * compose file is embedded in the older host CLI. The dedicated demo mount is
 * therefore retained as a backward-compatible discriminator. Production uses
 * /config/graphjin/agentic.yml and must keep its capability-probe behavior.
 */
export function shouldReconcileDemoSourceAuthMode(
  configPath: string | undefined,
  configContent: string,
  stackMode?: string,
): boolean {
  const isDemo =
    stackMode === "demo" || configPath === LEGACY_PACKAGED_DEMO_GRAPHJIN_CONFIG;
  return isDemo && SOURCES_JWT_SECRET_RE.test(configContent);
}

/**
 * Write-then-rename within the target directory. GraphJin watches these
 * files (reload_on_config_change) and other processes read them on their
 * own schedule; an in-place truncate-and-write exposes a partial file to
 * both. rename(2) within one volume is atomic, so readers see either the
 * old content or the new — never a truncated intermediate.
 */
export async function atomicWriteFile(
  path: string,
  content: string,
  opts?: { mode?: number },
): Promise<void> {
  const tmp = join(
    dirname(path),
    `.${basename(path)}.tmp-${process.pid}-${Date.now().toString(36)}`,
  );
  await writeFile(tmp, content, { encoding: "utf8", mode: opts?.mode ?? 0o664 });
  await rename(tmp, path);
}
