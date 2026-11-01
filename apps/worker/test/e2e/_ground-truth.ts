/**
 * Ground-truth values for E2E sample cards. Hand-derived from the
 * AdventureWorks dataset via `psql`.
 *
 * The E2E tier does NOT pass/fail on these — shape is the only gate.
 * They feed the post-run comparison table so we can eyeball whether
 * the agent landed in the right neighbourhood.
 *
 * Values are recorded per timeWindow.grain. The comparison logic picks
 * the ground-truth row matching the grain the agent reported. Falls back
 * to the all_time value when the grain isn't pre-computed.
 */

import type { TimeWindowGrain } from "@neko/llm";

export type GroundTruthByGrain = Partial<Record<TimeWindowGrain, GroundTruth>>;
export type GroundTruth = { value: number; display: string };

/**
 * AdventureWorks data range: 2022-05-30 → 2025-06-29 (31,465 orders).
 * The agent typically picks `year` (TTM) by default; we record the TTM
 * value computed against the most-recent-data-date anchor (2025-06-29)
 * alongside the all_time total.
 */
export const GROUND_TRUTH: Record<string, GroundTruthByGrain> = {
  // Revenue across both sales channels (Reseller + Internet).
  //   all_time : SELECT sum(subtotal)   → $109,846,381.40
  //              ├─ Reseller: $80,487,704.18 (3,806 orders, onlineorderflag=false)
  //              └─ Internet: $29,358,677.22 (27,659 orders, onlineorderflag=true)
  //   year (TTM 2024-06-30 → 2025-06-29; exactly 365 days anchored to the
  //   most-recent orderdate): $45,004,585.48 over 23,202 orders
  //              ├─ Reseller: $29,389,240.42 (1,531 orders)
  //              └─ Internet: $15,615,345.06 (21,671 orders)
  // AdventureWorks is a manufacturer with no first-party retail — the two
  // channels above are exhaustive (`sales.store` holds reseller partners,
  // not retail outlets). The agent's headlineMetric for a donut card may
  // surface the total or the largest segment; we compare against the total.
  "revenue-by-channel": {
    all_time: { value: 109_846_381, display: "$109.85M" },
    year: { value: 45_004_585, display: "$45.00M" },
  },

  // Order count across all channels.
  //   all_time: 31,465
  //   year (TTM 2024-06-30 → 2025-06-29; exactly 365 days): 23,202
  //   year (calendar 2024): 14,244
  "order-volume-trend": {
    all_time: { value: 31_465, display: "31,465" },
    year: { value: 23_202, display: "23,202" },
  },
};

/**
 * Pick the ground-truth row that best matches the agent's reported grain.
 * Falls back to all_time when the grain isn't pre-computed for a given card.
 */
export function pickGroundTruth(
  slug: string,
  grain: TimeWindowGrain | undefined,
): { gt: GroundTruth | null; matchedGrain: TimeWindowGrain | "fallback" | null } {
  const byGrain = GROUND_TRUTH[slug];
  if (!byGrain) return { gt: null, matchedGrain: null };
  if (grain && byGrain[grain]) {
    return { gt: byGrain[grain]!, matchedGrain: grain };
  }
  if (byGrain.all_time) {
    return { gt: byGrain.all_time, matchedGrain: "fallback" };
  }
  // Last-ditch: return whatever's defined.
  const first = Object.values(byGrain)[0];
  return first ? { gt: first, matchedGrain: "fallback" } : { gt: null, matchedGrain: null };
}
