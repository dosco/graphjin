import { describe, expect, it } from "vitest";
import { fillDailySeries, formatSavedShort } from "@/lib/hours-saved";

describe("fillDailySeries", () => {
  const since = new Date("2026-06-03T00:00:00.000Z");

  it("returns a zero-filled series of the requested length", () => {
    expect(fillDailySeries(new Map(), since, 7)).toEqual([0, 0, 0, 0, 0, 0, 0]);
  });

  it("places day values oldest → newest and zero-fills gaps", () => {
    const byDay = new Map<string, number>([
      ["2026-06-03", 10],
      ["2026-06-05", 25],
      ["2026-06-09", 40],
    ]);
    expect(fillDailySeries(byDay, since, 7)).toEqual([10, 0, 25, 0, 0, 0, 40]);
  });

  it("ignores days outside the window", () => {
    const byDay = new Map<string, number>([
      ["2026-06-01", 99], // before `since`
      ["2026-06-04", 12],
    ]);
    expect(fillDailySeries(byDay, since, 7)).toEqual([0, 12, 0, 0, 0, 0, 0]);
  });
});

describe("formatSavedShort", () => {
  it("renders minutes under an hour", () => {
    expect(formatSavedShort(18)).toBe("~18 min");
  });
  it("renders hours with one decimal under 10h", () => {
    expect(formatSavedShort(150)).toBe("~2.5h");
  });
  it("renders empty for non-positive", () => {
    expect(formatSavedShort(0)).toBe("");
  });
});
