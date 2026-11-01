import { lstat, readFile, readdir } from "node:fs/promises";
import { extname, join, relative, resolve, sep } from "node:path";
import {
  action_policy,
  and,
  db,
  eq,
  metric,
  pack_action_definition,
  watcher,
  workflow_definition,
} from "@neko/db";
import { canonicalHash, sha256, type PackArtifact } from "@neko/packs";
import { parse as parseYaml } from "yaml";
import { readLiveGraphjinConfig, readLiveSavedQuery, type GraphjinTarget, type LiveGraphjinConfig } from "./graphjin-target.js";

type ArtifactMetadata = Record<string, unknown>;

export type PackArtifactLocator = {
  source?: string;
  ignorePackAliases?: boolean;
  role?: string;
  slug?: string;
  name?: string;
  kind?: string;
};

const nativeStateKeys = {
  metric: [
    "role", "slug", "source", "title", "description", "why", "chart_hint", "unit",
    "direction_good", "cadence", "active", "execution_mode", "definition_json",
    "definition_version", "definition_hash",
  ],
  workflow: [
    "name", "description", "enabled", "status", "goal", "cron", "cron_timezone",
    "cron_enabled", "output_contract",
  ],
  watcher: [
    "workflow_id", "name", "description", "enabled", "query", "value_path", "op",
    "threshold", "cadence_seconds", "debounce_seconds", "cooldown_seconds", "dedupe_key",
    "activation", "variables_json", "severity",
  ],
  policy: [
    "name", "description", "applies_to_kinds", "applies_to_scopes", "mode",
    "allowed_targets", "limits", "approver_role", "priority",
  ],
  action: ["kind", "description", "definition", "definition_hash", "enabled"],
} as const;

type NativeStateKind = keyof typeof nativeStateKeys;

export function nativeArtifactStateHash(
  kind: NativeStateKind,
  value: Record<string, unknown>,
): string {
  return canonicalHash(
    Object.fromEntries(nativeStateKeys[kind].map((key) => [key, value[key] ?? null])),
  );
}

/** Older pack receipts hashed an approver role that action_policy never stored. */
export function policyArtifactStateHash(
  policy: Record<string, unknown>,
  lastAppliedHash?: string,
): string {
  const current = nativeArtifactStateHash("policy", policy);
  if (!lastAppliedHash || current === lastAppliedHash || Object.hasOwn(policy, "approver_role")) return current;
  const legacy = nativeArtifactStateHash("policy", { ...policy, approver_role: "admin" });
  return legacy === lastAppliedHash ? legacy : current;
}

function isMissing(error: unknown): boolean {
  return (error as NodeJS.ErrnoException).code === "ENOENT";
}

async function hashMaterializedFile(path: string): Promise<string | null> {
  let stat;
  try {
    stat = await lstat(path);
  } catch (error) {
    if (isMissing(error)) return null;
    throw error;
  }
  if (stat.isSymbolicLink()) throw new Error(`pack-owned target became a symlink: ${path}`);
  if (!stat.isFile()) throw new Error(`pack-owned target is not a file: ${path}`);
  const raw = (await readFile(path, "utf8")).replace(/\r\n/g, "\n");
  const extension = extname(path).toLowerCase();
  if (extension === ".json") return canonicalHash(JSON.parse(raw));
  if (extension === ".yaml" || extension === ".yml") return canonicalHash(parseYaml(raw));
  return sha256(raw);
}

async function hashMarkdownTree(root: string, directory = root): Promise<string | null> {
  let stat;
  try {
    stat = await lstat(directory);
  } catch (error) {
    if (isMissing(error)) return null;
    throw error;
  }
  if (stat.isSymbolicLink()) throw new Error(`pack-owned skill became a symlink: ${directory}`);
  if (!stat.isDirectory()) throw new Error(`pack-owned skill is not a directory: ${directory}`);
  const entries = await readdir(directory, { withFileTypes: true });
  const values: Array<{ path: string; hash: string }> = [];
  for (const entry of entries.sort((a, b) => a.name.localeCompare(b.name))) {
    const child = join(directory, entry.name);
    if (entry.isSymbolicLink()) throw new Error(`pack-owned skill contains a symlink: ${child}`);
    if (entry.isDirectory()) {
      const hash = await hashMarkdownTree(root, child);
      if (hash) values.push({ path: relative(root, child), hash });
      continue;
    }
    if (!entry.isFile() || extname(entry.name).toLowerCase() !== ".md") {
      throw new Error(`pack-owned skill contains a non-Markdown file: ${child}`);
    }
    values.push({ path: relative(root, child), hash: sha256((await readFile(child, "utf8")).replace(/\r\n/g, "\n")) });
  }
  return canonicalHash(values);
}

function safeMaterializedTarget(configFile: string, metadata: ArtifactMetadata): string | null {
  const target = typeof metadata.materializedTarget === "string" ? metadata.materializedTarget : null;
  if (!target) return null;
  const root = resolve(configFile, "..");
  const absolute = resolve(target);
  const path = relative(root, absolute);
  if (path === ".." || path.startsWith(`..${sep}`) || path.startsWith(sep)) {
    throw new Error(`pack materialized target escapes GraphJin config root: ${target}`);
  }
  return absolute;
}

type GraphjinConfigView = {
  sources?: Array<Record<string, unknown>>;
  tables?: Array<Record<string, unknown>>;
  relationships?: Array<Record<string, unknown>>;
};

const liveConfigs = new WeakMap<object, Promise<LiveGraphjinConfig>>();

/** One live read per target object, shared by every artifact it inspects. */
function liveConfig(target: Extract<GraphjinTarget, { mode: "api" }>): Promise<LiveGraphjinConfig> {
  let pending = liveConfigs.get(target);
  if (!pending) {
    pending = readLiveGraphjinConfig(target);
    liveConfigs.set(target, pending);
  }
  return pending;
}

async function graphjinConfigView(graphjin: GraphjinTarget): Promise<GraphjinConfigView | null> {
  if (graphjin.mode === "api") return liveConfig(graphjin);
  let raw: string;
  try {
    raw = await readFile(graphjin.configFile, "utf8");
  } catch (error) {
    if (isMissing(error)) return null;
    throw error;
  }
  return parseYaml(raw) as GraphjinConfigView;
}

/**
 * The pack-controlled part of a source as GraphJin reports it. GraphJin
 * redacts sealed credentials and fills defaults differently after a reload,
 * so a hash of the whole source would report drift that nobody made. The
 * spec document has its own artifact.
 */
export function liveSourceStateHash(source: Record<string, unknown>, lastAppliedHash?: string): string {
  const current = canonicalHash(stableLiveSource(source));
  if (!lastAppliedHash || current === lastAppliedHash) return current;
  const legacy = canonicalHash(legacyStableLiveSource(source));
  return legacy === lastAppliedHash ? legacy : current;
}

/** Receipts written before empty values were dropped hashed this shape. */
function legacyStableLiveSource(source: Record<string, unknown>): Record<string, unknown> {
  const specs = source.specs && typeof source.specs === "object" ? source.specs as Record<string, Record<string, unknown>> : {};
  return {
    name: source.name ?? null,
    kind: source.kind ?? null,
    read_only: source.read_only ?? null,
    access: source.access ?? null,
    capabilities: source.capabilities ?? null,
    specs: Object.fromEntries(Object.entries(specs).map(([key, spec]) => [key, {
      base_url: spec.base_url ?? null,
      operations: spec.operations ?? null,
    }])),
  };
}

/**
 * GraphJin reports an unset list as null after an apply and as [] after a
 * restart reads its YAML. Drop empty values so both views hash the same.
 * False and 0 stay: a capability set to false differs from a missing one.
 */
function withoutEmpty(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(withoutEmpty);
  if (!value || typeof value !== "object" || value instanceof Date) return value;
  const out: Record<string, unknown> = {};
  for (const [key, entry] of Object.entries(value)) {
    const compact = withoutEmpty(entry);
    if (compact === null || compact === undefined || compact === "") continue;
    if (Array.isArray(compact) && compact.length === 0) continue;
    if (typeof compact === "object" && !Array.isArray(compact) && Object.keys(compact).length === 0) continue;
    out[key] = compact;
  }
  return out;
}

function stableLiveSource(source: Record<string, unknown>): Record<string, unknown> {
  const specs = source.specs && typeof source.specs === "object" ? source.specs as Record<string, Record<string, unknown>> : {};
  return {
    name: source.name ?? null,
    kind: source.kind ?? null,
    read_only: source.read_only ?? null,
    access: withoutEmpty(source.access ?? {}),
    capabilities: source.capabilities ?? null,
    specs: Object.fromEntries(Object.entries(specs).map(([key, spec]) => [key, {
      base_url: spec.base_url ?? null,
      operations: withoutEmpty(spec.operations ?? {}),
    }])),
  };
}

async function graphjinConfigState(
  graphjin: GraphjinTarget,
  kind: "source" | "relationships",
  targetRef: string,
  locator: PackArtifactLocator,
  lastAppliedHash?: string,
): Promise<string | null> {
  const config = await graphjinConfigView(graphjin);
  if (!config) return null;
  if (kind === "source") {
    const source = config.sources?.find((value) => value.name === (locator.name ?? targetRef));
    if (!source) return null;
    return graphjin.mode === "api" ? liveSourceStateHash(source, lastAppliedHash) : canonicalHash(source);
  }
  const source = locator.source ?? targetRef;
  const tables = (config.tables ?? []).filter(
    (value) => (value.source === source || value.database === source) && !(locator.ignorePackAliases && /^pack_[a-f0-9]{20}$/.test(String(value.name))),
  );
  const relationships = (config.relationships ?? []).filter(
    (value) =>
      String(value.from ?? "").startsWith(`${source}:`) ||
      String(value.to ?? "").startsWith(`${source}:`),
  );
  return tables.length || relationships.length || config.sources?.some(value => value.name === source)
    ? canonicalHash({ tables, relationships })
    : null;
}

/** The hash of a spec or saved query that an api-mode install sent to GraphJin. */
async function liveGraphjinArtifactState(
  graphjin: Extract<GraphjinTarget, { mode: "api" }>,
  kind: "spec" | "saved_query",
  target: string,
): Promise<string | null> {
  if (kind === "saved_query") {
    const name = target.slice("graphjin:saved_query:".length);
    const query = await readLiveSavedQuery(graphjin, name);
    return query === null ? null : sha256(query.replace(/\r\n/g, "\n").trim());
  }
  const path = target.slice("graphjin:spec:".length);
  const key = path.split("/").pop()!.replace(/\.(ya?ml|json)$/i, "");
  for (const source of (await liveConfig(graphjin)).sources) {
    const specs = source.specs && typeof source.specs === "object" ? source.specs as Record<string, Record<string, unknown>> : {};
    const document = specs[key]?.document;
    if (typeof document === "string" && document.trim()) return canonicalHash(parseYaml(document));
  }
  return null;
}

async function inspectNative(
  orgId: string,
  kind: "metric" | "workflow" | "watcher" | "policy" | "action",
  targetRef: string,
  locator: PackArtifactLocator,
  lastAppliedHash?: string,
): Promise<string | null> {
  switch (kind) {
    case "metric": {
      const [row] = await db().select().from(metric).where(and(
        eq(metric.org_id, orgId),
        eq(metric.role, locator.role ?? ""),
        eq(metric.slug, locator.slug ?? targetRef),
      )).limit(1);
      return row ? nativeArtifactStateHash("metric", row as unknown as Record<string, unknown>) : null;
    }
    case "workflow": {
      const [row] = await db().select().from(workflow_definition).where(and(
        eq(workflow_definition.org_id, orgId),
        eq(workflow_definition.owner_user_id, ""),
        eq(workflow_definition.name, locator.name ?? targetRef),
      )).limit(1);
      return row ? nativeArtifactStateHash("workflow", row as unknown as Record<string, unknown>) : null;
    }
    case "watcher": {
      const [row] = await db().select().from(watcher).where(and(
        eq(watcher.org_id, orgId),
        eq(watcher.name, locator.name ?? targetRef),
      )).limit(1);
      return row ? nativeArtifactStateHash("watcher", row as unknown as Record<string, unknown>) : null;
    }
    case "policy": {
      const [row] = await db().select().from(action_policy).where(and(
        eq(action_policy.org_id, orgId),
        eq(action_policy.name, locator.name ?? targetRef),
      )).limit(1);
      return row ? policyArtifactStateHash(row as unknown as Record<string, unknown>, lastAppliedHash) : null;
    }
    case "action": {
      const [row] = await db().select().from(pack_action_definition).where(and(
        eq(pack_action_definition.org_id, orgId),
        eq(pack_action_definition.kind, locator.kind ?? targetRef),
      )).limit(1);
      return row ? nativeArtifactStateHash("action", row as unknown as Record<string, unknown>) : null;
    }
    default:
      return null;
  }
}

export function packArtifactLocator(artifact: PackArtifact): PackArtifactLocator {
  const content = artifact.content && typeof artifact.content === "object" && !Array.isArray(artifact.content)
    ? artifact.content as Record<string, unknown>
    : {};
  switch (artifact.kind) {
    case "source":
      return { name: artifact.targetRef };
    case "relationships":
      return { source: String(content.source ?? artifact.targetRef) };
    case "metric":
      return { role: String(content.role ?? ""), slug: artifact.targetRef };
    case "workflow":
    case "watcher":
    case "policy":
      return { name: String(content.name ?? artifact.targetRef) };
    case "action":
      return { kind: String(content.kind ?? artifact.targetRef) };
    default:
      return {};
  }
}

function locatorFromMetadata(
  metadata: ArtifactMetadata,
  fallback?: PackArtifact,
): PackArtifactLocator {
  const value = metadata.locator;
  if (value && typeof value === "object" && !Array.isArray(value)) {
    return value as PackArtifactLocator;
  }
  return fallback ? packArtifactLocator(fallback) : {};
}

export async function inspectInstalledPackArtifactCurrent(input: {
  orgId: string;
  kind: PackArtifact["kind"];
  targetRef: string;
  metadata: ArtifactMetadata;
  graphjin: GraphjinTarget | null;
  fallbackArtifact?: PackArtifact;
  lastAppliedHash?: string;
}): Promise<string | null> {
  const locator = locatorFromMetadata(input.metadata, input.fallbackArtifact);
  switch (input.kind) {
    case "source":
    case "relationships":
      return input.graphjin
        ? graphjinConfigState(input.graphjin, input.kind, input.targetRef, locator, input.lastAppliedHash)
        : null;
    case "spec":
    case "saved_query": {
      if (!input.graphjin) return null;
      const materialized = typeof input.metadata.materializedTarget === "string" ? input.metadata.materializedTarget : null;
      if (materialized?.startsWith("graphjin:")) {
        return input.graphjin.mode === "api" ? liveGraphjinArtifactState(input.graphjin, input.kind, materialized) : null;
      }
      if (input.graphjin.mode !== "files") return null;
      const target = safeMaterializedTarget(input.graphjin.configFile, input.metadata);
      return target ? hashMaterializedFile(target) : null;
    }
    case "skill": {
      const target = typeof input.metadata.materializedTarget === "string"
        ? input.metadata.materializedTarget
        : null;
      return target ? hashMarkdownTree(target) : null;
    }
    case "metric":
    case "workflow":
    case "watcher":
    case "policy":
    case "action":
      return inspectNative(input.orgId, input.kind, input.targetRef, locator, input.lastAppliedHash);
  }
}

export async function inspectPackArtifactCurrent(input: {
  orgId: string;
  artifact: PackArtifact;
  metadata: ArtifactMetadata;
  graphjin: GraphjinTarget | null;
}): Promise<string | null> {
  return inspectInstalledPackArtifactCurrent({
    orgId: input.orgId,
    kind: input.artifact.kind,
    targetRef: input.artifact.targetRef,
    metadata: input.metadata,
    graphjin: input.graphjin,
    fallbackArtifact: input.artifact,
  });
}
