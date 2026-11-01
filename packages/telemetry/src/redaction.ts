import type { AttributeValue } from "./types";

const FORBIDDEN_ATTRIBUTE_KEY =
  /(?:prompt|response|completion|message|query|sql|graphql|tool[._-]?(?:input|output|result|argument)|content|body|secret|password|api[._-]?key|authorization|cookie|token(?![._-]?(?:count|usage)))/iu;

// Standard GenAI metadata whose key contains a generally content-bearing word
// but whose value is only a model identifier.
const SAFE_ATTRIBUTE_KEYS = new Set(["gen_ai.response.model"]);

const SECRET_VALUE_PATTERNS = [
  /\bsk-[A-Za-z0-9_-]{12,}\b/gu,
  /\bAIza[0-9A-Za-z_-]{20,}\b/gu,
  /\b(?:Bearer|Basic)\s+[A-Za-z0-9._~+/=-]{8,}\b/giu,
  /([?&](?:key|api_key|access_token|token)=)[^&\s"'\\]+/giu,
] as const;

export function redactText(value: string): string {
  let redacted = value;
  for (const pattern of SECRET_VALUE_PATTERNS) {
    redacted = redacted.replace(pattern, (_match, prefix: unknown) =>
      typeof prefix === "string" ? `${prefix}[REDACTED]` : "[REDACTED]",
    );
  }
  return redacted;
}

function safeAttributeValue(value: unknown): AttributeValue | undefined {
  if (typeof value === "boolean") return value;
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "string") return redactText(value).slice(0, 512);
  if (
    Array.isArray(value) &&
    value.length <= 32 &&
    value.every((item): item is string => typeof item === "string")
  ) {
    return value.map((item) => redactText(item).slice(0, 256));
  }
  return undefined;
}

export function sanitizeAttributes(
  attributes: Readonly<Record<string, unknown>> | undefined,
): Record<string, AttributeValue> {
  if (!attributes) return {};
  const safe: Record<string, AttributeValue> = {};
  for (const [key, value] of Object.entries(attributes)) {
    if (!SAFE_ATTRIBUTE_KEYS.has(key) && FORBIDDEN_ATTRIBUTE_KEY.test(key)) continue;
    const normalized = safeAttributeValue(value);
    if (normalized !== undefined) safe[key.slice(0, 128)] = normalized;
  }
  return safe;
}

export function sanitizeErrorType(value: string | undefined): string | undefined {
  if (!value) return undefined;
  const normalized = value
    .replace(/[^A-Za-z0-9_.:-]+/g, "_")
    .replace(/^_+|_+$/g, "")
    .slice(0, 128);
  return normalized || "unknown";
}

export function sanitizeErrorMessage(value: string | undefined): string | undefined {
  if (!value) return undefined;
  return redactText(value).trim().slice(0, 512) || undefined;
}
