const fmt = new Intl.NumberFormat("en", { notation: "compact", maximumFractionDigits: 1 });

export function formatCompact(value: number): string {
  if (value === 0) return "0";
  return fmt.format(value);
}
