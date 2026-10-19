/**
 * Stub return-value factories for `@neko/llm` exports. Test files mock the
 * module via `vi.mock("@neko/llm", ...)` and use these factories to build
 * deterministic responses with the right shape.
 *
 * Usage pattern in a test file:
 *
 *   import { vi } from "vitest";
 *   import { stubMetricAgentResult, stubClassification } from "../_helpers/mock-llm";
 *
 *   const { runMetricAgent, classifyQuestion } = vi.hoisted(() => ({
 *     runMetricAgent: vi.fn(),
 *     classifyQuestion: vi.fn(),
 *   }));
 *   vi.mock("@neko/llm", () => ({
 *     runMetricAgent,
 *     classifyQuestion,
 *     provisionHostConfig: vi.fn(),
 *   }));
 *
 *   beforeEach(() => {
 *     runMetricAgent.mockResolvedValue(stubMetricAgentResult());
 *     classifyQuestion.mockResolvedValue(stubClassification());
 *   });
 */

import type { MetricAgentResult } from "@neko/llm";

export function stubMetricAgentResult(
  overrides: Partial<MetricAgentResult> = {},
): MetricAgentResult {
  return {
    reasoning: "stub: ran agent against test fixture",
    headlineMetric: "$1.00M",
    headlineLabel: "Test Metric",
    insightText: "Numbers look fine.",
    detailText: "Driven by stub data.",
    mood: "watch",
    chartType: "kpi",
    chartData: [{ d: "value", v: 1_000_000, t: 950_000 }],
    baselineValue: 950_000,
    timeWindow: {
      grain: "year",
      start: "2024-04-01",
      end: "2025-04-01",
      label: "TTM",
    },
    ...overrides,
  };
}

export type ClassificationStub = {
  slug: string;
  title: string;
  why: string;
  chartHint: "kpi" | "line" | "bar" | "donut" | "area";
  role?: string;
};

export function stubClassification(
  overrides: Partial<ClassificationStub> = {},
): ClassificationStub {
  return {
    slug: "test-question",
    title: "What is the test question?",
    why: "User asked something testable.",
    chartHint: "bar",
    role: "CEO",
    ...overrides,
  };
}
