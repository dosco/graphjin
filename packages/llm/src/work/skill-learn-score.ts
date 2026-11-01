export type SkillLearnScoreSample = {
  skillName: string;
  hadCorrection: boolean;
  hadRelevantFailure: boolean;
  tokens: number;
  latencyMs: number;
};

export type SkillLearnScore = {
  samples: number;
  correctionRate: number;
  failureRate: number;
  meanTokens: number;
  meanLatencyMs: number;
};

export const MAGENTO_LEARN_SKILLS = [
  "magento-investigate-refunds",
  "magento-triage-fulfillment",
] as const;

export function scoreSkillLearnWindow(
  samples: readonly SkillLearnScoreSample[],
): SkillLearnScore {
  const n = samples.length;
  if (n === 0) {
    return {
      samples: 0,
      correctionRate: 0,
      failureRate: 0,
      meanTokens: 0,
      meanLatencyMs: 0,
    };
  }
  let corrections = 0;
  let failures = 0;
  let tokens = 0;
  let latency = 0;
  for (const sample of samples) {
    if (sample.hadCorrection) corrections += 1;
    if (sample.hadRelevantFailure) failures += 1;
    tokens += sample.tokens;
    latency += sample.latencyMs;
  }
  return {
    samples: n,
    correctionRate: corrections / n,
    failureRate: failures / n,
    meanTokens: tokens / n,
    meanLatencyMs: latency / n,
  };
}

export function magentoScoreImproved(
  before: SkillLearnScore,
  after: SkillLearnScore,
): boolean {
  if (after.samples === 0 || before.samples === 0) return false;
  return (
    after.correctionRate <= before.correctionRate &&
    after.failureRate <= before.failureRate &&
    after.meanTokens <= before.meanTokens * 1.25 &&
    after.meanLatencyMs <= before.meanLatencyMs * 1.25
  );
}
