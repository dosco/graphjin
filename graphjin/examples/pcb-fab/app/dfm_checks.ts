export type DesignRule = {
  name: string;
  minTraceMil: number;
  minSpaceMil: number;
  minAnnularRingMil: number;
};

export type DfmFinding = {
  code: string;
  severity: "info" | "warning" | "blocker";
  message: string;
};

export function evaluateDfmRules(rule: DesignRule, measured: Record<string, number>): DfmFinding[] {
  const findings: DfmFinding[] = [];

  if ((measured.traceMil ?? 0) < rule.minTraceMil) {
    findings.push({
      code: "TRACE_WIDTH_LOW",
      severity: "blocker",
      message: `Trace width is below ${rule.minTraceMil} mil`,
    });
  }

  if ((measured.spaceMil ?? 0) < rule.minSpaceMil) {
    findings.push({
      code: "SPACE_LOW",
      severity: "blocker",
      message: `Copper spacing is below ${rule.minSpaceMil} mil`,
    });
  }

  if ((measured.annularRingMil ?? 0) < rule.minAnnularRingMil) {
    findings.push({
      code: "ANNULAR_RING_LOW",
      severity: "warning",
      message: `Annular ring is below ${rule.minAnnularRingMil} mil`,
    });
  }

  return findings;
}

export function camReleaseGate(orderStatus: string, findings: DfmFinding[]): "release" | "hold" {
  if (orderStatus === "engineering_hold") {
    return "hold";
  }
  return findings.some((finding) => finding.severity === "blocker") ? "hold" : "release";
}
