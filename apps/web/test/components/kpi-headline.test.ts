import { describe, expect, it } from "vitest";
import { deltaToneClass, formatDeltaPct } from "@/components/KpiHeadline";

describe("formatDeltaPct", () => {
  it.each([
    [0, "0.0%"],
    [0.1, "0.1%"],
    [5.123, "5.1%"],
    [9.94, "9.9%"],
    [10, "10%"],
    [11.5, "12%"],
    [150, "150%"],
    [-5.4, "5.4%"],
    [-150, "150%"],
  ])("formats %p as %p (sign stripped, %% always shown)", (pct, expected) => {
    expect(formatDeltaPct(pct)).toBe(expected);
  });

  it("uses 1 decimal below 10 and rounds at/above 10", () => {
    expect(formatDeltaPct(9.99)).toBe("10.0%");
    expect(formatDeltaPct(10.4)).toBe("10%");
  });
});

describe("deltaToneClass", () => {
  const up = { isUp: true, isBigDrop: false };
  const down = { isUp: false, isBigDrop: false };
  const crash = { isUp: false, isBigDrop: true };

  it("forces crash tone for act/bad moods regardless of direction", () => {
    expect(deltaToneClass(up, "act")).toBe("kpi-delta-crash");
    expect(deltaToneClass(down, "act")).toBe("kpi-delta-crash");
    expect(deltaToneClass(up, "bad")).toBe("kpi-delta-crash");
    expect(deltaToneClass(crash, "bad")).toBe("kpi-delta-crash");
  });

  it("uses warm down tone for watch mood regardless of direction", () => {
    expect(deltaToneClass(up, "watch")).toBe("kpi-delta-down");
    expect(deltaToneClass(down, "watch")).toBe("kpi-delta-down");
    expect(deltaToneClass(crash, "watch")).toBe("kpi-delta-down");
  });

  it("falls back to direction-based tone when mood is good/missing/unknown", () => {
    expect(deltaToneClass(up, "good")).toBe("kpi-delta-up");
    expect(deltaToneClass(down, "good")).toBe("kpi-delta-down");
    expect(deltaToneClass(up, undefined)).toBe("kpi-delta-up");
    expect(deltaToneClass(down, undefined)).toBe("kpi-delta-down");
    expect(deltaToneClass(up, "neutral")).toBe("kpi-delta-up");
  });

  it("escalates a >=20% drop to crash even on neutral/good moods", () => {
    expect(deltaToneClass(crash, "good")).toBe("kpi-delta-crash");
    expect(deltaToneClass(crash, undefined)).toBe("kpi-delta-crash");
  });
});
