// Lossless structural compaction of JSON tool output (Headroom "SmartCrusher"
// idea, in-process and information-preserving). The dominant token sink in the
// /work loop is GraphJin results returned by the native broker tool, and the
// raw GraphQL JSON stdout flows straight into model context, where an array of
// N rows repeats every column key N times. This module rewrites a uniform array
// of flat objects into a columnar envelope (keys stated once, values as rows),
// which is reversible by construction — see `expandJson`.
//
// Two guarantees:
//   1. Lossless. `expandJson(parse(compactJson(raw).text))` deep-equals
//      `parse(raw)` for every input we transform. Anything we can't transform
//      safely (non-JSON, ragged object shapes, nested structures) is left as
//      its minified self, which is also lossless.
//   2. Never regresses. If the compacted form isn't smaller than the input, we
//      return the original text untouched.
//
// MEASUREMENT-FIRST: today this module is only used to *measure* potential
// savings (see metrics.ts). It is NOT yet wired to rewrite what the model sees —
// the columnar envelope needs a one-line legend before it's fed to a model, and
// we want real numbers from `OPENNEKO_TOOL_OUTPUT_METRICS` before changing
// agent behavior on financial data.

/** Marker key identifying a columnar envelope produced by `compactJson`. */
export const COLUMNAR_MARKER = "__neko_cols__" as const;

export type CompactionFormat = "passthrough" | "minified" | "columnar";

export interface CompactionResult {
  /** Compacted text, or the original input if compaction didn't help. */
  text: string;
  originalBytes: number;
  compactedBytes: number;
  /** True when the returned text differs structurally from a plain re-serialize. */
  transformed: boolean;
  format: CompactionFormat;
}

/** A columnar envelope: an array of uniform flat objects, keys stated once. */
interface ColumnarEnvelope {
  [COLUMNAR_MARKER]: {
    cols: string[];
    rows: Array<Array<string | number | boolean | null>>;
  };
}

const utf8Bytes = (s: string): number => Buffer.byteLength(s, "utf8");

/** Minimum array length worth tabularizing — below this the envelope overhead wins. */
const MIN_ROWS = 3;

function isPrimitive(v: unknown): v is string | number | boolean | null {
  return (
    v === null ||
    typeof v === "string" ||
    typeof v === "number" ||
    typeof v === "boolean"
  );
}

function isPlainObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/**
 * If `arr` is an array of >= MIN_ROWS plain objects that all share the EXACT
 * same key set and whose every value is a primitive, return its columnar
 * envelope. Otherwise return null. The exact-key-set requirement is what keeps
 * the transform unambiguously reversible: no "missing key vs null value"
 * guessing on expand.
 */
function toColumnar(arr: unknown[]): ColumnarEnvelope | null {
  if (arr.length < MIN_ROWS) return null;
  if (!arr.every(isPlainObject)) return null;

  const first = arr[0] as Record<string, unknown>;
  const cols = Object.keys(first);
  if (cols.length === 0) return null;
  const colSet = new Set(cols);

  for (const row of arr as Array<Record<string, unknown>>) {
    const keys = Object.keys(row);
    if (keys.length !== cols.length) return null;
    for (const k of keys) {
      if (!colSet.has(k)) return null;
      if (!isPrimitive(row[k])) return null;
    }
  }

  const rows = (arr as Array<Record<string, unknown>>).map((row) =>
    cols.map((c) => row[c] as string | number | boolean | null),
  );
  return { [COLUMNAR_MARKER]: { cols, rows } };
}

/** Recursively compact: tabularize uniform arrays, recurse into the rest. */
function compactValue(value: unknown): unknown {
  if (Array.isArray(value)) {
    const columnar = toColumnar(value);
    if (columnar) return columnar;
    return value.map(compactValue);
  }
  if (isPlainObject(value)) {
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(value)) out[k] = compactValue(v);
    return out;
  }
  return value;
}

/**
 * Compact a tool-output string. Safe on ANY input: non-JSON returns
 * passthrough; valid JSON is minified and (where uniform arrays appear)
 * columnarized; if that isn't smaller, the original is returned verbatim.
 */
export function compactJson(raw: string): CompactionResult {
  const originalBytes = utf8Bytes(raw);
  const base: CompactionResult = {
    text: raw,
    originalBytes,
    compactedBytes: originalBytes,
    transformed: false,
    format: "passthrough",
  };

  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return base; // not JSON (plain text, logs, partial output) — leave as-is.
  }

  const compactedValue = compactValue(parsed);
  const minified = JSON.stringify(compactedValue);
  if (minified === undefined) return base; // e.g. raw was a bare `undefined`-ish edge

  const compactedBytes = utf8Bytes(minified);
  if (compactedBytes >= originalBytes) return base; // never regress

  // Did columnarization actually fire, or did we only strip whitespace?
  const transformed = minified.includes(`"${COLUMNAR_MARKER}"`);
  return {
    text: minified,
    originalBytes,
    compactedBytes,
    transformed: true,
    format: transformed ? "columnar" : "minified",
  };
}

/** True if `value` is a columnar envelope produced by `compactJson`. */
function isColumnarEnvelope(value: unknown): value is ColumnarEnvelope {
  if (!isPlainObject(value)) return false;
  const keys = Object.keys(value);
  if (keys.length !== 1 || keys[0] !== COLUMNAR_MARKER) return false;
  const inner = (value as Record<string, unknown>)[COLUMNAR_MARKER];
  return (
    isPlainObject(inner) &&
    Array.isArray((inner as Record<string, unknown>).cols) &&
    Array.isArray((inner as Record<string, unknown>).rows)
  );
}

/**
 * Inverse of the columnar transform. Walks a parsed value and expands every
 * columnar envelope back into its array of objects. This is what proves the
 * transform is lossless (round-trip tests) and is the seed of a future
 * `headroom_retrieve`-style reconstruction if the model ever needs the
 * original shape back.
 */
export function expandJson(value: unknown): unknown {
  if (isColumnarEnvelope(value)) {
    const { cols, rows } = value[COLUMNAR_MARKER];
    return rows.map((row) => {
      const obj: Record<string, unknown> = {};
      cols.forEach((c, i) => {
        obj[c] = row[i];
      });
      return obj;
    });
  }
  if (Array.isArray(value)) return value.map(expandJson);
  if (isPlainObject(value)) {
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(value)) out[k] = expandJson(v);
    return out;
  }
  return value;
}
