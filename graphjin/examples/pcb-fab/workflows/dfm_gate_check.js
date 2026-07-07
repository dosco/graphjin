function main(input) {
  const order = input.order || {};
  const design = input.design || {};
  const bom = input.bom || [];
  const measurements = input.measurements || [];

  const highRiskParts = bom.filter((item) => item.criticality === "critical" && Number(item.lead_time_days || 0) > 14);
  const failingMeasurements = measurements.filter((m) => m.status === "fail");
  const impedanceTarget = Number(design.target_impedance_ohms || 0);
  const impedanceRisk = failingMeasurements.some((m) => String(m.measurement || "").includes("impedance"));

  return {
    order_code: order.order_code,
    checks: {
      high_risk_parts: highRiskParts.length,
      failing_measurements: failingMeasurements.length,
      impedance_target_ohms: impedanceTarget,
      impedance_risk: impedanceRisk,
    },
    recommendation:
      highRiskParts.length > 0 || failingMeasurements.length > 0
        ? "hold CAM release until DFM and supplier risks are cleared"
        : "release to CAM",
  };
}
