// Human-hours-saved estimate guardrails. The agent self-estimates the
// minutes a competent human would spend on a task; these caps make the
// number safe to surface — one hallucinated estimate can't poison a total.
// See docs/HOURS_SAVED_PLAN.md.

export const HOURS_SAVED = {
  /** Max minutes credited to a single action. */
  perActionCapMin: 120,
  /** Max minutes credited to a single run's analysis. */
  perAnalysisCapMin: 180,
  /** Methodology revision stamped on every estimate row. */
  estimateVersion: 1,
} as const;

function clamp(raw: number | null | undefined, capMin: number): number | null {
  if (raw == null || typeof raw !== "number" || !Number.isFinite(raw)) {
    return null;
  }
  if (raw < 0) return 0;
  return Math.min(Math.round(raw), capMin);
}

/** Clamp an agent's per-action minutes estimate. Null when unusable. */
export function clampActionMinutes(raw: number | null | undefined): number | null {
  return clamp(raw, HOURS_SAVED.perActionCapMin);
}

/** Clamp an agent's per-run analysis minutes estimate. Null when unusable. */
export function clampAnalysisMinutes(
  raw: number | null | undefined,
): number | null {
  return clamp(raw, HOURS_SAVED.perAnalysisCapMin);
}
