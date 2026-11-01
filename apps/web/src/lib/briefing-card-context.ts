import type { BriefingCardData } from "@/components/BriefingCard";

/**
 * Sentinel prefix that marks a work_message as a deep-dive briefing card,
 * not a plain text user message. The line below the sentinel (no newline
 * before the closing brace) is the BriefingCardData payload as JSON, so
 * the transcript can swap a card render in place of the markdown bubble
 * — and the agent can still read the same structured fields via the
 * normal thread history mapping.
 */
export const BRIEFING_CARD_SENTINEL = "::neko-briefing-card::";

/**
 * Returns the parsed BriefingCardData if `content` is a briefing-card
 * context message, else null. Tolerant of trailing whitespace / extra
 * lines after the JSON; only the first line is the data payload.
 */
export function parseBriefingCardMessage(
  content: string,
): BriefingCardData | null {
  if (!content.startsWith(BRIEFING_CARD_SENTINEL)) return null;
  const newlineAt = content.indexOf("\n");
  const jsonStr = content.slice(
    BRIEFING_CARD_SENTINEL.length,
    newlineAt === -1 ? undefined : newlineAt,
  );
  try {
    return JSON.parse(jsonStr) as BriefingCardData;
  } catch {
    return null;
  }
}
