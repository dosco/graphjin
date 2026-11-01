import { randomUUID } from "node:crypto";
import { open, readFile, rename, rm, stat, writeFile } from "node:fs/promises";
import { basename, dirname, join } from "node:path";
import { isMap, isSeq, parseDocument, YAMLMap, YAMLSeq } from "yaml";

type SourcePatch = Record<string, unknown> & { name?: unknown };

const GRAPHJIN_CONFIG_LOCK_STALE_MS = 10 * 60 * 1_000;

/**
 * Serialize durable GraphJin config writers across web/worker processes.
 * GraphJin's catalog revision protects the live engine; this lock provides
 * the matching optimistic boundary for the shared YAML file.
 */
export async function acquireGraphjinConfigLock(input: {
  configFile: string;
}): Promise<() => Promise<void>> {
  const lockPath = join(
    /* turbopackIgnore: true */ dirname(input.configFile),
    `.${basename(input.configFile)}.openneko.lock`,
  );
  const token = randomUUID();
  const createLock = async () => {
    const handle = await open(
      /* turbopackIgnore: true */ lockPath,
      "wx",
      0o600,
    );
    try {
      await handle.writeFile(token);
      await handle.sync();
    } finally {
      await handle.close();
    }
  };
  try {
    await createLock();
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "EEXIST") throw error;
    const metadata = await stat(
      /* turbopackIgnore: true */ lockPath,
    ).catch(() => null);
    if (!metadata || Date.now() - metadata.mtimeMs <= GRAPHJIN_CONFIG_LOCK_STALE_MS) {
      throw new Error("GraphJin config has another approved change in progress");
    }
    await rm(/* turbopackIgnore: true */ lockPath, { force: true });
    await createLock().catch((retryError) => {
      if ((retryError as NodeJS.ErrnoException).code === "EEXIST") {
        throw new Error("GraphJin config has another approved change in progress");
      }
      throw retryError;
    });
  }
  return async () => {
    const currentToken = await readFile(
      /* turbopackIgnore: true */ lockPath,
      "utf8",
    ).catch(() => "");
    if (currentToken === token) {
      await rm(/* turbopackIgnore: true */ lockPath, { force: true });
    }
  };
}

function sourceName(patch: SourcePatch): string {
  const name = typeof patch.name === "string" ? patch.name.trim() : "";
  if (!name) throw new Error("GraphJin source persistence needs a source name");
  return name;
}

function secretRefSegment(value: string): string {
  return value
    .trim()
    .replaceAll("/", "_")
    .replaceAll("\\", "_")
    .replaceAll("\0", "_");
}

function isSensitiveConfigKey(key: string): boolean {
  const normalized = key.toLowerCase().replaceAll("-", "_");
  return (
    normalized.includes("password") ||
    normalized.includes("secret") ||
    normalized.includes("token") ||
    normalized.includes("passphrase") ||
    normalized.includes("private_key") ||
    normalized.includes("client_key") ||
    normalized.includes("api_key") ||
    normalized.includes("apikey") ||
    normalized.includes("authorization") ||
    normalized.includes("cookie") ||
    normalized === "connection_string" ||
    normalized === "key_value"
  );
}

function shouldReplaceWithSecretRef(value: string): boolean {
  const trimmed = value.trim();
  return Boolean(trimmed) && !trimmed.startsWith("gjsecret://") && !value.includes("${");
}

function sealSourceForDisk(
  value: unknown,
  path: string[],
  parentKey = "",
): unknown {
  if (Array.isArray(value)) {
    return value.map((item, index) => {
      const named = item && typeof item === "object" && !Array.isArray(item) &&
        typeof (item as Record<string, unknown>).name === "string"
        ? String((item as Record<string, unknown>).name)
        : String(index);
      return sealSourceForDisk(item, [...path, secretRefSegment(named)]);
    });
  }
  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value as Record<string, unknown>).map(([key, child]) => [
        key,
        sealSourceForDisk(child, [...path, secretRefSegment(key)], key),
      ]),
    );
  }
  if (
    typeof value === "string" &&
    (isSensitiveConfigKey(parentKey) ||
      (path.length >= 2 && path.at(-2)?.toLowerCase() === "headers" && isSensitiveConfigKey(parentKey))) &&
    shouldReplaceWithSecretRef(value)
  ) {
    return `gjsecret://${path.filter(Boolean).join("/")}`;
  }
  return value;
}

function sourceForDisk(patch: SourcePatch): SourcePatch {
  const name = sourceName(patch);
  return sealSourceForDisk(
    structuredClone(patch),
    ["sources", secretRefSegment(name)],
  ) as SourcePatch;
}

function mapByName(sources: YAMLSeq, name: string): YAMLMap | null {
  for (const item of sources.items) {
    if (isMap(item) && item.get("name") === name) return item;
  }
  return null;
}

export async function graphjinConfigHasSource(input: {
  configFile: string;
  sourceName: string;
}): Promise<boolean> {
  const raw = await readFile(
    /* turbopackIgnore: true */ input.configFile,
    "utf8",
  );
  const document = parseDocument(raw);
  if (document.errors.length > 0 || !isMap(document.contents)) {
    throw new Error("GraphJin config is unavailable for source validation");
  }
  const sources = document.get("sources", true);
  if (sources == null) return false;
  if (!isSeq(sources)) {
    throw new Error("GraphJin config sources must be a YAML list");
  }
  return mapByName(sources, input.sourceName) !== null;
}

function mergeMap(target: YAMLMap, patch: Record<string, unknown>): void {
  for (const [key, value] of Object.entries(patch)) {
    const current = target.get(key, true);
    if (
      value &&
      typeof value === "object" &&
      !Array.isArray(value) &&
      isMap(current)
    ) {
      mergeMap(current, value as Record<string, unknown>);
    } else {
      target.set(key, value);
    }
  }
}

/**
 * Persist the already-approved source portion of a GraphJin config update.
 * GraphJin intentionally keeps production-mode gj_config writes in memory;
 * OpenNeko owns durability for managed sources because its approval worker is
 * the trusted writer that shares the config volume.
 */
export async function persistGraphjinSourceConfigUpdate(input: {
  configFile: string;
  update: Record<string, unknown>;
}): Promise<void> {
  const raw = await readFile(
    /* turbopackIgnore: true */ input.configFile,
    "utf8",
  );
  const document = parseDocument(raw);
  if (document.errors.length > 0) {
    throw new Error(
      `GraphJin config is not valid YAML: ${document.errors.map((error) => error.message).join("; ")}`,
    );
  }
  if (!isMap(document.contents)) {
    throw new Error("GraphJin config root must be a YAML object");
  }

  let sourcesNode = document.get("sources", true);
  if (sourcesNode == null) {
    sourcesNode = new YAMLSeq();
    document.set("sources", sourcesNode);
  }
  if (!isSeq(sourcesNode)) {
    throw new Error("GraphJin config sources must be a YAML list");
  }

  const updates = input.update.update_sources;
  if (Array.isArray(updates)) {
    for (const value of updates) {
      if (!value || typeof value !== "object" || Array.isArray(value)) {
        throw new Error("GraphJin update_sources contains an invalid source");
      }
      const patch = sourceForDisk(value as SourcePatch);
      const name = sourceName(patch);
      const existing = mapByName(sourcesNode, name);
      if (existing) mergeMap(existing, patch);
      else sourcesNode.add(document.createNode(patch));
    }
  }

  const removals = input.update.remove_sources;
  if (Array.isArray(removals)) {
    const names = new Set(
      removals.map((value, index) => {
        if (typeof value !== "string" || !value.trim()) {
          throw new Error(`GraphJin remove_sources[${index}] must be a source name`);
        }
        return value.trim().toLowerCase();
      }),
    );
    sourcesNode.items = sourcesNode.items.filter(
      (item) => !isMap(item) || !names.has(String(item.get("name") ?? "").trim().toLowerCase()),
    );
  }

  const accessPatches = input.update.source_patches;
  if (Array.isArray(accessPatches)) {
    for (const value of accessPatches) {
      if (!value || typeof value !== "object" || Array.isArray(value)) {
        throw new Error("GraphJin source_patches contains an invalid source");
      }
      const patch = value as SourcePatch;
      const name = sourceName(patch);
      const existing = mapByName(sourcesNode, name);
      if (!existing) {
        throw new Error(`GraphJin source ${name} is absent from the durable config`);
      }
      mergeMap(existing, patch);
    }
  }

  const previousMode = (
    await stat(/* turbopackIgnore: true */ input.configFile)
  ).mode & 0o777;
  const temporary = join(
    /* turbopackIgnore: true */ dirname(input.configFile),
    `.${randomUUID()}.graphjin-config.tmp`,
  );
  await writeFile(
    /* turbopackIgnore: true */ temporary,
    document.toString(),
    { mode: previousMode },
  );
  await rename(
    /* turbopackIgnore: true */ temporary,
    input.configFile,
  );
}
