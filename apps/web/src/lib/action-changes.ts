export type ActionChange = {
  key: string;
  label: string;
  before: string | null;
  after: string;
};

export type ActionChangeSet = {
  entity: string | null;
  changes: ActionChange[];
};

export type DiffToken = { text: string; kind: "same" | "removed" | "added" };

const ACRONYMS = new Set(["sku", "id", "url", "api", "sla", "msrp", "vat", "ean", "upc"]);
const MAX_DIFF_TOKENS = 600;

function record(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function normalize(value: string): string {
  return value.replace(/\r\n?/g, "\n").trim();
}

function asText(value: unknown): string | null {
  if (value === null || value === undefined) return null;
  if (typeof value === "string") return normalize(value);
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  if (Array.isArray(value) && value.every((v) => typeof v !== "object" || v === null)) {
    return value.map(String).join(", ");
  }
  return null;
}

export function humanizeKey(key: string): string {
  const words = key
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .split(/[_\s.-]+/)
    .filter(Boolean)
    .map((word) => word.toLowerCase());
  if (words.length === 0) return key;
  return words
    .map((word, index) =>
      ACRONYMS.has(word)
        ? word.toUpperCase()
        : index === 0
          ? word.charAt(0).toUpperCase() + word.slice(1)
          : word,
    )
    .join(" ");
}

function isAttributeList(value: unknown): value is Array<{ attribute_code: string; value: unknown }> {
  return (
    Array.isArray(value) &&
    value.length > 0 &&
    value.every((item) => typeof record(item)?.attribute_code === "string")
  );
}

// Flattens one entity into leaf fields. Commerce attribute lists
// ([{ attribute_code, value }]) become named fields.
export function flattenFields(value: unknown, prefix = "", out = new Map<string, string>()): Map<string, string> {
  const obj = record(value);
  if (!obj) return out;
  for (const [key, child] of Object.entries(obj)) {
    if (isAttributeList(child)) {
      for (const item of child) {
        const text = asText(item.value);
        if (text !== null) out.set(item.attribute_code, text);
      }
      continue;
    }
    const text = asText(child);
    if (text !== null) {
      out.set(prefix ? `${prefix}.${key}` : key, text);
    } else if (record(child)) {
      flattenFields(child, prefix ? `${prefix}.${key}` : key, out);
    }
  }
  return out;
}

function labelFor(key: string): string {
  const leaf = key.split(".").pop() ?? key;
  return humanizeKey(leaf);
}

function changesFromPreview(rows: unknown[]): ActionChangeSet[] {
  return rows.flatMap((row) => {
    const r = record(row);
    if (!r || !record(r.after)) return [];
    const before = flattenFields(r.before);
    const after = flattenFields(r.after);
    const changes: ActionChange[] = [];
    for (const [key, next] of after) {
      const prev = before.get(key) ?? null;
      if (prev !== null && prev === next) continue;
      changes.push({ key, label: labelFor(key), before: prev, after: next });
    }
    return [{ entity: typeof r.entity_ref === "string" ? r.entity_ref : null, changes }];
  });
}

function changesFromRows(rows: unknown[]): ActionChangeSet[] {
  return rows.flatMap((row) => {
    const r = record(row);
    const body = record(r?.body) ?? r;
    if (!body) return [];
    const fields = flattenFields(body);
    const entity = typeof r?.entity_ref === "string" ? r.entity_ref : null;
    const changes = [...fields]
      .filter(([key, value]) => !(entity && value === entity && /(^|\.)sku$|(^|\.)id$/i.test(key)))
      .map(([key, value]) => ({ key, label: labelFor(key), before: null, after: value }));
    return [{ entity, changes }];
  });
}

// Reads a proposed action payload and returns the field-level changes a
// reviewer needs. Returns null when the payload has no recognizable rows.
export function describeActionChanges(payload: unknown): ActionChangeSet[] | null {
  const p = record(payload);
  if (!p) return null;
  const previewRows = record(p.preview)?.rows;
  if (Array.isArray(previewRows) && previewRows.length > 0) {
    const sets = changesFromPreview(previewRows).filter((s) => s.changes.length > 0);
    if (sets.length > 0) return sets;
  }
  if (Array.isArray(p.rows) && p.rows.length > 0) {
    const sets = changesFromRows(p.rows).filter((s) => s.changes.length > 0);
    if (sets.length > 0) return sets;
  }
  return null;
}

// Word-level diff by longest common subsequence. Whitespace stays attached
// to the following word so the output reads as prose.
export function diffWords(before: string, after: string): DiffToken[] {
  const a = before.split(/(?=\s)/);
  const b = after.split(/(?=\s)/);
  if (a.length * b.length > MAX_DIFF_TOKENS * MAX_DIFF_TOKENS) {
    return [
      { text: before, kind: "removed" },
      { text: after, kind: "added" },
    ];
  }
  const dp: number[][] = Array.from({ length: a.length + 1 }, () => new Array<number>(b.length + 1).fill(0));
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
    }
  }
  const out: DiffToken[] = [];
  const push = (text: string, kind: DiffToken["kind"]) => {
    const last = out[out.length - 1];
    if (last && last.kind === kind) last.text += text;
    else out.push({ text, kind });
  };
  let i = 0;
  let j = 0;
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) {
      push(a[i], "same");
      i++;
      j++;
    } else if (dp[i + 1][j] >= dp[i][j + 1]) {
      push(a[i++], "removed");
    } else {
      push(b[j++], "added");
    }
  }
  while (i < a.length) push(a[i++], "removed");
  while (j < b.length) push(b[j++], "added");
  return out;
}

const CONTEXT_WORDS = 8;

// Shortens unchanged runs between edits so a reviewer sees each change with
// a few words of context on each side.
export function trimDiffContext(tokens: DiffToken[], context = CONTEXT_WORDS): DiffToken[] {
  return tokens.map((token, index) => {
    if (token.kind !== "same") return token;
    const words = token.text.split(/(?=\s)/);
    const first = index === 0;
    const last = index === tokens.length - 1;
    const keep = first || last ? context : context * 2;
    if (words.length <= keep + 2) return token;
    if (first) return { ...token, text: `…${words.slice(-context).join("")}` };
    if (last) return { ...token, text: `${words.slice(0, context).join("")} …` };
    return {
      ...token,
      text: `${words.slice(0, context).join("")} … ${words.slice(-context).join("").trimStart()}`,
    };
  });
}
