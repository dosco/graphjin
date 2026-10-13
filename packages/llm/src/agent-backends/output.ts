// Output handling shared by every backend: hidden fences, surface text and JSON answers.

const FENCE_RE = /^\s*```(?:json)?\s*([\s\S]*?)\s*```\s*$/;

const FENCE_CLOSE = "\n```";

// Fences the runtime parses out-of-band: a2ui drives surface cards, the rest
// are builder fences. Hide them from the chat stream until the closing ```.
const HIDDEN_FENCE_OPENERS = [
  "```neko_a2ui",
  "```neko_cards",
  "```neko_workflow_save",
  "```neko_workflow_output",
  "```neko_action_request",
  "```neko_rule_save",
] as const;

export function extractMarkdownText(messages: Array<Record<string, unknown>>): string {
  const out: string[] = [];
  const visit = (node: unknown): void => {
    if (!node || typeof node !== "object") return;
    if (Array.isArray(node)) {
      for (const item of node) visit(item);
      return;
    }
    const obj = node as Record<string, unknown>;
    if (obj.component === "Markdown" && typeof obj.text === "string") {
      out.push(obj.text);
    }
    for (const value of Object.values(obj)) visit(value);
  };
  visit(messages);
  return out.join("\n\n").trim();
}

function findNextOpener(
  raw: string,
  from: number,
): { index: number; opener: string } | null {
  let best: { index: number; opener: string } | null = null;
  for (const opener of HIDDEN_FENCE_OPENERS) {
    const idx = raw.indexOf(opener, from);
    if (idx === -1) continue;
    if (!best || idx < best.index) best = { index: idx, opener };
  }
  return best;
}

export function outsideFenceText(raw: string): string {
  let out = "";
  let i = 0;
  while (i < raw.length) {
    const next = findNextOpener(raw, i);
    if (!next) {
      // No full opener visible. Hold back any tail of `raw` that matches a
      // prefix of any opener — it might complete in a later streamed chunk.
      // Without this, a partial opener like "```neko_a2" or "```neko_wo"
      // leaks into the message event stream as an empty code block.
      const tail = raw.slice(i);
      let holdBack = 0;
      for (const opener of HIDDEN_FENCE_OPENERS) {
        const maxK = Math.min(tail.length, opener.length - 1);
        for (let k = maxK; k > holdBack; k--) {
          if (tail.slice(-k) === opener.slice(0, k)) {
            holdBack = k;
            break;
          }
        }
      }
      out += tail.slice(0, tail.length - holdBack);
      break;
    }
    out += raw.slice(i, next.index);
    const close = raw.indexOf(FENCE_CLOSE, next.index + next.opener.length);
    if (close === -1) break;
    i = close + FENCE_CLOSE.length;
  }
  return out;
}

export function parseJsonFromOutput(raw: string): unknown {
  const trimmed = raw.trim();
  const fenced = trimmed.match(FENCE_RE);
  const candidate = (fenced?.[1] ?? trimmed).trim();
  try {
    return JSON.parse(candidate);
  } catch {
    const first = candidate.indexOf("{");
    const last = candidate.lastIndexOf("}");
    if (first === -1 || last === -1 || last < first) {
      throw new Error(
        `hermes output not parseable as JSON (no object braces found): ${candidate.slice(0, 200)}`,
      );
    }
    return JSON.parse(candidate.slice(first, last + 1));
  }
}
