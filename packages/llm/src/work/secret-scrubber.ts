// Env-value scrubber for agent output. Built from the deployment secrets
// store (@neko/secrets). Applied at every sink that persists or
// displays agent output — tool_end output, message content,
// action_execution.error, fence emitters — so accidentally-leaked
// secrets get replaced with [REDACTED] before they hit
// work_memory, run replays, or the Briefing.
//
// Defense-in-depth, NOT the first line of defence. The first line is
// the agent sandbox + its network egress policy + the secrets file that
// the agent never sees. The scrubber catches
// verbatim leaks (the 95% case: `env | grep TOKEN`, a tool's stderr
// echoing the value on auth failure, agent paraphrasing the value into
// a message). It does NOT catch transformations (base64, URL-encode,
// partial echo).

export const REDACTED_PLACEHOLDER = "[REDACTED]";

/** Values shorter than this are not scrubbed to avoid false positives. */
const MIN_REDACTED_LENGTH = 8;

export interface Scrubber {
  (text: string): string;
}

const NOOP_SCRUBBER: Scrubber = (s) => s;

/**
 * Builds a scrubber that replaces every occurrence of any value in
 * `values` with `[REDACTED]`. Values shorter than 8 chars are
 * discarded (e.g. a plugin storing `KEY=x` would otherwise match `x`
 * everywhere — the scrubber would be noise). Duplicates are removed.
 * Longest-first ordering ensures longer values mask shorter
 * substrings (matters when one secret is a substring of another).
 */
export function createScrubber(values: readonly string[]): Scrubber {
  const usable = [...new Set(values)].filter(
    (v) => typeof v === "string" && v.length >= MIN_REDACTED_LENGTH,
  );
  if (usable.length === 0) return NOOP_SCRUBBER;
  usable.sort((a, b) => b.length - a.length);
  const rx = new RegExp(usable.map(escapeRegex).join("|"), "g");
  return (s) => {
    if (typeof s !== "string" || s.length === 0) return s;
    return s.replace(rx, REDACTED_PLACEHOLDER);
  };
}

/**
 * Recursively scrubs string leaves inside a JSON-like value. Returns a
 * deep clone with scrubbed strings; non-string leaves are passed
 * through unchanged. Used at fence-emitter sinks where the agent's
 * tool result is an object whose nested strings may contain leaked
 * secrets.
 */
export function scrubJson<T>(scrubber: Scrubber, value: T): T {
  if (scrubber === NOOP_SCRUBBER) return value;
  return scrubInner(scrubber, value) as T;
}

function scrubInner(scrubber: Scrubber, value: unknown): unknown {
  if (typeof value === "string") return scrubber(value);
  if (value === null || typeof value !== "object") return value;
  if (Array.isArray(value)) return value.map((v) => scrubInner(scrubber, v));
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
    out[k] = scrubInner(scrubber, v);
  }
  return out;
}

/**
 * POSIX regex metachar escape. Kept exported for tests; the scrubber
 * uses it internally to build a safe alternation regex from raw
 * secret values that may contain `.`, `?`, `[`, `(`, `$`, etc.
 */
export function escapeRegex(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

export function isNoopScrubber(scrubber: Scrubber): boolean {
  return scrubber === NOOP_SCRUBBER;
}

/**
 * Apply a scrubber to every operator-visible string field of an
 * AgentEvent. Keys (event types, tool ids, kinds) pass through
 * untouched — only operator-visible content gets scrubbed.
 *
 * The function takes `unknown` because the AgentEvent type lives in
 * @neko/llm and importing it here would create a cycle (work/ already
 * depends on agent-backend's types indirectly). We accept the loose
 * typing as the cost of keeping this scrubber module a pure leaf.
 */
export function scrubAgentEvent<T>(scrubber: Scrubber, event: T): T {
  if (isNoopScrubber(scrubber)) return event;
  if (typeof event !== "object" || event === null) return event;
  const e = event as Record<string, unknown>;
  switch (e.type) {
    case "message":
    case "interim":
      return {
        ...e,
        content: typeof e.content === "string" ? scrubber(e.content) : e.content,
      } as T;
    case "tool_start":
      return {
        ...e,
        input: scrubJson(scrubber, e.input),
      } as T;
    case "tool_delta":
      return {
        ...e,
        delta: scrubJson(scrubber, e.delta),
      } as T;
    case "tool_end":
      return {
        ...e,
        result: scrubJson(scrubber, e.result),
        error: typeof e.error === "string" ? scrubber(e.error) : e.error,
      } as T;
    case "surface":
      return {
        ...e,
        messages: scrubJson(scrubber, e.messages),
      } as T;
    case "artifact":
      return {
        ...e,
        artifact: scrubJson(scrubber, e.artifact),
      } as T;
    case "status":
    case "progress":
    case "error":
      return {
        ...e,
        ...(typeof e.message === "string"
          ? { message: scrubber(e.message) }
          : {}),
        ...(typeof e.content === "string"
          ? { content: scrubber(e.content) }
          : {}),
      } as T;
    case "done":
      return {
        ...e,
        result: scrubJson(scrubber, e.result),
      } as T;
    case "needs_input":
      return {
        ...e,
        question:
          typeof e.question === "string" ? scrubber(e.question) : e.question,
        options: Array.isArray(e.options)
          ? e.options.map((o) => (typeof o === "string" ? scrubber(o) : o))
          : e.options,
        questions: Array.isArray(e.questions)
          ? scrubJson(scrubber, e.questions)
          : e.questions,
        reason:
          typeof e.reason === "string" ? scrubber(e.reason) : e.reason,
      } as T;
    case "output_emit":
      // Just identifiers + kind names, no user-supplied content.
      return event;
    case "action_request_emit":
      // Identifiers + kind are fine, but intent + summary are
      // agent-authored prose that could echo a leaked secret —
      // scrub those defensively.
      return {
        ...e,
        ...(typeof e.intent === "string"
          ? { intent: scrubber(e.intent) }
          : {}),
        ...(typeof e.summary === "string"
          ? { summary: scrubber(e.summary) }
          : {}),
      } as T;
    case "action_request_result":
      // outcome.result / error / rejection_reason all could carry
      // leaked values. Run the JSON scrubber over the whole object.
      return scrubJson(scrubber, event) as T;
    default:
      // Unknown event type — scrub every string field defensively.
      return scrubJson(scrubber, event) as T;
  }
}
