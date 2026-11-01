import { describe, expect, it } from "vitest";
import {
  BRIEFING_CARD_SENTINEL,
  parseBriefingCardMessage,
} from "@/lib/briefing-card-context";

function makeCard(overrides: Record<string, unknown> = {}) {
  return {
    id: "seed-abc",
    metricId: "abc-123",
    source: "briefing",
    state: "ok",
    mood: "good",
    text: "Revenue Momentum",
    metric: "$50.37M",
    label: "TTM Revenue",
    detail: "Revenue surged 28.1% YoY.",
    chart: "line",
    chartData: [
      { d: "Jan", v: 1, t: 0.9 },
      { d: "Feb", v: 1.1, t: 0.95 },
    ],
    ...overrides,
  };
}

describe("parseBriefingCardMessage", () => {
  it("returns null when content lacks the sentinel", () => {
    expect(parseBriefingCardMessage("just a regular user message")).toBeNull();
    expect(parseBriefingCardMessage("")).toBeNull();
    expect(
      parseBriefingCardMessage(` ${BRIEFING_CARD_SENTINEL}{"id":"x"}`),
    ).toBeNull();
  });

  it("parses the JSON payload that follows the sentinel", () => {
    const card = makeCard();
    const content = `${BRIEFING_CARD_SENTINEL}${JSON.stringify(card)}`;
    const parsed = parseBriefingCardMessage(content);
    expect(parsed).not.toBeNull();
    expect(parsed?.metricId).toBe("abc-123");
    expect(parsed?.mood).toBe("good");
    expect(parsed?.chartData).toHaveLength(2);
  });

  it("returns null on malformed JSON after the sentinel", () => {
    expect(
      parseBriefingCardMessage(`${BRIEFING_CARD_SENTINEL}{not-json`),
    ).toBeNull();
    expect(parseBriefingCardMessage(BRIEFING_CARD_SENTINEL)).toBeNull();
    expect(
      parseBriefingCardMessage(`${BRIEFING_CARD_SENTINEL}undefined`),
    ).toBeNull();
  });

  it("only treats content up to the first newline as the JSON payload", () => {
    // Anything after the first newline is fallback prose for the agent and
    // must not corrupt the JSON parse.
    const card = makeCard({ text: "Inventory turn" });
    const content =
      `${BRIEFING_CARD_SENTINEL}${JSON.stringify(card)}` +
      "\n\nExtra context the agent can read here.";
    const parsed = parseBriefingCardMessage(content);
    expect(parsed?.text).toBe("Inventory turn");
  });

  it("survives JSON containing newlines inside string fields", () => {
    // JSON.stringify escapes \n inside strings, so the first newline in the
    // serialised payload is always the post-JSON separator. This guards
    // against a future regression where pretty-printed JSON could break it.
    const card = makeCard({ detail: "line one\nline two" });
    const content = `${BRIEFING_CARD_SENTINEL}${JSON.stringify(card)}`;
    expect(content.split("\n")).toHaveLength(1);
    const parsed = parseBriefingCardMessage(content);
    expect(parsed?.detail).toBe("line one\nline two");
  });
});
