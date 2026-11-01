// Parses agent-emitted `neko_memory` fences into memory operations.
// Mirrors the neko_a2ui surface fence pattern in surface.ts so an agent
// that answers in prose instead of calling the MCP tool can still write
// memories — the agent emits the fence, the runtime extracts and persists.
//
// Fence shape:
//   ```neko_memory
//   [
//     { "save": { "text": "...", "scope": "global",
//                 "kind": "business_rule", "pinned": true } }
//   ]
//   ```
//
// Multiple `{ "save": ... }` items in the array are allowed. Anything
// not matching the spec is silently dropped (the agent's prose around
// the fence is preserved either way).

const NEKO_MEMORY_FENCE_RE = /```neko_memory\s*([\s\S]*?)```/gi;

export type MemorySaveOp = {
  kind: "save";
  text: string;
  scope?: "global" | "thread";
  memoryKind?: string;
  pinned?: boolean;
};

export type MemoryFenceExtraction = {
  // Original text minus all parsed fences. This is what the user sees.
  text: string;
  ops: MemorySaveOp[];
};

function isStr(v: unknown): v is string {
  return typeof v === "string";
}

function parseSaveItem(item: unknown): MemorySaveOp | null {
  if (!item || typeof item !== "object") return null;
  const o = item as Record<string, unknown>;
  const save = o.save;
  if (!save || typeof save !== "object") return null;
  const s = save as Record<string, unknown>;
  if (!isStr(s.text) || s.text.trim().length < 3) return null;
  const scope =
    s.scope === "thread" || s.scope === "global"
      ? (s.scope as "thread" | "global")
      : undefined;
  return {
    kind: "save",
    text: s.text.trim(),
    scope,
    memoryKind: isStr(s.kind) ? s.kind : undefined,
    pinned: typeof s.pinned === "boolean" ? s.pinned : undefined,
  };
}

export function extractMemoryFences(raw: string): MemoryFenceExtraction {
  const ops: MemorySaveOp[] = [];
  let stripped = raw;
  // Capture all fences first (regex with /g iterator), then strip.
  const matches = Array.from(raw.matchAll(NEKO_MEMORY_FENCE_RE));
  for (const m of matches) {
    const body = m[1].trim();
    try {
      const parsed = JSON.parse(body);
      if (Array.isArray(parsed)) {
        for (const item of parsed) {
          const op = parseSaveItem(item);
          if (op) ops.push(op);
        }
      } else {
        const op = parseSaveItem(parsed);
        if (op) ops.push(op);
      }
    } catch {
      // Body wasn't valid JSON — skip silently. The fence still gets
      // stripped from the output below to avoid leaking malformed
      // attempts to the user.
    }
    stripped = stripped.replace(m[0], "");
  }
  return { text: stripped.trim(), ops };
}
