import { describe, expect, it } from "vitest";
import { extractValueFence } from "../src/workflows/fence-parsers";
import {
  HOURS_SAVED,
  clampActionMinutes,
  clampAnalysisMinutes,
} from "../src/workflows/value";

describe("extractValueFence", () => {
  it("returns null payload when no fence is present", () => {
    const r = extractValueFence("just an answer, nothing else");
    expect(r.payload).toBeNull();
    expect(r.errors).toEqual([]);
    expect(r.text).toBe("just an answer, nothing else");
  });

  it("parses a valid value fence and strips it from text", () => {
    const raw = [
      "Here's the revenue report.",
      "",
      "```neko_value",
      JSON.stringify({ minutes_saved: 18, basis: "Cross-checked a 3-table report" }),
      "```",
    ].join("\n");
    const r = extractValueFence(raw);
    expect(r.payload?.minutes_saved).toBe(18);
    expect(r.payload?.basis).toBe("Cross-checked a 3-table report");
    expect(r.text).toBe("Here's the revenue report.");
  });

  it("accepts a zero estimate (no-op runs report 0)", () => {
    const raw = '```neko_value\n{ "minutes_saved": 0 }\n```';
    const r = extractValueFence(raw);
    expect(r.payload?.minutes_saved).toBe(0);
  });

  it("rejects a negative estimate as invalid", () => {
    const raw = '```neko_value\n{ "minutes_saved": -5 }\n```';
    const r = extractValueFence(raw);
    expect(r.payload).toBeNull();
    expect(r.errors.length).toBe(1);
  });

  it("keeps the last fence when several are emitted", () => {
    const raw = [
      '```neko_value\n{ "minutes_saved": 5 }\n```',
      '```neko_value\n{ "minutes_saved": 30 }\n```',
    ].join("\n\n");
    const r = extractValueFence(raw);
    expect(r.payload?.minutes_saved).toBe(30);
  });
});

describe("clamping", () => {
  it("caps an over-eager action estimate", () => {
    expect(clampActionMinutes(99999)).toBe(HOURS_SAVED.perActionCapMin);
  });

  it("caps an over-eager analysis estimate", () => {
    expect(clampAnalysisMinutes(99999)).toBe(HOURS_SAVED.perAnalysisCapMin);
  });

  it("floors negatives at 0 and drops non-numbers", () => {
    expect(clampActionMinutes(-10)).toBe(0);
    expect(clampActionMinutes(null)).toBeNull();
    expect(clampActionMinutes(undefined)).toBeNull();
    expect(clampActionMinutes(Number.NaN)).toBeNull();
  });

  it("rounds and passes through values within the cap", () => {
    expect(clampAnalysisMinutes(17.6)).toBe(18);
    expect(clampActionMinutes(8)).toBe(8);
  });
});
