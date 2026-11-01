const CURRENCIES: Record<string, string> = { usd: "USD", "$": "USD", eur: "EUR", "€": "EUR", gbp: "GBP", "£": "GBP", aud: "AUD", cad: "CAD", inr: "INR", "₹": "INR" };
const SYMBOLS: Record<string, string> = { "$": "USD", "€": "EUR", "£": "GBP", "₹": "INR" };

export function currencyOf(unit: string | undefined): string | null {
  return unit ? CURRENCIES[unit.trim().toLowerCase()] ?? null : null;
}

/** A computed result in its unit: $1.52M, 12.4%, 1,250 units. */
export function formatUnitValue(value: number, unit?: string, opts: { compact?: boolean } = {}): string {
  if (!Number.isFinite(value)) return "—";
  const currency = currencyOf(unit);
  const compact = (opts.compact ?? true) && Math.abs(value) >= 100_000;
  const base: Intl.NumberFormatOptions = compact
    ? { notation: "compact", minimumSignificantDigits: 3, maximumSignificantDigits: 3 }
    : { maximumFractionDigits: Math.abs(value) >= 1_000 ? 0 : 2 };
  if (currency) return new Intl.NumberFormat("en-US", { ...base, style: "currency", currency }).format(value);
  const number = new Intl.NumberFormat("en-US", base).format(value);
  if (!unit) return number;
  return unit === "%" ? `${number}%` : `${number} ${unit}`;
}

const FIGURE = /^([$€£₹])?\s*(-?[\d,]+(?:\.\d+)?)\s*([A-Za-z%]{0,12})$/;

/**
 * A headline figure as the agent wrote it, shortened for display when it is
 * a large plain amount. The exact text stays available as `exact`.
 */
export function displayFigure(value: string): { display: string; exact: string | null } {
  const match = FIGURE.exec(value.trim());
  if (!match || match[3]) return { display: value, exact: null };
  const amount = Number(match[2]!.replaceAll(",", ""));
  if (!Number.isFinite(amount) || Math.abs(amount) < 10_000) return { display: value, exact: null };
  const currency = match[1] ? SYMBOLS[match[1]] : undefined;
  const display = Math.abs(amount) >= 1_000_000
    ? formatUnitValue(amount, currency ?? undefined)
    : new Intl.NumberFormat("en-US", currency ? { style: "currency", currency, maximumFractionDigits: 0 } : { maximumFractionDigits: 0 }).format(amount);
  return display === value ? { display: value, exact: null } : { display, exact: value };
}
