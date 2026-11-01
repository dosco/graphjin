// The admin GraphJin config file editor. Admins edit the whole config file
// as YAML, with secrets shown as <hidden>; a save puts the hidden values back,
// has GraphJin check and apply the edited text live, and then writes the
// file, so the running GraphJin and the file never disagree and nothing
// restarts.
//
// The edited text goes to GraphJin over openneko serve's local socket, not
// GraphJin's API: GraphJin refuses a whole config from any token, so only
// web and the worker, which can reach the socket, can replace one. Protocol:
// apps/ax-harness/serve/config.go.

import { createHash } from "node:crypto";
import { readFile, rename, rm, stat, writeFile } from "node:fs/promises";
import { createConnection } from "node:net";
import { createInterface } from "node:readline";
import { isMap, isScalar, isSeq, parseDocument, Scalar, type Document } from "yaml";
import { acquireGraphjinConfigLock } from "./persist-source-config";

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

/** GraphJin's answer to a config check or apply (its gj_config update result). */
export type GraphjinConfigEditResult = {
  success?: boolean;
  valid?: boolean;
  applied?: boolean;
  message?: string;
  errors?: string[];
  scope?: string;
  reload_mode?: string;
  change_summary_json?: string;
  findings_json?: string;
};

/** Checks text, or with apply applies it, on the GraphJin openneko serve hosts. */
export type GraphjinConfigEdit = (text: string, apply: boolean) => Promise<GraphjinConfigEditResult>;

/** Sends edits to the GraphJin that openneko serve at socket hosts. */
export function serveGraphjinConfigEdit(socket: string, timeoutMs = 120_000): GraphjinConfigEdit {
  return (text, apply) =>
    new Promise((resolve, reject) => {
      const conn = createConnection(socket);
      let settled = false;
      const settle = (error: Error | null, value?: GraphjinConfigEditResult) => {
        if (settled) return;
        settled = true;
        conn.destroy();
        if (error) reject(error);
        else resolve(value!);
      };
      const fail = (error: Error) => settle(error);
      conn.setTimeout(timeoutMs, () => fail(new Error("openneko serve did not answer the config edit in time")));
      conn.on("error", (error) => fail(new Error(`openneko serve unavailable: ${error.message}`)));
      let result: GraphjinConfigEditResult | null = null;
      const lines = createInterface({ input: conn });
      // readline repeats the socket's errors, which the handler above reports.
      lines.on("error", () => {});
      lines.on("line", (line) => {
        if (!line.trim()) return;
        let frame: { type?: string; result?: GraphjinConfigEditResult; code?: number; error?: string };
        try {
          frame = JSON.parse(line);
        } catch {
          fail(new Error("openneko serve sent an unreadable answer"));
          return;
        }
        if (frame.type === "result") result = frame.result ?? null;
        if (frame.type !== "exit") return;
        if (frame.code || !result) fail(new Error(frame.error || "openneko serve could not edit the config"));
        else settle(null, result);
      });
      lines.on("close", () => fail(new Error("openneko serve hung up during the config edit")));
      conn.write(`${JSON.stringify({ type: "config", config: { text, apply } })}\n`);
    });
}

function parseJSON(value: string | undefined): unknown {
  if (!value) return null;
  try {
    return JSON.parse(value);
  } catch {
    return value;
  }
}

/** GraphJin's errors: those it listed, else its message when it failed. */
function editErrors(result: GraphjinConfigEditResult): string[] {
  const errors = (result.errors ?? []).map(String).filter(Boolean);
  if (errors.length === 0 && result.success === false) errors.push(result.message || "GraphJin refused the config");
  return errors;
}

/** Has GraphJin check the edited config without changing anything. */
export async function checkEditedGraphjinConfig(input: {
  configFile: string;
  edited: string;
  baseHash: string;
  edit: GraphjinConfigEdit;
}): Promise<GraphjinConfigCheck> {
  const raw = await readFile(input.configFile, "utf8");
  if (configHash(raw) !== input.baseHash) {
    throw new GraphjinConfigEditorError("The config file changed since you opened it. Reload to see the current version.", 409);
  }
  const text = unmaskGraphjinConfig(raw, input.edited);
  const scrub = scrubber(raw, text);
  const result = await input.edit(text, false);
  const errors = editErrors(result);
  return {
    valid: errors.length === 0 && result.valid === true,
    scope: result.scope ?? null,
    reloadMode: result.reload_mode ?? null,
    changes: parseJSON(scrub(result.change_summary_json ?? "")),
    findings: parseJSON(scrub(result.findings_json ?? "")),
    errors: errors.map(scrub),
  };
}

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
  edit: GraphjinConfigEdit;
}): Promise<{ hash: string }> {
  const apply = async (text: string) => {
    const result = await input.edit(text, true);
    const errors = editErrors(result);
    if (errors.length > 0 || result.valid === false) throw new Error(errors.join("; ") || "GraphJin refused the config");
  };
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
      await apply(text);
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
      await apply(raw).catch(() => {});
      throw error;
    }
    return { hash: configHash(text) };
  } finally {
    await release();
  }
}
