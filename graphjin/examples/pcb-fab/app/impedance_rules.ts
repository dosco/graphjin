export type Stackup = {
  layerCount: number;
  material: string;
  targetOhms: number;
  traceWidthMil: number;
  dielectricMil: number;
};

export function calculateImpedance(stackup: Stackup): number {
  const materialFactor = stackup.material.includes("408") ? 0.92 : 1.0;
  const geometry = (stackup.dielectricMil / Math.max(stackup.traceWidthMil, 0.1)) * 22;
  const layerPenalty = stackup.layerCount >= 8 ? -2.5 : 0;
  return Math.round((geometry * materialFactor + layerPenalty) * 10) / 10;
}

export function impedanceWindow(targetOhms: number): { lower: number; upper: number } {
  return {
    lower: Math.round(targetOhms * 0.94 * 10) / 10,
    upper: Math.round(targetOhms * 1.06 * 10) / 10,
  };
}

export function classifyImpedance(stackup: Stackup): "pass" | "review" {
  const value = calculateImpedance(stackup);
  const window = impedanceWindow(stackup.targetOhms);
  return value >= window.lower && value <= window.upper ? "pass" : "review";
}
