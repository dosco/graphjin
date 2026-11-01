/**
 * Tolerant numeric parser for the agent's `headlineMetric` field.
 *
 * The agent emits human-friendly strings: "$45.0M", "31,465", "+8%",
 * "1.8x", "26.3%". We parse these for comparison-table arithmetic.
 * Returns null on anything that doesn't look like a number — the
 * comparison row falls back to "—" for that case.
 *
 * Suffix handling:
 *   K / k  → ×1,000
 *   M / m  → ×1,000,000
 *   B / b  → ×1,000,000,000
 *   x      → ×1 (kept as-is; ratios)
 *   %      → ×1 (kept as-is; consumer applies meaning)
 *
 * The agent occasionally returns plain numbers without units for counts
 * ("31,465"). Commas, $ signs and whitespace are stripped before parsing.
 */

const HEADLINE_PATTERN = /^([+-]?\d+(?:\.\d+)?)([kmb])?\s*([%x]?)$/i;

export function parseHeadline(s: string): number | null {
  if (!s) return null;
  const cleaned = s.replace(/[\s$,]/g, "").trim();
  const m = cleaned.match(HEADLINE_PATTERN);
  if (!m) return null;
  let n = parseFloat(m[1]);
  if (!Number.isFinite(n)) return null;
  const suffix = m[2]?.toLowerCase();
  if (suffix === "k") n *= 1_000;
  else if (suffix === "m") n *= 1_000_000;
  else if (suffix === "b") n *= 1_000_000_000;
  return n;
}

/**
 * Format a percent delta from ground truth. `null` when either side
 * is unavailable.
 */
export function formatDeltaPct(actual: number | null, truth: number): string {
  if (actual == null) return "—";
  if (truth === 0) return actual === 0 ? "=" : "n/a";
  const deltaPct = ((actual - truth) / truth) * 100;
  if (Math.abs(deltaPct) < 0.05) return "=";
  const sign = deltaPct > 0 ? "+" : "";
  return `${sign}${deltaPct.toFixed(1)}%`;
}
