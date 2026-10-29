// The admin GraphJin config file editor. Admins edit the whole config file
// as YAML, with secrets shown as <hidden>; a save puts the hidden values back,
// has GraphJin check and apply the edited text live (gj_config config_yaml),
// and then writes the file, so the running GraphJin and the file never
// disagree and nothing restarts.

import { createHash, randomUUID } from "node:crypto";
import { readFile, rename, rm, stat, writeFile } from "node:fs/promises";
import { isMap, isScalar, isSeq, parseDocument, Scalar, type Document } from "yaml";
import { data_source, db, desc, eq } from "@neko/db";
import { graphjinQuery } from "./client";
import { applyGraphjinConfigUpdate, currentRevisionFromConflict, gjConfigRow, isCatalogRevisionConflict } from "./config-apply";
import { graphjinInputWithJsonVariables } from "./config-change";
import { acquireGraphjinConfigLock } from "./persist-source-config";
import { mintGraphjinToken } from "./token";

/** What the editor shows in place of a secret value. */
export const HIDDEN_VALUE = "<hidden>";

const SECRET_KEYS = new Set([
  "secret", "password", "passwd", "token", "key", "keys", "previous_keys", "api_key", "apikey",
  "private_key", "client_secret", "connection_string", "conn_string", "dsn", "access_token", "refresh_token",
]);

/** Keys whose values are secrets; *_env and *_file name where a secret lives. */
export function isSecretKey(key: string): boolean {
  const k = key.toLowerCase();
  if (k.endsWith("_env") || k.endsWith("_file")) return false;
  return SECRET_KEYS.has(k) || /_(secret|password|token|private_key)$/.test(k);
}

/** A reference to a secret, not the secret: shown as written. */
function isSecretReference(value: string): boolean {
  return value === "" || value.startsWith("gjsecret://") || /^\$\{?[A-Za-z_][A-Za-z0-9_]*\}?$/.test(value);
}

type Path = string[];

/**
 * Calls visit for each scalar under a secret key. A list item that is a map
 * with a name is addressed by that name, so reordering a list such as
 * sources keeps each hidden password with its own entry.
 */
function walkSecrets(node: unknown, path: Path, secret: boolean, visit: (scalar: Scalar, path: Path) => void): void {
  if (isMap(node)) {
    for (const pair of node.items) {
      const key = isScalar(pair.key) ? String(pair.key.value) : String(pair.key);
      walkSecrets(pair.value, [...path, key], secret || isSecretKey(key), visit);
    }
  } else if (isSeq(node)) {
    node.items.forEach((item, index) => {
      const name = isMap(item) ? item.get("name") : undefined;
      walkSecrets(item, [...path, typeof name === "string" && name ? `[name=${name}]` : String(index)], secret, visit);
    });
  } else if (secret && isScalar(node)) {
    visit(node, path);
  }
}

function secretValues(document: Document): Map<string, Scalar> {
  const values = new Map<string, Scalar>();
  walkSecrets(document.contents, [], false, (scalar, path) => {
    if (typeof scalar.value !== "string" || !isSecretReference(scalar.value)) values.set(path.join("."), scalar);
  });
  return values;
}

function parse(text: string, what: string): Document {
  const document = parseDocument(text);
  if (document.errors.length > 0) {
    throw new GraphjinConfigEditorError(`${what} is not valid YAML: ${document.errors[0]!.message}`);
  }
  if (!isMap(document.contents)) throw new GraphjinConfigEditorError(`${what} must be a YAML mapping`);
  return document;
}

export class GraphjinConfigEditorError extends Error {
  constructor(message: string, readonly status = 400) {
    super(message);
  }
}

export function configHash(text: string): string {
  return createHash("sha256").update(text).digest("hex");
}

/** The file with its secrets hidden. */
export function maskGraphjinConfig(raw: string): { text: string; hidden: number } {
  const document = parse(raw, "The config file");
  let hidden = 0;
  walkSecrets(document.contents, [], false, (scalar) => {
    if (typeof scalar.value === "string" && isSecretReference(scalar.value)) return;
    scalar.value = HIDDEN_VALUE;
    scalar.type = Scalar.PLAIN;
    hidden++;
  });
  return { text: document.toString(), hidden };
}

/**
 * The edited text with every <hidden> put back to the value it hid. A hidden
 * value with no secret at the same place in the file is refused rather than
 * guessed.
 */
export function unmaskGraphjinConfig(raw: string, edited: string): string {
  const original = secretValues(parse(raw, "The config file"));
  const document = parse(edited, "The edited config");
  const missing: string[] = [];
  walkSecrets(document.contents, [], false, (scalar, path) => {
    if (scalar.value !== HIDDEN_VALUE) return;
    const key = path.join(".");
    if (!original.has(key)) {
      missing.push(key);
      return;
    }
    const saved = original.get(key)!;
    scalar.value = saved.value;
    scalar.type = saved.type;
  });
  if (missing.length > 0) {
    throw new GraphjinConfigEditorError(
      `${missing.join(", ")} ${missing.length === 1 ? "is" : "are"} ${HIDDEN_VALUE} but the file has no saved value there. Enter the value.`,
    );
  }
  const text = document.toString();
  if (text.includes(HIDDEN_VALUE)) {
    // Left under a key that is not a secret, where it stands for nothing.
    throw new GraphjinConfigEditorError(`${HIDDEN_VALUE} can only stand for a secret the file already has.`);
  }
  return text;
}

/** Removes the file's secret values from GraphJin's messages. */
function scrubber(raw: string, edited?: string): (value: string) => string {
  const secrets = new Set<string>();
  for (const text of [raw, edited]) {
    if (!text) continue;
    try {
      for (const { value } of secretValues(parseDocument(text)).values()) {
        if (typeof value === "string" && value.length >= 6) secrets.add(value);
      }
    } catch {
      // An unparseable edit has no secrets to scrub beyond the file's.
    }
  }
  return (value) => [...secrets].reduce((out, secret) => out.split(secret).join(HIDDEN_VALUE), value);
}

export type EditableGraphjinConfig = { path: string; text: string; hash: string; hidden: number };

export async function readEditableGraphjinConfig(configFile: string): Promise<EditableGraphjinConfig> {
  const raw = await readFile(configFile, "utf8");
  return { path: configFile, hash: configHash(raw), ...maskGraphjinConfig(raw) };
}

export type GraphjinConfigCheck = {
  valid: boolean;
  scope: string | null;
  reloadMode: string | null;
  changes: unknown;
  findings: unknown;
  errors: string[];
};

type Connection = { endpoint: string; headers: Record<string, string> };

/** The org's GraphJin and an admin token for its config API. */
export async function graphjinAdminConnection(orgId: string, userId: string): Promise<Connection | null> {
  const [row] = await db()
    .select({ graphql_url: data_source.graphql_url })
    .from(data_source)
    .where(eq(data_source.org_id, orgId))
    .orderBy(desc(data_source.is_default), data_source.created_at)
    .limit(1);
  if (!row?.graphql_url) return null;
  return {
    endpoint: row.graphql_url,
    headers: { authorization: `Bearer ${mintGraphjinToken({ orgId, userId, role: "admin", ttlSeconds: 120 })}` },
  };
}

function parseJSON(value: string | undefined): unknown {
  if (!value) return null;
  try {
    return JSON.parse(value);
  } catch {
    return value;
  }
}

/** Has GraphJin check the edited config without changing anything. */
export async function checkEditedGraphjinConfig(input: {
  configFile: string;
  edited: string;
  baseHash: string;
  connection: Connection;
}): Promise<GraphjinConfigCheck> {
  const raw = await readFile(input.configFile, "utf8");
  if (configHash(raw) !== input.baseHash) {
    throw new GraphjinConfigEditorError("The config file changed since you opened it. Reload to see the current version.", 409);
  }
  const text = unmaskGraphjinConfig(raw, input.edited);
  const scrub = scrubber(raw, text);
  for (let attempt = 1; attempt <= 3; attempt++) {
    const name = `ConfigRevision_${randomUUID().replaceAll("-", "")}`;
    const current = await graphjinQuery<{ gj_config?: { catalog_revision?: string } | Array<{ catalog_revision?: string }> }>({
      baseUrl: input.connection.endpoint,
      configurationOnly: true,
      headers: input.connection.headers,
      query: `query ${name} { gj_config(id: "current") { catalog_revision } }`,
    });
    const revision = gjConfigRow(current.data?.gj_config)?.catalog_revision;
    if (!revision) throw new Error("GraphJin config revision unavailable");
    const request = graphjinInputWithJsonVariables({ mode: "validate", expected_catalog_revision: revision, config_yaml: text });
    const result = await graphjinQuery<{
      gj_config?: GjCheckRow | GjCheckRow[];
    }>({
      baseUrl: input.connection.endpoint,
      configurationOnly: true,
      longRunning: true,
      headers: input.connection.headers,
      query: `mutation${request.variableDefinitions} { gj_config(id: "current", update: ${request.literal}) { valid scope reload_mode change_summary_json findings_json errors_json } }`,
      variables: request.variables,
    });
    const messages = (result.errors ?? []).map((error) => error.message);
    if (messages.length > 0 && attempt < 3 && isCatalogRevisionConflict(messages) && currentRevisionFromConflict(messages)) continue;
    const row = gjConfigRow(result.data?.gj_config);
    const errors = [...messages, ...(Array.isArray(parseJSON(row?.errors_json)) ? (parseJSON(row?.errors_json) as unknown[]).map(String) : [])];
    return {
      valid: errors.length === 0 && row?.valid === true,
      scope: row?.scope ?? null,
      reloadMode: row?.reload_mode ?? null,
      changes: parseJSON(scrub(row?.change_summary_json ?? "")),
      findings: parseJSON(scrub(row?.findings_json ?? "")),
      errors: errors.map(scrub),
    };
  }
  throw new Error("GraphJin config revision did not stabilize");
}

type GjCheckRow = {
  valid?: boolean;
  scope?: string;
  reload_mode?: string;
  change_summary_json?: string;
  findings_json?: string;
  errors_json?: string;
};

/**
 * Applies the edited config live, then writes the file. Holds the config lock
 * throughout, so no other writer lands in between. When the file cannot be
 * written after GraphJin took the change, GraphJin is put back on the old
 * file, so the two never disagree.
 */
export async function applyEditedGraphjinConfig(input: {
  configFile: string;
  edited: string;
  baseHash: string;
  connection: Connection;
  apply?: (update: Record<string, unknown>) => Promise<unknown>;
}): Promise<{ hash: string }> {
  const apply =
    input.apply ??
    ((update: Record<string, unknown>) =>
      applyGraphjinConfigUpdate({ endpoint: input.connection.endpoint, headers: input.connection.headers, update }));
  const release = await acquireGraphjinConfigLock({ configFile: input.configFile });
  try {
    const raw = await readFile(input.configFile, "utf8");
    if (configHash(raw) !== input.baseHash) {
      throw new GraphjinConfigEditorError("The config file changed since you opened it. Reload to see the current version.", 409);
    }
    const text = unmaskGraphjinConfig(raw, input.edited);
    if (text === raw) return { hash: input.baseHash };
    const scrub = scrubber(raw, text);
    try {
      await apply({ config_yaml: text });
    } catch (error) {
      throw new GraphjinConfigEditorError(scrub(error instanceof Error ? error.message : String(error)), 422);
    }
    try {
      const { mode } = await stat(input.configFile);
      const temporary = `${input.configFile}.${process.pid}.${Date.now()}.tmp`;
      await writeFile(temporary, text, { mode: mode & 0o777 });
      try {
        await rename(temporary, input.configFile);
      } catch (error) {
        await rm(temporary, { force: true });
        throw error;
      }
    } catch (error) {
      await apply({ config_yaml: raw }).catch(() => {});
      throw error;
    }
    return { hash: configHash(text) };
  } finally {
    await release();
  }
}
