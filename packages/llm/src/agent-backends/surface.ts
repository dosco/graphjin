import type { AgentSurfaceMessage } from "../agent-backend";
import {
  coerceGeneratedSurfaceMessages,
  coerceReadableSurfaceMessages,
  validateRenderCardsInput,
} from "../work/a2ui-contract";

const NEKO_A2UI_FENCE_RE = /```neko_a2ui\s*([\s\S]*?)```/i;
const NEKO_CARDS_FENCE_RE = /```neko_cards\s*([\s\S]*?)\n?```/gi;

/**
 * Cards written into the final answer as render_cards arguments, for a stage
 * that writes the answer without tool access (the Ax responder).
 */
export function extractCardFences(raw: string, opts: { rich?: boolean } = {}): {
  text: string;
  messages: AgentSurfaceMessage[];
  issues: string[];
} {
  const messages: AgentSurfaceMessage[] = [];
  const issues: string[] = [];
  const text = raw.replace(NEKO_CARDS_FENCE_RE, (_, body: string) => {
    let args: unknown;
    try {
      args = JSON.parse(body.trim());
    } catch {
      issues.push("neko_cards fence is not JSON");
      return "";
    }
    const validated = validateRenderCardsInput(args, undefined, opts);
    if (validated.success) messages.push(...validated.messages);
    else issues.push(...validated.issues.map((issue) => issue.message));
    return "";
  });
  return { text: messages.length || issues.length ? text.replace(/\n{3,}/g, "\n\n").trim() : raw, messages, issues };
}

const JSX_TAG_RE = /<\/?[A-Z][A-Za-z0-9]*\b[^>]*>/g;

// Validate an already-parsed messages value (e.g. a render_cards tool call's
// `messages` argument) into surface messages. Returns [] when nothing valid.
export function coerceSurfaceMessages(value: unknown): AgentSurfaceMessage[] {
  return coerceReadableSurfaceMessages(value);
}

/** New render_cards calls emit v1.0; v0.9 remains a reader-only format. */
export { coerceGeneratedSurfaceMessages };

export function extractSurfaceMessages(raw: string): {
  text: string;
  messages: AgentSurfaceMessage[];
} {
  const match = raw.match(NEKO_A2UI_FENCE_RE);
  if (!match) return { text: raw.trim(), messages: [] };
  const outsideText = raw.replace(match[0], "").trim();
  const body = match[1].trim();
  try {
    const parsed = JSON.parse(body);
    if (Array.isArray(parsed)) {
      const messages = coerceReadableSurfaceMessages(parsed);
      return { text: outsideText, messages };
    }
    // Non-array JSON falls through to synthetic markdown surface.
  } catch {
    // body wasn't JSON — fall through to synthetic markdown fallback
  }
  const proseFallback = body.replace(JSX_TAG_RE, "").trim();
  if (!proseFallback) return { text: outsideText, messages: [] };
  return {
    text: outsideText,
    messages: synthesizeMarkdownSurface(proseFallback),
  };
}

function synthesizeMarkdownSurface(text: string): AgentSurfaceMessage[] {
  const surfaceId = "fallback";
  return [
    {
      version: "v1.0",
      createSurface: {
        surfaceId,
        catalogId: "urn:openneko:catalog:work:v2",
        components: [{ id: "root", component: "Markdown", text }],
      },
    } as AgentSurfaceMessage,
  ];
}
