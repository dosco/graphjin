// Tool-output token instrumentation. Confirms where the /work loop actually
// spends tokens before we change any agent behavior. Every tool result (most
// importantly GraphJin query-tool JSON output) is measured here: raw
// bytes, an estimated token count, and — by running the lossless compactor in
// "dry-run" — the tokens we COULD save. Nothing the model sees changes; this is
// a measurement probe, gated entirely behind OPENNEKO_TOOL_OUTPUT_METRICS so
// it's zero-cost when off.
//
// Wired into run-chat-turn's wrappedEmit at the `tool_end` boundary, so it
// records agent tool-output behavior through one seam.

import { compactJson } from "./compact-json";

/** Live flag check — read at call time so the gate is togglable (and testable). */
export function metricsEnabled(): boolean {
  const v = process.env.OPENNEKO_TOOL_OUTPUT_METRICS;
  return !!v && v !== "0";
}

/** Load-time snapshot of {@link metricsEnabled} for callers that want a constant. */
export const TOOL_OUTPUT_METRICS_ENABLED = metricsEnabled();

export interface ToolResultMetric {
  tool: string;
  originalBytes: number;
  estTokens: number;
  /** True when the result parsed as JSON (the compressible case). */
  looksJson: boolean;
  /** Bytes after lossless compaction (== originalBytes when nothing helped). */
  compactedBytes: number;
  estTokensSaved: number;
  /** 0–100, rounded; share of estimated tokens the compactor would remove. */
  savingsPct: number;
  /** How the compactor would represent it: passthrough | minified | columnar. */
  format: ReturnType<typeof compactJson>["format"];
}

/**
 * Rough token estimate without pulling in a tokenizer dependency. ~4 chars per
 * token is the standard heuristic for English+JSON; good enough to rank sinks
 * and size savings. We measure characters (not UTF-8 bytes) here because token
 * count tracks code points, not byte length.
 */
export function estimateTokens(text: string): number {
  return Math.ceil(text.length / 4);
}

/** Coerce a `tool_end` result (string, object, or array of blocks) to text. */
export function toolResultToText(result: unknown): string {
  if (typeof result === "string") return result;
  if (result === null || result === undefined) return "";
  // Anthropic/ACP tool results are often `[{type:"text", text:"..."}]`.
  if (Array.isArray(result)) {
    const texts = result
      .map((b) =>
        b && typeof b === "object" && typeof (b as { text?: unknown }).text === "string"
          ? (b as { text: string }).text
          : "",
      )
      .filter(Boolean);
    if (texts.length > 0) return texts.join("");
  }
  try {
    return JSON.stringify(result);
  } catch {
    return String(result);
  }
}

/** Pure measurement — no I/O, no flag check. Returns null for empty output. */
export function measureToolResult(args: {
  tool: string;
  result: unknown;
}): ToolResultMetric | null {
  const text = toolResultToText(args.result);
  if (text.length === 0) return null;

  const compacted = compactJson(text);
  const estTokens = estimateTokens(text);
  const estCompactedTokens = estimateTokens(compacted.text);
  const estTokensSaved = Math.max(0, estTokens - estCompactedTokens);
  const savingsPct = estTokens > 0 ? Math.round((estTokensSaved / estTokens) * 100) : 0;

  return {
    tool: args.tool,
    originalBytes: compacted.originalBytes,
    estTokens,
    looksJson: compacted.format !== "passthrough",
    compactedBytes: compacted.compactedBytes,
    estTokensSaved,
    savingsPct,
    format: compacted.format,
  };
}

/**
 * Flag-gated side-effecting probe. Measures the result and logs one structured
 * line. NEVER throws — instrumentation must not be able to break a run.
 */
export function recordToolResult(args: { tool: string; result: unknown }): void {
  if (!metricsEnabled()) return;
  try {
    const metric = measureToolResult(args);
    if (!metric) return;
    console.info(`[tool-output-metrics] ${JSON.stringify(metric)}`);
  } catch {
    // swallow — a measurement failure must never affect the agent turn.
  }
}

export interface ToolOutputRecorder {
  /** Feed every agent event; tool_start/tool_end are handled, the rest ignored. */
  observe(event: unknown): void;
}

/**
 * Stateful observer over an agent event stream. A tool_end carries only an id,
 * so we remember each tool_start's name and correlate it back when the result
 * lands, then record the output's size metrics. Extracted from run-chat-turn's
 * wrappedEmit so the correlation is unit-testable without standing up a full
 * run; `record` is injectable for tests (defaults to {@link recordToolResult}).
 */
export function createToolOutputRecorder(
  record: (args: { tool: string; result: unknown }) => void = recordToolResult,
): ToolOutputRecorder {
  const namesById = new Map<string, string>();
  return {
    observe(event: unknown): void {
      if (!event || typeof event !== "object") return;
      const e = event as { type?: unknown; id?: unknown; name?: unknown; result?: unknown };
      if (e.type === "tool_start" && typeof e.id === "string") {
        namesById.set(e.id, typeof e.name === "string" ? e.name : "unknown");
      } else if (e.type === "tool_end" && e.result !== undefined) {
        const id = typeof e.id === "string" ? e.id : "";
        record({ tool: namesById.get(id) ?? "unknown", result: e.result });
        if (id) namesById.delete(id);
      }
    },
  };
}
