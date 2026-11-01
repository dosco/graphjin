import type { SkillLearnPayload } from "@neko/db/jobs";
import { runSkillLearnForOrgSkill } from "@neko/llm/work";

export async function runSkillLearnJob(
  payload: SkillLearnPayload,
): Promise<void> {
  const result = await runSkillLearnForOrgSkill({
    orgId: payload.orgId,
    skillName: payload.skillName,
  });
  console.log(
    `[skill_learn] org=${payload.orgId} skill=${payload.skillName} decision=${result.decision} reason=${result.reason}`,
  );
}
