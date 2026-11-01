import { createHash } from "node:crypto";
import {
  assertManagedFileSourceName,
  validateFilePresignTtl,
  validateObjectStoreBucket,
  validateObjectStoreEndpoint,
  validatePublicFileBaseUrl,
} from "./file-source";
import { parseManagedFileSourceManifest } from "./file-source-assets";

export type ResolveSourceSecret = (name: string) => Promise<string>;

function canonicalConfigValue(value: unknown): string {
  if (value === null || typeof value !== "object") {
    return JSON.stringify(value) ?? "null";
  }
  if (Array.isArray(value)) {
    return `[${value.map(canonicalConfigValue).join(",")}]`;
  }
  const entries = Object.entries(value as Record<string, unknown>)
    .filter(([, item]) => item !== undefined)
    .sort(([left], [right]) => left.localeCompare(right));
  return `{${entries
    .map(
      ([key, item]) =>
        `${JSON.stringify(key)}:${canonicalConfigValue(item)}`,
    )
    .join(",")}}`;
}

/** Stable across JSONB key reordering between preview and approved execution. */
export function graphjinConfigPatchHash(payload: Record<string, unknown>): string {
  return createHash("sha256")
    .update(canonicalConfigValue(payload))
    .digest("hex");
}

/** Serialize a JS value to GraphJin's inline gj_config input syntax. */
export function graphjinInputValue(value: unknown): string {
  if (value === null || value === undefined) return "null";
  if (typeof value === "string") return JSON.stringify(value);
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  if (Array.isArray(value)) {
    return `[${value.map(graphjinInputValue).join(", ")}]`;
  }
  const entries = Object.entries(value as Record<string, unknown>)
    .filter(([, item]) => item !== undefined)
    .map(([key, item]) => `${key}: ${graphjinInputValue(item)}`);
  return `{ ${entries.join(", ")} }`;
}

export function graphjinInputWithJsonVariables(value: unknown): {
  literal: string;
  variableDefinitions: string;
  variables: Record<string, unknown>;
} {
  const variables: Record<string, unknown> = {};
  let sequence = 0;
  const encode = (item: unknown): string => {
    if (item === null || item === undefined) return "null";
    if (typeof item === "string") return JSON.stringify(item);
    if (typeof item === "number" || typeof item === "boolean") {
      return String(item);
    }
    if (Array.isArray(item)) return `[${item.map(encode).join(", ")}]`;
    const record = item as Record<string, unknown>;
    if (Object.keys(record).some((key) => !/^[_A-Za-z][_0-9A-Za-z]*$/.test(key))) {
      const name = `json${++sequence}`;
      variables[name] = record;
      return `$${name}`;
    }
    return `{ ${Object.entries(record)
      .filter(([, entry]) => entry !== undefined)
      .map(([key, entry]) => `${key}: ${encode(entry)}`)
      .join(", ")} }`;
  };
  const literal = encode(value);
  const names = Object.keys(variables);
  return {
    literal,
    variableDefinitions:
      names.length > 0
        ? `(${names.map((name) => `$${name}: JSON!`).join(", ")})`
        : "",
    variables,
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

const WRITABLE_SOURCE_KINDS = new Set(["api", "file"]);

/**
 * Customer databases are never written. Reject any proposal that would open a
 * write path on a database source (or on a source of unknown kind), whether
 * through `read_only`, `access`, `capabilities`, or OpenAPI operation
 * exposure. Runs at preview and again at approved execution.
 */
export function assertDatabaseSourcesStayReadOnly(
  update: Record<string, unknown>,
  currentSources: unknown,
): void {
  const existing = new Map<string, Record<string, unknown>>();
  if (Array.isArray(currentSources)) {
    for (const source of currentSources) {
      if (isRecord(source) && typeof source.name === "string") {
        existing.set(source.name.trim().toLowerCase(), source);
      }
    }
  }
  const entries = [
    ...(Array.isArray(update.update_sources) ? update.update_sources : []),
    ...(Array.isArray(update.source_patches) ? update.source_patches : []),
  ];
  for (const entry of entries) {
    if (!isRecord(entry)) continue;
    const name = String(entry.name ?? "").trim();
    const current = existing.get(name.toLowerCase());
    const kind = String(entry.kind ?? current?.kind ?? "database");
    if (WRITABLE_SOURCE_KINDS.has(kind)) continue;

    const violations: string[] = [];
    if (entry.read_only === false) violations.push("read_only: false");
    const access = isRecord(entry.access) ? entry.access : {};
    for (const mode of ["write", "delete"] as const) {
      if (access[mode] !== undefined && access[mode] !== "blocked") {
        violations.push(`access.${mode}: ${String(access[mode])}`);
      }
    }
    const capabilities = isRecord(entry.capabilities) ? entry.capabilities : {};
    for (const key of ["api.write", "api.delete"]) {
      if (capabilities[key] === true) violations.push(`capabilities.${key}: true`);
    }
    if (entry.specs !== undefined) violations.push("specs (API operations)");
    if (violations.length > 0) {
      throw new Error(
        `database source "${name}" stays read-only; rejected ${violations.join(", ")}`,
      );
    }
  }
}

/**
 * Convert OpenNeko's narrow, typed source-config proposal into GraphJin's
 * additive patch shape. The optional resolver is trusted host code; a secret
 * value is never accepted from, or returned to, the model.
 */
export async function buildGraphjinConfigUpdate(
  payload: Record<string, unknown>,
  resolveSecret?: ResolveSourceSecret,
  options?: {
    managedOpenApiSpecsDir?: string;
    managedLocalFilesRoot?: string;
  },
): Promise<{ update: Record<string, unknown>; secretName: string | null }> {
  const action = String(payload.action ?? "");
  const update: Record<string, unknown> = {};
  let secretName: string | null = null;

  if (action === "add_role") {
    const name = String(payload.name ?? "").trim();
    const match = String(payload.match ?? "").trim();
    if (!name || !match) throw new Error("add_role needs name + match");
    const role: Record<string, unknown> = { name, match };
    if (typeof payload.comment === "string" && payload.comment.trim()) {
      role.comment = payload.comment.trim();
    }
    update.roles = [role];
    return { update, secretName };
  }

  if (action === "set_source_access") {
    const source = String(payload.source ?? "").trim();
    if (!source) throw new Error("set_source_access needs source");
    const access: Record<string, unknown> = {};
    if (payload.read) access.read = String(payload.read);
    if (payload.write) access.write = String(payload.write);
    if (payload.delete) access.delete = String(payload.delete);
    if (Object.keys(access).length === 0) {
      throw new Error("set_source_access needs at least one access field");
    }
    update.source_patches = [{ name: source, access }];
    return { update, secretName };
  }

  if (action === "set_source_capabilities") {
    const source = String(payload.source ?? "").trim();
    if (!source) throw new Error("set_source_capabilities needs source");
    const capabilities: Record<string, boolean> = {};
    if (typeof payload.apiWrite === "boolean") {
      capabilities["api.write"] = payload.apiWrite;
    }
    if (typeof payload.apiDelete === "boolean") {
      capabilities["api.delete"] = payload.apiDelete;
    }
    if (Object.keys(capabilities).length === 0) {
      throw new Error("set_source_capabilities needs apiWrite or apiDelete");
    }
    update.update_sources = [{ name: source, capabilities }];
    return { update, secretName };
  }

  if (action === "expose_api_operation") {
    const source = String(payload.source ?? "").trim();
    const spec = String(payload.spec ?? "").trim();
    const operation = String(payload.operation ?? "").trim();
    if (!source || !spec || !operation) {
      throw new Error("expose_api_operation needs source, spec, and operation");
    }
    const exposeMutation = payload.exposeMutation === true;
    const allowedRoles = Array.isArray(payload.allowedRoles)
      ? payload.allowedRoles
          .map((role) => String(role).trim().toLowerCase())
          .filter(Boolean)
      : [];
    if (exposeMutation && allowedRoles.length === 0) {
      throw new Error(
        "expose_api_operation needs allowedRoles when exposeMutation is true",
      );
    }
    const operationPatch: Record<string, unknown> = {
      expose_mutation: exposeMutation,
      allowed_roles: allowedRoles,
    };
    if (typeof payload.exposeAs === "string" && payload.exposeAs.trim()) {
      operationPatch.expose_as = payload.exposeAs.trim();
    }
    update.update_sources = [
      {
        name: source,
        specs: { [spec]: { operations: { [operation]: operationPatch } } },
      },
    ];
    return { update, secretName };
  }

  if (action === "enable_api_writes") {
    const source = String(payload.source ?? "").trim();
    const spec = String(payload.spec ?? "").trim();
    const operation = String(payload.operation ?? "").trim();
    if (!source || !spec || !operation) {
      throw new Error("enable_api_writes needs source, spec, and operation");
    }
    const allowedRoles = Array.isArray(payload.allowedRoles)
      ? payload.allowedRoles
          .map((role) => String(role).trim().toLowerCase())
          .filter(Boolean)
      : [];
    if (allowedRoles.length === 0) {
      throw new Error("enable_api_writes needs allowedRoles");
    }
    const write = String(payload.write ?? "authenticated");
    if (write !== "authenticated" && write !== "admin") {
      throw new Error("enable_api_writes write must be authenticated or admin");
    }
    const operationPatch: Record<string, unknown> = {
      expose_mutation: true,
      allowed_roles: allowedRoles,
    };
    if (typeof payload.exposeAs === "string" && payload.exposeAs.trim()) {
      operationPatch.expose_as = payload.exposeAs.trim();
    }
    const capabilities: Record<string, boolean> = { "api.write": true };
    if (payload.apiDelete === true) capabilities["api.delete"] = true;
    // One update carries every write setting, so a single approval and a
    // single GraphJin apply open the operation.
    // Packs ship API sources with read_only: true; GraphJin's source gate
    // checks that flag before capabilities, access, and allowed_roles.
    update.update_sources = [
      {
        name: source,
        read_only: false,
        capabilities,
        specs: { [spec]: { operations: { [operation]: operationPatch } } },
      },
    ];
    update.source_patches = [{ name: source, access: { write } }];
    return { update, secretName };
  }

  if (action === "register_source") {
    const name = String(payload.name ?? "").trim();
    const selectedString = (value: unknown, fallback = "") => {
      const selected = Array.isArray(value) && value.length === 1 ? value[0] : value;
      return typeof selected === "string" ? selected.trim() : fallback;
    };
    const kind = selectedString(payload.kind, "database");
    if (!name) throw new Error("register_source needs name");
    if (!["database", "api", "file"].includes(kind)) {
      throw new Error(
        `register_source kind must be database, api, or file (received "${kind}")`,
      );
    }
    const source: Record<string, unknown> = { name, kind };
    const copyString = (payloadKey: string, configKey = payloadKey) => {
      const value =
        typeof payload[payloadKey] === "string"
          ? payload[payloadKey].trim()
          : "";
      if (value) source[configKey] = value;
      return value;
    };
    if (kind === "database") {
      // Customer databases are read-only by contract. GraphJin pins this flag
      // at startup, so a later config patch cannot lift it.
      source.read_only = true;
      source.type = String(payload.type ?? "postgres");
      copyString("host");
      if (payload.port) source.port = Number(payload.port);
      copyString("dbname");
      copyString("user");
      const ref =
        typeof payload.secretRef === "string" ? payload.secretRef.trim() : "";
      if (ref) {
        if (!resolveSecret) {
          throw new Error("register_source secret resolution is unavailable");
        }
        secretName = ref;
        source.password = await resolveSecret(ref);
      }
    }
    if (kind === "api") {
      const specAssetId =
        typeof payload.specAssetId === "string" ? payload.specAssetId.trim() : "";
      if (!specAssetId) {
        throw new Error("register_source api needs a managed specAssetId");
      }
      // This path is trusted deployment configuration, never model/user input.
      source.specs_dir = options?.managedOpenApiSpecsDir ?? "/config/specs";
    }
    if (kind === "file") {
      assertManagedFileSourceName(name);
      const backend = selectedString(payload.backend);
      if (backend) source.backend = backend;
      if (!["local", "s3", "gcs"].includes(backend)) {
        throw new Error("register_source file needs backend local, s3, or gcs");
      }
      if (typeof payload.root === "string" && payload.root.trim()) {
        throw new Error(
          "register_source local file roots are managed by OpenNeko and cannot be supplied in a proposal",
        );
      }
      if (backend === "local") {
        parseManagedFileSourceManifest(payload.localFiles, name);
        if (!options?.managedLocalFilesRoot) {
          throw new Error(
            "register_source local file needs OpenNeko managed storage",
          );
        }
        source.root = options.managedLocalFilesRoot;
      }
      if (backend === "s3" || backend === "gcs") {
        const bucket = copyString("bucket");
        if (!bucket) throw new Error(`register_source ${backend} file needs bucket`);
        validateObjectStoreBucket(backend, bucket);
        const prefix = copyString("prefix");
        if (prefix.startsWith("/") || prefix.includes("\0")) {
          throw new Error(
            "register_source file prefix must be a relative object-key prefix",
          );
        }
        if (backend === "s3") copyString("region");
        if (typeof payload.endpoint === "string" && payload.endpoint.trim()) {
          if (backend !== "s3") {
            throw new Error("register_source file endpoint is supported only for S3");
          }
          source.endpoint = validateObjectStoreEndpoint(payload.endpoint.trim());
        }
        if (
          typeof payload.publicBaseUrl === "string" &&
          payload.publicBaseUrl.trim()
        ) {
          source.public_base_url = validatePublicFileBaseUrl(
            payload.publicBaseUrl.trim(),
          );
        }
        if (typeof payload.presignTtl === "string" && payload.presignTtl.trim()) {
          source.presign_ttl = validateFilePresignTtl(payload.presignTtl.trim());
        }
      }
      source.read_only = true;
      source.max_list_page_size = 500;
      source.capabilities = {
        "files.list": true,
        "files.read": true,
        "files.write": false,
        "files.delete": false,
        "files.watch": false,
      };
    }
    source.access =
      kind === "file"
        ? { read: "authenticated", write: "blocked", delete: "blocked" }
        : {
            read: String(payload.read ?? "authenticated"),
            write: String(payload.write ?? "blocked"),
            delete: String(payload.delete ?? "blocked"),
          };
    update.update_sources = [source];
    return { update, secretName };
  }

  throw new Error(`unknown source config action "${action}"`);
}
