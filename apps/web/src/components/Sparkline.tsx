// Tiny bar sparkline — shared by the workflows list and the hours-saved hero.
// Pure presentational: bars scaled to the series max over one baseline.

export function Sparkline({
  values,
  width = 88,
  height = 16,
}: {
  values: number[];
  width?: number;
  height?: number;
}) {
  if (values.length === 0) return null;
  const max = Math.max(1, ...values);
  const barW = width / values.length;
  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      aria-hidden="true"
      style={{ display: "block" }}
    >
      <rect x={0} y={height - 1} width={width} height={1} fill="var(--border)" />
      {values.map((v, i) =>
        v === 0 ? null : (
          <rect
            key={i}
            x={i * barW + barW * 0.2}
            y={height - Math.max(2, (v / max) * height)}
            width={Math.max(1.5, barW * 0.6)}
            height={Math.max(2, (v / max) * height)}
            rx={Math.min(1, barW * 0.3)}
            fill="var(--accent)"
            opacity={0.7}
          />
        ),
      )}
    </svg>
  );
}
