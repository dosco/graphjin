function main(input) {
  const yields = input.yields || [];
  const defects = input.defects || [];

  const weakYield = yields
    .filter((row) => Number(row.yield_pct || 100) < 88)
    .map((row) => ({ layer_count: row.layer_count, yield_pct: row.yield_pct }));
  const topDefect = defects
    .slice()
    .sort((a, b) => Number(b.defect_count || 0) - Number(a.defect_count || 0))[0];

  return {
    weak_yield: weakYield,
    top_defect: topDefect || null,
    recommendation:
      weakYield.length > 0
        ? "triage plating and drill controls before releasing more 8-layer starts"
        : "yield is within release guardrails",
  };
}
