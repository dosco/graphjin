import {
  and,
  db,
  eq,
  skill_learn_event,
  skill_learn_org,
  skill_learn_state,
  skill_usage,
  sql,
} from "@neko/db";
import { recordConfigChange } from "../config-vcs";
import { getOrgAgentRoot } from "./workspace";
import { learnedBodyHash, runSkillLearn, type SkillLearnResult } from "./skill-learn";

export type SkillLearnOrgSettings = {
  enabled: boolean;
  source: "org" | "default";
};

export async function getSkillLearnOrgSettings(
  orgId: string,
): Promise<SkillLearnOrgSettings> {
  const [row] = await db()
    .select({ enabled: skill_learn_org.enabled })
    .from(skill_learn_org)
    .where(eq(skill_learn_org.org_id, orgId))
    .limit(1);
  if (!row) return { enabled: false, source: "default" };
  return { enabled: row.enabled, source: "org" };
}

export async function setSkillLearnOrgEnabled(input: {
  orgId: string;
  enabled: boolean;
}): Promise<SkillLearnOrgSettings> {
  await db()
    .insert(skill_learn_org)
    .values({
      org_id: input.orgId,
      enabled: input.enabled,
      updated_at: new Date(),
    })
    .onConflictDoUpdate({
      target: skill_learn_org.org_id,
      set: {
        enabled: input.enabled,
        updated_at: new Date(),
      },
    });
  return { enabled: input.enabled, source: "org" };
}

export async function runSkillLearnForOrgSkill(input: {
  orgId: string;
  skillName: string;
}): Promise<SkillLearnResult> {
  const orgRoot = getOrgAgentRoot(input.orgId);
  const [orgRow] = await db()
    .select({ enabled: skill_learn_org.enabled })
    .from(skill_learn_org)
    .where(eq(skill_learn_org.org_id, input.orgId))
    .limit(1);
  const [stateRow] = await db()
    .select()
    .from(skill_learn_state)
    .where(
      and(
        eq(skill_learn_state.org_id, input.orgId),
        eq(skill_learn_state.skill_name, input.skillName),
      ),
    )
    .limit(1);
  const usageRows = await db()
    .select({
      runId: skill_usage.run_id,
      skillName: skill_usage.skill_name,
      contentHash: skill_usage.content_hash,
      createdAt: skill_usage.created_at,
    })
    .from(skill_usage)
    .where(
      and(
        eq(skill_usage.org_id, input.orgId),
        eq(skill_usage.skill_name, input.skillName),
      ),
    );

  return db().transaction(async (tx) => {
    await tx.execute(
      sql`select pg_advisory_xact_lock(hashtext(${input.orgId}), hashtext(${input.skillName}))`,
    );

    const result = await runSkillLearn({
      orgId: input.orgId,
      orgRoot,
      skillName: input.skillName,
      orgEnabled: orgRow?.enabled === true,
      skillEnabled: stateRow ? stateRow.enabled : true,
      usages: usageRows.map((row) => ({
        runId: row.runId,
        skillName: row.skillName,
        contentHash: row.contentHash,
        createdAt: row.createdAt,
      })),
    });

    await tx.insert(skill_learn_event).values({
      org_id: input.orgId,
      skill_name: input.skillName,
      base_hash: result.trace.baseHash ?? null,
      content_hash: result.trace.contentHash ?? null,
      run_ids: result.trace.evidenceRunIds ?? [],
      lesson: result.trace.lesson ?? null,
      rationale: result.trace.rationale ?? null,
      diff: result.diff ?? null,
      model_trace: result.trace,
      decision: result.decision,
      reason: result.reason,
    });

    if (result.decision === "applied" && result.overlay) {
      await tx
        .insert(skill_learn_state)
        .values({
          org_id: input.orgId,
          skill_name: input.skillName,
          enabled: true,
          current_base_hash: result.overlay.baseHash,
          current_learned_hash: learnedBodyHash(result.overlay.body),
        })
        .onConflictDoUpdate({
          target: [skill_learn_state.org_id, skill_learn_state.skill_name],
          set: {
            current_base_hash: result.overlay.baseHash,
            current_learned_hash: learnedBodyHash(result.overlay.body),
            updated_at: new Date(),
          },
        });
      await recordConfigChange({
        workspaceRoot: orgRoot,
        paths: [`skill-overlays/${input.skillName}`],
        message: `Learned overlay: ${input.skillName}`,
      });
    }

    return result;
  });
}
