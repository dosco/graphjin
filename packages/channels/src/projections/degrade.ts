import type { Fidelity } from "@neko/interaction";

export const clampChars = (text: string, max?: number): string => {
  if (!max || text.length <= max) return text;
  if (max <= 1) return text.slice(0, Math.max(0, max));
  return `${text.slice(0, max - 1)}…`;
};

export const firstSentence = (text: string): string => {
  const match = text.match(/^.*?[.!?](\s|$)/);
  return (match?.[0] ?? text).trim();
};

export const summarizeBody = (body: string, fidelity: Fidelity): string => {
  if (fidelity === "full") return body;
  if (fidelity === "headline") return "";
  return firstSentence(body);
};

const XML_ESCAPES: Record<string, string> = {
  "&": "&amp;",
  "<": "&lt;",
  ">": "&gt;",
  '"': "&quot;",
  "'": "&apos;",
};

export const escapeXml = (text: string): string =>
  text.replace(/[&<>"']/g, (char) => XML_ESCAPES[char] ?? char);
