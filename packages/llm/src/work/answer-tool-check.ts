/** The largest answer tool, in characters. */
export const ANSWER_TOOL_MAX_HTML = 40_000;

const MARKUP_PATTERNS = [
  /\b(src|href|action|formaction|srcset|poster|data)\s*=\s*["']?\s*(?!data:|#)[a-z][a-z0-9+.-]*:/i,
  /\b(src|href|action|formaction|srcset|poster)\s*=\s*["']?\s*\/\//i,
  /url\(\s*["']?\s*(?!data:)[a-z][a-z0-9+.-]*:/i,
  /<\s*(iframe|frame|object|embed|link|base|meta|portal)\b/i,
];
const SCRIPT_PATTERNS = [
  /\blocation\s*(=|\.|\[)/,
  /\b(window|self|top|parent|globalThis|document)\s*\.\s*(location|open)\b/,
  /\bopen\s*\(/,
  /\bimport\s*\(/,
  /\b(fetch|XMLHttpRequest|WebSocket|EventSource|sendBeacon|Worker|SharedWorker)\b/,
];

function scriptText(html: string): string {
  const bodies = [...html.matchAll(/<script\b[^>]*>([\s\S]*?)<\/script\s*>/gi)].map((match) => match[1] ?? "");
  const handlers = [...html.matchAll(/\son[a-z]+\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)/gi)].map((match) => match[1] ?? "");
  return [...bodies, ...handlers].join("\n");
}

/**
 * True when a tool tries to load from or navigate to anything outside its
 * frame. The sandboxed frame is the real boundary; this check gives the model
 * the reason before the reader sees a blank tool.
 */
export function answerToolReachesOut(html: string): boolean {
  const script = scriptText(html);
  return MARKUP_PATTERNS.some((pattern) => pattern.test(html)) || SCRIPT_PATTERNS.some((pattern) => pattern.test(script));
}
