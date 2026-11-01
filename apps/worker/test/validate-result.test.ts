import { describe, expect, it } from "vitest";
import { validateResult } from "../src/jobs/metric-refresh";
import type { MetricAgentResult } from "@neko/llm";

const valid: MetricAgentResult = {
  reasoning: "computed revenue from salesorderheader filtered to MTD",
  headlineMetric: "$4.7M",
  headlineLabel: "Revenue MTD",
  insightText: "Revenue is 3.2% above target this month.",
  detailText: "Driven by enterprise renewals.",
  mood: "good",
  chartType: "bar",
  chartData: [
    { d: "Mon", v: 100 },
    { d: "Tue", v: 110 },
  ],
  timeWindow: {
    grain: "month",
    start: "2025-04-01",
    end: "2025-04-30",
    label: "MTD",
  },
};

describe("validateResult — agent output quality gate", () => {
  it("accepts a fully-populated valid result", () => {
    expect(validateResult(valid)).toBeNull();
  });

  it("rejects empty headlineMetric", () => {
    expect(validateResult({ ...valid, headlineMetric: "" })).toMatch(
      /empty headlineMetric/,
    );
  });

  it("rejects empty headlineLabel", () => {
    expect(validateResult({ ...valid, headlineLabel: "" })).toMatch(
      /empty headlineLabel/,
    );
  });

  it("rejects empty insightText", () => {
    expect(validateResult({ ...valid, insightText: "" })).toMatch(
      /empty insightText/,
    );
  });

  it("rejects unknown mood values", () => {
    expect(
      validateResult({ ...valid, mood: "neutral" as unknown as "good" }),
    ).toMatch(/invalid mood/);
  });

  it("accepts every documented mood", () => {
    for (const mood of ["good", "watch", "bad"] as const) {
      expect(validateResult({ ...valid, mood })).toBeNull();
    }
  });

  it("rejects unknown chartType values", () => {
    expect(
      validateResult({
        ...valid,
        chartType: "scatter" as unknown as "bar",
      }),
    ).toMatch(/invalid chartType/);
  });

  it("rejects empty chartData array", () => {
    expect(validateResult({ ...valid, chartData: [] })).toMatch(
      /empty chartData/,
    );
  });

  it("rejects NaN in chartData.v", () => {
    expect(
      validateResult({ ...valid, chartData: [{ d: "Mon", v: NaN }] }),
    ).toMatch(/non-numeric/);
  });

  it("rejects missing d label in chartData", () => {
    expect(
      validateResult({
        ...valid,
        chartData: [{ d: "", v: 5 }],
      }),
    ).toMatch(/missing d label/);
  });

  describe("kpi chartType invariants", () => {
    const baseKpi: MetricAgentResult = {
      ...valid,
      chartType: "kpi",
      chartData: [{ d: "Revenue", v: 4_700_000, t: 4_550_000 }],
    };

    it("accepts a single-item kpi with baseline", () => {
      expect(validateResult(baseKpi)).toBeNull();
    });

    it("rejects kpi with more than one chartData item", () => {
      expect(
        validateResult({
          ...baseKpi,
          chartData: [
            { d: "a", v: 1, t: 1 },
            { d: "b", v: 2, t: 2 },
          ],
        }),
      ).toMatch(/exactly 1 chartData item/);
    });

    it("rejects kpi without a baseline (missing t)", () => {
      expect(
        validateResult({
          ...baseKpi,
          chartData: [{ d: "Revenue", v: 1 }],
        }),
      ).toMatch(/baseline.*value/);
    });
  });

  describe("timeWindow", () => {
    it("rejects missing timeWindow field", () => {
      const noWindow = { ...valid } as MetricAgentResult;
      // @ts-expect-error — deliberately remove for the test
      delete noWindow.timeWindow;
      expect(validateResult(noWindow)).toMatch(/missing timeWindow/);
    });

    it("rejects unknown grain", () => {
      expect(
        validateResult({
          ...valid,
          timeWindow: { ...valid.timeWindow, grain: "decade" as never },
        }),
      ).toMatch(/invalid timeWindow.grain/);
    });

    it("rejects empty label", () => {
      expect(
        validateResult({
          ...valid,
          timeWindow: { ...valid.timeWindow, label: "" },
        }),
      ).toMatch(/empty timeWindow.label/);
    });

    it("rejects non-iso start date", () => {
      expect(
        validateResult({
          ...valid,
          timeWindow: { ...valid.timeWindow, start: "Apr 1 2025" },
        }),
      ).toMatch(/timeWindow.start must be yyyy-mm-dd/);
    });

    it("rejects non-iso end date", () => {
      expect(
        validateResult({
          ...valid,
          timeWindow: { ...valid.timeWindow, end: "2025/04/30" },
        }),
      ).toMatch(/timeWindow.end must be yyyy-mm-dd/);
    });

    it("allows null start/end only when grain='all_time'", () => {
      expect(
        validateResult({
          ...valid,
          timeWindow: { grain: "all_time", start: null, end: null, label: "All time" },
        }),
      ).toBeNull();

      expect(
        validateResult({
          ...valid,
          timeWindow: { grain: "year", start: null, end: null, label: "TTM" },
        }),
      ).toMatch(/timeWindow.start must be yyyy-mm-dd/);
    });

    it("accepts every valid grain", () => {
      for (const grain of ["day", "week", "month", "quarter", "year", "snapshot"] as const) {
        expect(
          validateResult({
            ...valid,
            timeWindow: { grain, start: "2025-04-01", end: "2025-04-30", label: grain },
          }),
          `grain=${grain}`,
        ).toBeNull();
      }
    });
  });

  describe("headlineMetric error sentinels", () => {
    // The agent occasionally narrates a failed data fetch by emitting
    // headlineMetric: "Error" with a chart payload describing the error.
    // Without rejection, the snapshot row lands and the dashboard renders
    // the error narrative as if it were a real metric.
    it.each([
      "Error",
      "errors",
      "N/A",
      "n/a",
      "Unavailable",
      "Data Unavailable",
      "Data Unavilable", // tolerate the agent's typo seen in the wild
      "No data",
      "null",
      "undefined",
      "—",
      "-",
      "?",
      "tbd",
      "  Error  ",
      "ERROR",
    ])("rejects sentinel headline %j", (headline) => {
      expect(validateResult({ ...valid, headlineMetric: headline })).toMatch(
        /sentinel/,
      );
    });

    it.each(["-—", "???", "    ", "..."])(
      "rejects pure-punctuation headline %j",
      (headline) => {
        expect(validateResult({ ...valid, headlineMetric: headline })).toMatch(
          /sentinel|empty/,
        );
      },
    );

    it.each(["$1.2M", "31,465", "+8%", "1.8x", "26.3%", "$45.00M"])(
      "accepts legitimate headline %j",
      (headline) => {
        expect(validateResult({ ...valid, headlineMetric: headline })).toBeNull();
      },
    );
  });
});
