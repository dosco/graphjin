// Self-contained stdin->stdout compactor the graphjin guard wrapper pipes
// tool output through, so the model receives compact columnar tables instead
// of row-repeated JSON. Pure JS / zero imports on purpose: it runs under plain
// `node` inside the agent sandbox, where tsx/module resolution isn't
// guaranteed. The logic mirrors compact-json.ts; compact-cli.test.ts pins the
// two together so they can't drift. On ANY error it echoes input verbatim —
// it must never corrupt or drop a tool result.

const COLUMNAR_MARKER = "__neko_cols__";
const MIN_ROWS = 3;

const isPrimitive = (v) =>
  v === null || typeof v === "string" || typeof v === "number" || typeof v === "boolean";
const isPlainObject = (v) => typeof v === "object" && v !== null && !Array.isArray(v);

function toColumnar(arr) {
  if (arr.length < MIN_ROWS) return null;
  if (!arr.every(isPlainObject)) return null;
  const cols = Object.keys(arr[0]);
  if (cols.length === 0) return null;
  const colSet = new Set(cols);
  for (const row of arr) {
    const keys = Object.keys(row);
    if (keys.length !== cols.length) return null;
    for (const k of keys) {
      if (!colSet.has(k)) return null;
      if (!isPrimitive(row[k])) return null;
    }
  }
  return { [COLUMNAR_MARKER]: { cols, rows: arr.map((row) => cols.map((c) => row[c])) } };
}

function compactValue(value) {
  if (Array.isArray(value)) {
    const columnar = toColumnar(value);
    if (columnar) return columnar;
    return value.map(compactValue);
  }
  if (isPlainObject(value)) {
    const out = {};
    for (const [k, v] of Object.entries(value)) out[k] = compactValue(v);
    return out;
  }
  return value;
}

function compact(raw) {
  let parsed;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return raw;
  }
  const minified = JSON.stringify(compactValue(parsed));
  if (minified === undefined) return raw;
  return Buffer.byteLength(minified, "utf8") < Buffer.byteLength(raw, "utf8") ? minified : raw;
}

const chunks = [];
process.stdin.on("data", (c) => chunks.push(c));
process.stdin.on("end", () => {
  const raw = Buffer.concat(chunks).toString("utf8");
  let out = raw;
  try {
    out = compact(raw);
  } catch {
    out = raw;
  }
  process.stdout.write(out);
});
process.stdin.on("error", () => process.stdout.write(Buffer.concat(chunks).toString("utf8")));
