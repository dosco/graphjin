import { isMap, isSeq, parseDocument, YAMLMap, YAMLSeq } from "yaml";
import { compileRowFilter, parseRowFilter } from "./row-filter";

/** GraphJin role for an OpenNeko group. The slug never changes after creation. */
export function groupRoleName(slug: string): string {
  return `og_${slug}`;
}

export const GROUP_ROLE_PREFIX = "og_";

export type GroupTableGrant = { name: string; columns: string[]; filter?: string };

export type GroupGrantsModel = {
  roles: Array<{ role: string; groupName: string }>;
  /** source name -> role -> table grants */
  grants: Map<string, Map<string, GroupTableGrant[]>>;
  /** "source:spec:operation" -> roles that hold the API operation */
  apiOperations: Map<string, string[]>;
};

export type DataAccessRuleInput = {
  groupSlug: string;
  groupName: string;
  source: string;
  tableSchema: string;
  tableName: string;
  columns: string[];
  rowFilter: unknown;
};

/** Builds the grant model from rules the groups hold. Invalid filters throw. */
export function buildGroupGrantsModel(input: {
  groups: Array<{ slug: string; name: string }>;
  rules: DataAccessRuleInput[];
  apiOperationHolders: Map<string, string[]>;
}): GroupGrantsModel {
  const grants = new Map<string, Map<string, GroupTableGrant[]>>();
  const roleNames = new Map<string, string>();
  for (const rule of input.rules) {
    const role = groupRoleName(rule.groupSlug);
    roleNames.set(role, rule.groupName);
    const bySource = grants.get(rule.source) ?? new Map<string, GroupTableGrant[]>();
    const tables = bySource.get(role) ?? [];
    const grant: GroupTableGrant = {
      name: rule.tableSchema ? `${rule.tableSchema}.${rule.tableName}` : rule.tableName,
      columns: [...rule.columns],
    };
    if (rule.rowFilter != null) grant.filter = compileRowFilter(parseRowFilter(rule.rowFilter));
    tables.push(grant);
    bySource.set(role, tables);
    grants.set(rule.source, bySource);
  }
  const apiOperations = new Map<string, string[]>();
  for (const [operation, slugs] of input.apiOperationHolders) {
    const roles = [...new Set(slugs.map(groupRoleName))].sort();
    for (const role of roles) {
      if (!roleNames.has(role)) {
        roleNames.set(role, input.groups.find((g) => groupRoleName(g.slug) === role)?.name ?? role);
      }
    }
    apiOperations.set(operation, roles);
  }
  return {
    roles: [...roleNames].map(([role, groupName]) => ({ role, groupName })).sort((a, b) => a.role.localeCompare(b.role)),
    grants,
    apiOperations,
  };
}

const NON_DATABASE_KINDS = new Set(["api", "file", "code", "graphjin"]);

function listOf(parent: YAMLMap, key: string): YAMLSeq {
  const node: unknown = parent.get(key, true);
  if (node == null) {
    const created = new YAMLSeq();
    parent.set(key, created);
    return created;
  }
  if (!isSeq(node)) throw new Error(`GraphJin config ${key} must be a YAML list`);
  return node;
}

function mapOf(parent: YAMLMap, key: string): YAMLMap {
  const node: unknown = parent.get(key, true);
  if (node == null) {
    const created = new YAMLMap();
    parent.set(key, created);
    return created;
  }
  if (!isMap(node)) throw new Error(`GraphJin config ${key} must be a YAML object`);
  return node;
}

function isGroupRole(value: unknown): boolean {
  return typeof value === "string" && value.startsWith(GROUP_ROLE_PREFIX);
}

function scopeFilter(access: YAMLMap, mode: string): string | null {
  if (mode === "account") return `{ ${String(access.get("namespace_column") || "account_id")}: { eq: $account_id } }`;
  if (mode === "owner") return `{ ${String(access.get("owner_column") || "user_id")}: { eq: $user_id } }`;
  return null;
}

/**
 * Writes the group grant model into a GraphJin sources-mode config. The patch
 * is idempotent and replaces only og_* roles, grants and allowed_roles, so
 * roles and grants written by hand or by packs stay. A source that read in
 * account or owner mode before grants keeps that row scope in every grant.
 */
export function applyGroupGrantsToConfig(
  raw: string,
  model: GroupGrantsModel,
  previousReadModes: Record<string, string>,
): { content: string; changed: boolean } {
  const document = parseDocument(raw);
  if (document.errors.length > 0) throw new Error(`GraphJin config is not valid YAML: ${document.errors[0]!.message}`);
  if (!isMap(document.contents)) throw new Error("GraphJin config root must be a YAML object");
  const root = document.contents;
  if (!root.has("sources")) return { content: raw, changed: false };
  const before = document.toString();

  const identity = mapOf(root, "identity");
  identity.set("role_mode", "union");
  const roleClaims = listOf(identity, "role_claims");
  for (const claim of ["role", "roles"]) {
    if (!roleClaims.items.some((item) => String((item as { value?: unknown }).value ?? item) === claim)) roleClaims.add(claim);
  }
  identity.set("group_claims", document.createNode(["groups"]));

  const roles = listOf(root, "roles");
  roles.items = roles.items.filter((item) => !(isMap(item) && isGroupRole(item.get("name"))));
  for (const role of model.roles) {
    roles.add(document.createNode({ name: role.role, comment: `OpenNeko group ${role.groupName}` }));
  }

  const sources = listOf(root, "sources");
  for (const source of sources.items) {
    if (!isMap(source)) continue;
    const name = String(source.get("name") ?? "");
    const kind = String(source.get("kind") ?? "database").toLowerCase();
    if (!NON_DATABASE_KINDS.has(kind)) {
      const access = mapOf(source, "access");
      const scope = scopeFilter(access, previousReadModes[name] ?? String(access.get("read") ?? ""));
      access.set("read", "admin");
      const existing = access.get("grants", true);
      const kept = isSeq(existing) ? existing.items.filter((item) => !(isMap(item) && isGroupRole(item.get("role")))) : [];
      const generated = [...(model.grants.get(name) ?? new Map())]
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([role, tables]) =>
          document.createNode({
            role,
            tables: [...tables]
              .sort((a, b) => a.name.localeCompare(b.name))
              .map((t) => {
                const filter = scope && t.filter ? `{ and: [${scope}, ${t.filter}] }` : (scope ?? t.filter);
                return { name: t.name, columns: t.columns, ...(filter ? { filter } : {}) };
              }),
          }),
        );
      if (kept.length + generated.length === 0) access.delete("grants");
      else {
        const seq = new YAMLSeq();
        seq.items = [...kept, ...generated];
        access.set("grants", seq);
      }
    }
    const specs = source.get("specs", true);
    if (!isMap(specs)) continue;
    for (const specPair of specs.items) {
      const specName = String((specPair.key as { value?: unknown })?.value ?? specPair.key);
      const spec = specPair.value;
      if (!isMap(spec)) continue;
      const operations = spec.get("operations", true);
      if (!isMap(operations)) continue;
      for (const operationPair of operations.items) {
        const operationName = String((operationPair.key as { value?: unknown })?.value ?? operationPair.key);
        const operation = operationPair.value;
        if (!isMap(operation)) continue;
        const current = operation.get("allowed_roles", true);
        const kept = isSeq(current)
          ? current.items.map((item) => String((item as { value?: unknown }).value ?? item)).filter((role) => !isGroupRole(role))
          : [];
        const generated = model.apiOperations.get(`${name}:${specName}:${operationName}`) ?? [];
        if (!isSeq(current) && generated.length === 0) continue;
        operation.set("allowed_roles", document.createNode([...kept, ...generated]));
      }
    }
  }

  const content = document.toString();
  return { content, changed: content !== before };
}

/** API operations exposed in the config, as "source:spec:operation" ids. */
export function listConfigApiOperations(raw: string): string[] {
  const document = parseDocument(raw);
  if (!isMap(document.contents)) return [];
  const sources = document.contents.get("sources", true);
  if (!isSeq(sources)) return [];
  const out: string[] = [];
  for (const source of sources.items) {
    if (!isMap(source)) continue;
    const specs = source.get("specs", true);
    if (!isMap(specs)) continue;
    for (const specPair of specs.items) {
      const spec = specPair.value;
      const operations = isMap(spec) ? spec.get("operations", true) : null;
      if (!isMap(operations)) continue;
      for (const operationPair of operations.items) {
        out.push(
          `${source.get("name")}:${String((specPair.key as { value?: unknown })?.value ?? specPair.key)}:${String((operationPair.key as { value?: unknown })?.value ?? operationPair.key)}`,
        );
      }
    }
  }
  return out.sort();
}

/** Current read modes of database sources, keyed by source name ("" when unset). */
export function readDatabaseSourceReadModes(raw: string): Record<string, string> {
  const document = parseDocument(raw);
  if (!isMap(document.contents)) return {};
  const sources = document.contents.get("sources", true);
  if (!isSeq(sources)) return {};
  const modes: Record<string, string> = {};
  for (const source of sources.items) {
    if (!isMap(source)) continue;
    const kind = String(source.get("kind") ?? "database").toLowerCase();
    if (NON_DATABASE_KINDS.has(kind)) continue;
    const access = source.get("access", true);
    modes[String(source.get("name") ?? "")] = isMap(access) ? String(access.get("read") ?? "") : "";
  }
  return modes;
}

/**
 * Undoes applyGroupGrantsToConfig: removes og_* roles, grants and allowed
 * roles, restores each database source's previous read mode and returns
 * GraphJin to first role mode.
 */
export function removeGroupGrantsFromConfig(raw: string, previousReadModes: Record<string, string>): { content: string; changed: boolean } {
  const cleared = applyGroupGrantsToConfig(raw, { roles: [], grants: new Map(), apiOperations: new Map() }, previousReadModes).content;
  const document = parseDocument(cleared);
  if (!isMap(document.contents) || !document.contents.has("sources")) return { content: raw, changed: false };
  const root = document.contents;
  const identity = mapOf(root, "identity");
  identity.set("role_mode", "first");
  identity.delete("group_claims");
  for (const source of listOf(root, "sources").items) {
    if (!isMap(source)) continue;
    const name = String(source.get("name") ?? "");
    if (!(name in previousReadModes)) continue;
    const access = mapOf(source, "access");
    const previous = previousReadModes[name];
    if (previous) access.set("read", previous);
    else access.delete("read");
    if (access.items.length === 0) source.delete("access");
  }
  const content = document.toString();
  return { content, changed: content !== raw };
}

type PlainConfig = {
  identity?: Record<string, unknown>;
  roles?: Array<Record<string, unknown>>;
  sources?: Array<Record<string, unknown>>;
};

function plain(raw: string): PlainConfig {
  const value = parseDocument(raw).toJSON() as PlainConfig | null;
  return value && typeof value === "object" ? value : {};
}

/** after, with null for each key that before had and after dropped (a merge patch). */
function mergePatch(before: Record<string, unknown> | undefined, after: Record<string, unknown> | undefined): Record<string, unknown> {
  const out: Record<string, unknown> = { ...(after ?? {}) };
  for (const key of Object.keys(before ?? {})) if (!(key in out)) out[key] = null;
  return out;
}

const same = (a: unknown, b: unknown) => JSON.stringify(a ?? null) === JSON.stringify(b ?? null);

/**
 * The gj_config update that makes a running GraphJin match a config file that
 * applyGroupGrantsToConfig or removeGroupGrantsFromConfig changed from
 * `before` to `after`: the identity block, the og_* roles, and each source's
 * access and API operation allowed_roles. GraphJin applies it live, so the
 * change needs no restart. Returns null when nothing differs.
 */
export function groupGrantsLivePatch(before: string, after: string): Record<string, unknown> | null {
  const old = plain(before);
  const next = plain(after);
  const update: Record<string, unknown> = {};

  if (!same(old.identity, next.identity)) update.identity = mergePatch(old.identity, next.identity);

  const oldGroupRoles = (old.roles ?? []).filter((role) => isGroupRole(role.name));
  const nextGroupRoles = (next.roles ?? []).filter((role) => isGroupRole(role.name));
  const changedRoles = nextGroupRoles.filter((role) => !oldGroupRoles.some((o) => same(o, role)));
  if (changedRoles.length > 0) update.roles = changedRoles;
  const removed = oldGroupRoles.map((role) => String(role.name)).filter((name) => !nextGroupRoles.some((role) => role.name === name));
  if (removed.length > 0) update.remove_roles = removed;

  const patches: Array<Record<string, unknown>> = [];
  for (const source of next.sources ?? []) {
    const name = String(source.name ?? "");
    const prior = (old.sources ?? []).find((item) => String(item.name ?? "") === name);
    if (!prior) continue;
    const patch: Record<string, unknown> = { name };
    if (!same(prior.access, source.access)) {
      patch.access = source.access === undefined ? null : mergePatch(prior.access as Record<string, unknown>, source.access as Record<string, unknown>);
    }
    const specs: Record<string, unknown> = {};
    for (const [specName, spec] of Object.entries((source.specs ?? {}) as Record<string, { operations?: Record<string, { allowed_roles?: unknown }> }>)) {
      const priorOps = ((prior.specs ?? {}) as Record<string, { operations?: Record<string, { allowed_roles?: unknown }> }>)[specName]?.operations ?? {};
      const operations: Record<string, unknown> = {};
      for (const [operationName, operation] of Object.entries(spec?.operations ?? {})) {
        if (!same(priorOps[operationName]?.allowed_roles, operation?.allowed_roles)) {
          operations[operationName] = { allowed_roles: operation?.allowed_roles ?? null };
        }
      }
      if (Object.keys(operations).length > 0) specs[specName] = { operations };
    }
    if (Object.keys(specs).length > 0) patch.specs = specs;
    if (Object.keys(patch).length > 1) patches.push(patch);
  }
  if (patches.length > 0) update.source_patches = patches;

  return Object.keys(update).length > 0 ? update : null;
}
