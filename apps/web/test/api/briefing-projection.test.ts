import { describe, expect, it } from "vitest";
import { briefingCardsViaChannel } from "@/app/api/briefing/route";

describe("briefingCardsViaChannel", () => {
  it("preserves metric display fields from the web channel projection", () => {
    const cards = briefingCardsViaChannel([
      {
        id: "revenue-mtd",
        metricId: "metric-1",
        source: "bootstrap",
        state: "ok",
        mood: "good",
        text: "Revenue this month",
        metric: "$5.20M",
        label: "Revenue MTD",
        detail: "Up vs last month.",
        chartType: "kpi",
        chartDataOverride: [{ d: "Revenue MTD", v: 5_200_000, t: 4_900_000 }],
      },
    ]);

    expect(cards).toHaveLength(1);
    expect(cards[0]).toMatchObject({
      id: "revenue-mtd",
      component: "BriefingCard",
      metricId: "metric-1",
      source: "bootstrap",
      state: "ok",
      mood: "good",
      text: "Revenue this month",
      metric: "$5.20M",
      label: "Revenue MTD",
      detail: "Up vs last month.",
      chartType: "kpi",
      chartData: [{ d: "Revenue MTD", v: 5_200_000, t: 4_900_000 }],
    });
  });
});
