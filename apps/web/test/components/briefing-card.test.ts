/**
 * Render-shape tests for BriefingCard's "Deep dive" action button.
 * Uses react-dom/server (renderToStaticMarkup) so we can assert on the
 * rendered HTML without pulling in jsdom or @testing-library/react.
 *
 * `use client` components don't have any client-side effects during a
 * server render — the JSX evaluates synchronously and emits HTML. That's
 * enough to verify the gating logic on the Deep dive button.
 */

import { describe, expect, it } from "vitest";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import BriefingCard, {
  type BriefingCardData,
} from "@/components/BriefingCard";

function makeCardData(overrides: Partial<BriefingCardData> = {}): BriefingCardData {
  return {
    id: "card-1",
    metricId: "metric-1",
    source: "bootstrap",
    state: "ok",
    mood: "good",
    text: "Revenue MTD",
    metric: "$4.7M",
    label: "Revenue MTD",
    detail: "Driven by strong renewals in enterprise wholesale.",
    chart: "kpi",
    chartData: [{ d: "May", v: 4700000, t: 4470000 }],
    ...overrides,
  };
}

function render(props: Parameters<typeof BriefingCard>[0]): string {
  return renderToStaticMarkup(createElement(BriefingCard, props));
}

describe("BriefingCard deep-dive button", () => {
  it("renders the Search/Deep dive button when onDeepDive + metricId + state=ok", () => {
    const html = render({
      ins: makeCardData(),
      index: 0,
      onDeepDive: () => {},
    });
    expect(html).toContain('aria-label="Deep dive in Work"');
    expect(html).toContain('data-ui-button=""');
    expect(html).toContain('data-size="icon-sm"');
  });

  it("hides the Deep dive button when onDeepDive is not provided", () => {
    const html = render({ ins: makeCardData(), index: 0 });
    expect(html).not.toContain("Deep dive in Work");
  });

  it("hides the Deep dive button while the card is pending a refresh", () => {
    const html = render({
      ins: makeCardData({ state: "pending" }),
      index: 0,
      onDeepDive: () => {},
    });
    expect(html).not.toContain("Deep dive in Work");
  });

  it("hides the Deep dive button when the metric has no id", () => {
    const html = render({
      ins: makeCardData({ metricId: "" }),
      index: 0,
      onDeepDive: () => {},
    });
    expect(html).not.toContain("Deep dive in Work");
  });
});
