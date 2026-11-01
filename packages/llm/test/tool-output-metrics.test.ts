import { afterEach, describe, expect, it, vi } from "vitest";
import {
  estimateTokens,
  measureToolResult,
  metricsEnabled,
  recordToolResult,
  toolResultToText,
} from "../src/work/tool-output/metrics";

describe("estimateTokens", () => {
  it("approximates ~4 chars per token", () => {
    expect(estimateTokens("")).toBe(0);
    expect(estimateTokens("abcd")).toBe(1);
    expect(estimateTokens("abcde")).toBe(2);
  });
});

describe("toolResultToText", () => {
  it("returns strings unchanged", () => {
    expect(toolResultToText("hello")).toBe("hello");
  });

  it("joins Anthropic/ACP text-block arrays", () => {
    expect(
      toolResultToText([
        { type: "text", text: "foo" },
        { type: "text", text: "bar" },
      ]),
    ).toBe("foobar");
  });

  it("stringifies plain objects", () => {
    expect(toolResultToText({ a: 1 })).toBe('{"a":1}');
  });

  it("empty/nullish → empty string", () => {
    expect(toolResultToText(null)).toBe("");
    expect(toolResultToText(undefined)).toBe("");
  });
});

describe("measureToolResult", () => {
  it("reports real savings on a GraphJin-shaped row array", () => {
    const products = Array.from({ length: 100 }, (_, i) => ({
      productid: i + 1,
      name: `Product ${i + 1}`,
      quantity: i,
      reorderpoint: 25,
    }));
    const metric = measureToolResult({
      tool: "Bash",
      result: JSON.stringify({ data: { products } }),
    });
    expect(metric).not.toBeNull();
    expect(metric!.looksJson).toBe(true);
    expect(metric!.format).toBe("columnar");
    expect(metric!.estTokensSaved).toBeGreaterThan(0);
    expect(metric!.savingsPct).toBeGreaterThan(40);
  });

  it("reports zero savings (and passthrough) on plain-text output", () => {
    const metric = measureToolResult({
      tool: "Bash",
      result: "rows: 3 (4ms)",
    });
    expect(metric).not.toBeNull();
    expect(metric!.looksJson).toBe(false);
    expect(metric!.estTokensSaved).toBe(0);
    expect(metric!.savingsPct).toBe(0);
    expect(metric!.format).toBe("passthrough");
  });

  it("returns null for empty output", () => {
    expect(measureToolResult({ tool: "Bash", result: "" })).toBeNull();
    expect(measureToolResult({ tool: "Bash", result: null })).toBeNull();
  });
});

describe("metricsEnabled — flag parsing", () => {
  afterEach(() => {
    delete process.env.OPENNEKO_TOOL_OUTPUT_METRICS;
  });

  it("off when unset", () => {
    delete process.env.OPENNEKO_TOOL_OUTPUT_METRICS;
    expect(metricsEnabled()).toBe(false);
  });

  it('off when "0"', () => {
    process.env.OPENNEKO_TOOL_OUTPUT_METRICS = "0";
    expect(metricsEnabled()).toBe(false);
  });

  it('on for "1" or any other non-empty value', () => {
    process.env.OPENNEKO_TOOL_OUTPUT_METRICS = "1";
    expect(metricsEnabled()).toBe(true);
    process.env.OPENNEKO_TOOL_OUTPUT_METRICS = "true";
    expect(metricsEnabled()).toBe(true);
  });
});

describe("recordToolResult — flag-gated logging", () => {
  afterEach(() => {
    delete process.env.OPENNEKO_TOOL_OUTPUT_METRICS;
    vi.restoreAllMocks();
  });

  it("emits nothing when the flag is off", () => {
    delete process.env.OPENNEKO_TOOL_OUTPUT_METRICS;
    const info = vi.spyOn(console, "info").mockImplementation(() => {});
    recordToolResult({ tool: "Bash", result: '{"a":[1,2,3]}' });
    expect(info).not.toHaveBeenCalled();
  });

  it("logs one structured line when the flag is on", () => {
    process.env.OPENNEKO_TOOL_OUTPUT_METRICS = "1";
    const info = vi.spyOn(console, "info").mockImplementation(() => {});
    const products = Array.from({ length: 5 }, (_, i) => ({ id: i, name: `p${i}` }));
    recordToolResult({ tool: "graphjin", result: JSON.stringify({ data: { products } }) });
    expect(info).toHaveBeenCalledTimes(1);
    const line = info.mock.calls[0][0] as string;
    expect(line.startsWith("[tool-output-metrics] ")).toBe(true);
    const metric = JSON.parse(line.replace("[tool-output-metrics] ", ""));
    expect(metric.tool).toBe("graphjin");
    expect(metric.estTokens).toBeGreaterThan(0);
  });

  it("skips empty results even when enabled", () => {
    process.env.OPENNEKO_TOOL_OUTPUT_METRICS = "1";
    const info = vi.spyOn(console, "info").mockImplementation(() => {});
    recordToolResult({ tool: "Bash", result: "" });
    expect(info).not.toHaveBeenCalled();
  });

  it("never throws, even if measurement would blow up", () => {
    process.env.OPENNEKO_TOOL_OUTPUT_METRICS = "1";
    vi.spyOn(console, "info").mockImplementation(() => {});
    const circular: Record<string, unknown> = {};
    circular.self = circular; // pathological input must not break the run
    expect(() => recordToolResult({ tool: "Bash", result: circular })).not.toThrow();
  });
});
