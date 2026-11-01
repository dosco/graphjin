import { describe, expect, it } from "vitest";
import {
  MAGENTO_LEARN_SKILLS,
  magentoScoreImproved,
  scoreSkillLearnWindow,
  type SkillLearnScoreSample,
} from "../src/work/skill-learn-score";

function sample(
  partial: Partial<SkillLearnScoreSample> & Pick<SkillLearnScoreSample, "skillName">,
): SkillLearnScoreSample {
  return {
    hadCorrection: false,
    hadRelevantFailure: false,
    tokens: 1000,
    latencyMs: 2000,
    ...partial,
  };
}

describe("scoreSkillLearnWindow", () => {
  it("scores Magento refund and fulfillment windows", () => {
    const before = scoreSkillLearnWindow([
      sample({
        skillName: MAGENTO_LEARN_SKILLS[0],
        hadCorrection: true,
        hadRelevantFailure: true,
        tokens: 4000,
        latencyMs: 8000,
      }),
      sample({
        skillName: MAGENTO_LEARN_SKILLS[1],
        hadCorrection: true,
        tokens: 3000,
        latencyMs: 6000,
      }),
    ]);
    const after = scoreSkillLearnWindow([
      sample({ skillName: MAGENTO_LEARN_SKILLS[0], tokens: 2200, latencyMs: 4000 }),
      sample({ skillName: MAGENTO_LEARN_SKILLS[1], tokens: 1800, latencyMs: 3500 }),
    ]);
    expect(before.correctionRate).toBe(1);
    expect(before.failureRate).toBe(0.5);
    expect(after.correctionRate).toBe(0);
    expect(after.failureRate).toBe(0);
    expect(magentoScoreImproved(before, after)).toBe(true);
  });

  it("does not call a cost or latency jump an improvement", () => {
    const before = scoreSkillLearnWindow([
      sample({ skillName: MAGENTO_LEARN_SKILLS[0], tokens: 1000, latencyMs: 1000 }),
    ]);
    const after = scoreSkillLearnWindow([
      sample({
        skillName: MAGENTO_LEARN_SKILLS[0],
        tokens: 5000,
        latencyMs: 9000,
      }),
    ]);
    expect(magentoScoreImproved(before, after)).toBe(false);
  });
});
