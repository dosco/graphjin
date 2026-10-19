// Deployment secrets store. The default location is
// $XDG_CONFIG_HOME/openneko/secrets.json (default
// ~/.config/openneko/secrets.json). Two sections:
//
//   1. Top-level keys → deployment-wide env-var bags keyed by section:
//      `channel.slack`, `signin.scalekit`, `pack.<id>`, … Used for static
//      API keys and tokens shared by every operator on this install.
//
//   2. `_operators` → OAuth credentials of connector consents. Keyed by
//      operator id (or the reserved `deployment` slot), then by connector.
//      Each credential is an opaque token blob the connector owns
//      (OpenNeko persists; the connector reads and refreshes it).
//
// File is written with 0600 perms so other users on the host can't
// read it.
//
// Backwards compatibility: older files have no `_operators` key. They
// load unchanged; the section is created lazily on first connect.
import { chmodSync, existsSync } from "node:fs";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { maybeDecryptSecret, maybeEncryptSecret } from "@neko/secret-crypt";

/**
 * Top-level shape of the secrets file:
 *
 *   { "<section>":  { ENV_KEY: "value", ... },
 *     "_operators": { "<operator_id>": { "<connector>": ConnectorCredential } } }
 *
 * The first form is exposed as a plain `Record<section, Record<env, string>>`
 * so callers that only care about deployment-wide env bags keep the
 * shape they were written against.
 */
export type SecretsStore = Record<string, Record<string, string>>;

/**
 * Credential produced by a connector's OAuth consent.
 * `tokens` is an opaque blob owned by the connector — OpenNeko persists
 * and reinjects but doesn't interpret the shape (access_token /
 * refresh_token / id_token / expires_in vary by provider).
 */
export interface ConnectorCredential {
  /** Opaque token blob the connector owns. */
  tokens: Record<string, unknown>;
  /** OAuth scopes granted, if applicable. */
  scopes?: string[];
  /** Short human-readable provider name, recorded at connect time. */
  providerLabel?: string;
  /** ISO timestamp when the operator originally connected. */
  connectedAt: string;
  /** ISO timestamp last touched by a refresh-token writeback. */
  refreshedAt?: string;
}

/** Reserved top-level key in the secrets file; never a section name. */
export const OPERATORS_KEY = "_operators";

/** Full file shape including the per-operator section. */
export interface FullSecretsFile {
  env: SecretsStore;
  operators: Record<string, Record<string, ConnectorCredential>>;
}

const STORE_FILENAME = "secrets.json";

function configDir(): string {
  const xdg = process.env.XDG_CONFIG_HOME;
  if (xdg) return path.join(xdg, "openneko");
  return path.join(process.env.HOME ?? "/tmp", ".config", "openneko");
}

export function defaultSecretsPath(overrideDir?: string): string {
  return path.join(overrideDir ?? configDir(), STORE_FILENAME);
}

/**
 * Read the deployment-wide env-var bags. Per-operator credentials are
 * filtered out — callers that need them use `readFullSecretsFile`.
 */
export async function readSecretsStore(
  overrideDir?: string,
): Promise<SecretsStore> {
  const full = await readFullSecretsFile(overrideDir);
  return full.env;
}

/**
 * Read the entire secrets file including the per-operator section.
 * Throws on invalid JSON or unexpected shape.
 */
export async function readFullSecretsFile(
  overrideDir?: string,
): Promise<FullSecretsFile> {
  const file = defaultSecretsPath(overrideDir);
  if (!existsSync(file)) return { env: {}, operators: {} };
  const raw = await readFile(file, "utf8");
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch (err) {
    throw new Error(
      `secrets store at ${file} is invalid JSON: ${err instanceof Error ? err.message : err}`,
    );
  }
  if (typeof parsed !== "object" || parsed === null) {
    throw new Error(`secrets store at ${file} has unexpected shape`);
  }
  return parseFullSecretsFile(parsed as Record<string, unknown>);
}

function parseFullSecretsFile(
  parsed: Record<string, unknown>,
): FullSecretsFile {
  const env: SecretsStore = {};
  const operators: Record<string, Record<string, ConnectorCredential>> = {};
  for (const [key, value] of Object.entries(parsed)) {
    if (key === OPERATORS_KEY) {
      if (typeof value !== "object" || value === null) continue;
      for (const [opId, connectors] of Object.entries(
        value as Record<string, unknown>,
      )) {
        if (typeof connectors !== "object" || connectors === null) continue;
        const inner: Record<string, ConnectorCredential> = {};
        for (const [connector, cred] of Object.entries(
          connectors as Record<string, unknown>,
        )) {
          const parsedCred = parseCredential(cred);
          if (parsedCred) inner[connector] = parsedCred;
        }
        if (Object.keys(inner).length > 0) operators[opId] = inner;
      }
      continue;
    }
    if (typeof value !== "object" || value === null) continue;
    const map: Record<string, string> = {};
    for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
      if (typeof v === "string") map[k] = maybeDecryptSecret(v);
    }
    env[key] = map;
  }
  return { env, operators };
}

function parseCredential(value: unknown): ConnectorCredential | null {
  // SEC1 at-rest form: the whole blob serialized + encrypted into one
  // enc:v1 string. Legacy plaintext-object form still parses below.
  if (typeof value === "string") {
    try {
      return parseCredential(JSON.parse(maybeDecryptSecret(value)));
    } catch {
      return null;
    }
  }
  if (typeof value !== "object" || value === null) return null;
  const obj = value as Record<string, unknown>;
  const tokens = obj.tokens;
  const connectedAt = obj.connectedAt;
  if (typeof tokens !== "object" || tokens === null) return null;
  if (typeof connectedAt !== "string") return null;
  const cred: ConnectorCredential = {
    tokens: tokens as Record<string, unknown>,
    connectedAt,
  };
  if (Array.isArray(obj.scopes)) {
    const scopes = obj.scopes.filter((s): s is string => typeof s === "string");
    if (scopes.length > 0) cred.scopes = scopes;
  }
  if (typeof obj.providerLabel === "string") cred.providerLabel = obj.providerLabel;
  if (typeof obj.refreshedAt === "string") cred.refreshedAt = obj.refreshedAt;
  return cred;
}

/**
 * Loader-friendly variant: never throws, never makes the worker boot
 * fail. Invalid JSON or unexpected shape → emit a warning via the
 * caller's logger and return an empty store. The worker uses this;
 * the CLI uses the stricter `readSecretsStore` so operators see
 * errors immediately.
 */
export async function readSecretsStoreSoft(
  overrideDir: string | undefined,
  warn: (line: string) => void = (m) => console.warn(`[secrets] ${m}`),
): Promise<SecretsStore> {
  try {
    return await readSecretsStore(overrideDir);
  } catch (err) {
    warn(`secrets store unreadable; treating as empty: ${err instanceof Error ? err.message : err}`);
    return {};
  }
}

/** Soft variant of `readFullSecretsFile` for the worker's boot path. */
export async function readFullSecretsFileSoft(
  overrideDir: string | undefined,
  warn: (line: string) => void = (m) => console.warn(`[secrets] ${m}`),
): Promise<FullSecretsFile> {
  try {
    return await readFullSecretsFile(overrideDir);
  } catch (err) {
    warn(`secrets store unreadable; treating as empty: ${err instanceof Error ? err.message : err}`);
    return { env: {}, operators: {} };
  }
}

/**
 * Write the env section only — preserves the per-operator section
 * already on disk. Callers that need to update both atomically use
 * `writeFullSecretsFile`.
 */
export async function writeSecretsStore(
  store: SecretsStore,
  overrideDir?: string,
): Promise<void> {
  const current = await readFullSecretsFileSoft(overrideDir, () => {});
  await writeFullSecretsFile({ env: store, operators: current.operators }, overrideDir);
}

export async function writeFullSecretsFile(
  full: FullSecretsFile,
  overrideDir?: string,
): Promise<void> {
  const file = defaultSecretsPath(overrideDir);
  await mkdir(path.dirname(file), { recursive: true, mode: 0o700 });
  const out: Record<string, unknown> = {};
  // Sort sections + keys deterministically so diffs are stable across runs.
  // Values are encrypted at rest (enc:v1); keys stay plaintext so
  // `secrets list` and diffs remain readable.
  for (const pkg of Object.keys(full.env).sort()) {
    const inner = full.env[pkg] ?? {};
    const sorted: Record<string, string> = {};
    for (const k of Object.keys(inner).sort()) {
      sorted[k] = maybeEncryptSecret(inner[k]!);
    }
    out[pkg] = sorted;
  }
  const opIds = Object.keys(full.operators).sort();
  if (opIds.length > 0) {
    const operators: Record<string, Record<string, string>> = {};
    for (const id of opIds) {
      const byConnector = full.operators[id] ?? {};
      const sortedConnectors: Record<string, string> = {};
      for (const connector of Object.keys(byConnector).sort()) {
        sortedConnectors[connector] = maybeEncryptSecret(
          JSON.stringify(byConnector[connector]!),
        );
      }
      operators[id] = sortedConnectors;
    }
    out[OPERATORS_KEY] = operators;
  }
  await writeFile(file, JSON.stringify(out, null, 2) + "\n", "utf8");
  try {
    chmodSync(file, 0o600);
  } catch {
    /* Windows etc; best-effort. */
  }
}

const ENV_KEY_RX = /^[A-Z][A-Z0-9_]*$/;

export function isValidEnvKey(key: string): boolean {
  return ENV_KEY_RX.test(key);
}

export function setSecret(
  store: SecretsStore,
  section: string,
  key: string,
  value: string,
): SecretsStore {
  if (!isValidEnvKey(key)) {
    throw new Error(`env key "${key}" must be UPPER_SNAKE_CASE`);
  }
  const existing = store[section] ?? {};
  return {
    ...store,
    [section]: { ...existing, [key]: value },
  };
}

export function unsetSecret(
  store: SecretsStore,
  section: string,
  key: string,
): { store: SecretsStore; removed: boolean } {
  const existing = store[section];
  if (!existing || !(key in existing)) {
    return { store, removed: false };
  }
  const { [key]: _removed, ...rest } = existing;
  void _removed;
  const next: SecretsStore = { ...store, [section]: rest };
  if (Object.keys(rest).length === 0) {
    const { [section]: _gone, ...withoutSection } = next;
    void _gone;
    return { store: withoutSection, removed: true };
  }
  return { store: next, removed: true };
}

export function listKeysForSection(
  store: SecretsStore,
  section: string,
): string[] {
  return Object.keys(store[section] ?? {}).sort();
}

/**
 * All distinct secret values across the store. Used by the env-scrubber
 * in `@neko/llm` to build a redaction regex applied to agent output —
 * accidentally-leaked secrets get replaced with [REDACTED] before
 * they reach work_memory, run replays, or the Briefing.
 *
 * Includes both the env-var bag values and any token strings nested
 * inside per-operator credential blobs. Token blobs are opaque to
 * OpenNeko, so we walk values recursively and collect every string.
 */
export function allSecretValues(store: SecretsStore): string[] {
  const out = new Set<string>();
  for (const env of Object.values(store)) {
    for (const v of Object.values(env)) {
      if (v) out.add(v);
    }
  }
  return [...out];
}

/** Like `allSecretValues` but walks both env and per-operator credentials. */
export function allSecretValuesFull(full: FullSecretsFile): string[] {
  const out = new Set<string>();
  for (const env of Object.values(full.env)) {
    for (const v of Object.values(env)) {
      if (v) out.add(v);
    }
  }
  for (const byConnector of Object.values(full.operators)) {
    for (const cred of Object.values(byConnector)) {
      collectStrings(cred.tokens, out);
    }
  }
  return [...out];
}

function collectStrings(node: unknown, out: Set<string>): void {
  if (typeof node === "string") {
    if (node) out.add(node);
    return;
  }
  if (Array.isArray(node)) {
    for (const item of node) collectStrings(item, out);
    return;
  }
  if (typeof node === "object" && node !== null) {
    for (const v of Object.values(node)) collectStrings(v, out);
  }
}

// ─── Per-operator credentials ─────────────────────────────────────────

const OPERATOR_ID_RX = /^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$/;

export function isValidOperatorId(id: string): boolean {
  return OPERATOR_ID_RX.test(id);
}

export function setOperatorCredential(
  full: FullSecretsFile,
  operatorId: string,
  section: string,
  credential: ConnectorCredential,
): FullSecretsFile {
  if (!isValidOperatorId(operatorId)) {
    throw new Error(
      `operator id "${operatorId}" must be alphanumeric (with optional _ -) and up to 128 chars`,
    );
  }
  const existing = full.operators[operatorId] ?? {};
  return {
    env: full.env,
    operators: {
      ...full.operators,
      [operatorId]: { ...existing, [section]: credential },
    },
  };
}

export function getOperatorCredential(
  full: FullSecretsFile,
  operatorId: string,
  section: string,
): ConnectorCredential | null {
  return full.operators[operatorId]?.[section] ?? null;
}

export function unsetOperatorCredential(
  full: FullSecretsFile,
  operatorId: string,
  section: string,
): { store: FullSecretsFile; removed: boolean } {
  const byConnector = full.operators[operatorId];
  if (!byConnector || !(section in byConnector)) {
    return { store: full, removed: false };
  }
  const { [section]: _removed, ...rest } = byConnector;
  void _removed;
  const operators = { ...full.operators, [operatorId]: rest };
  if (Object.keys(rest).length === 0) {
    const { [operatorId]: _gone, ...withoutOperator } = operators;
    void _gone;
    return { store: { env: full.env, operators: withoutOperator }, removed: true };
  }
  return { store: { env: full.env, operators }, removed: true };
}

/**
 * List every section a given operator has connected, sorted by section
 * name. Used by the /integrations API to render per-operator connect
 * status.
 */
export function listConnectorsForOperator(
  full: FullSecretsFile,
  operatorId: string,
): string[] {
  return Object.keys(full.operators[operatorId] ?? {}).sort();
}

/**
 * Inverse: every operator that has connected a given section. Used by
 * the /integrations API admin view ("who has connected Google?").
 */
export function listOperatorsForConnector(
  full: FullSecretsFile,
  section: string,
): string[] {
  const out: string[] = [];
  for (const [opId, byConnector] of Object.entries(full.operators)) {
    if (section in byConnector) out.push(opId);
  }
  return out.sort();
}
