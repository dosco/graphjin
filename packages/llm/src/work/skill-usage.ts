import {
  and,
  db,
  eq,
  ne,
  pack_artifact,
  pack_install,
  skill_usage,
} from "@neko/db";
import { join } from "node:path";
import type { AgentEvent } from "../agent-backend";
import { readConfigHead } from "../config-vcs";
import { appendWorkRunEvent } from "./store";
import { fingerprintEffectiveSkill } from "./skill-overlay";
import { getOrgAgentRoot, readSkillOrigin } from "./workspace";

const SKILL_NAME_RE = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;
const SKILL_MD_PATH_RE =
  /(?:^|\/)skills\/([a-z0-9]+(?:-[a-z0-9]+)*)\/SKILL\.md(?:$|[?#])/i;

export type SkillUseSource = "read";
export type SkillOriginKind = "builtin" | "custom" | "pack";

export type DetectedSkillUse = {
  name: string;
  source: SkillUseSource;
};

export function detectSkillUse(event: AgentEvent): DetectedSkillUse | null {
  if (event.type !== "tool_start") return null;

  const fromPath = skillNameFromPaths(collectStrings(event.input));
  if (fromPath) return { name: fromPath, source: "read" };
  return null;
}

export async function recordSkillUsageFromEvent(input: {
  orgId: string;
  threadId: string;
  runId: string;
  event: AgentEvent;
  triggeringEventId: number;
}): Promise<void> {
  const detected = detectSkillUse(input.event);
  if (!detected) return;

  try {
    await persistSkillUsage({
      orgId: input.orgId,
      threadId: input.threadId,
      runId: input.runId,
      detected,
      firstEventId: input.triggeringEventId,
    });
  } catch (error) {
    console.warn(
      `[skill-usage] failed to record ${detected.name} for run ${input.runId}: ${
        error instanceof Error ? error.message : error
      }`,
    );
  }
}

async function persistSkillUsage(input: {
  orgId: string;
  threadId: string;
  runId: string;
  detected: DetectedSkillUse;
  firstEventId: number;
}): Promise<void> {
  const orgRoot = getOrgAgentRoot(input.orgId);
  const skillDir = join(orgRoot, "skills", input.detected.name);
  const [contentHash, pack, configCommitSha, originHint] = await Promise.all([
    fingerprintEffectiveSkill(orgRoot, input.detected.name, skillDir).catch(
      () => null,
    ),
    lookupPackSkill(input.orgId, input.detected.name),
    readConfigHead(orgRoot),
    readSkillOrigin(skillDir),
  ]);
  if (!contentHash) return;

  const origin: SkillOriginKind = pack
    ? "pack"
    : originHint?.kind === "builtin"
      ? "builtin"
      : "custom";

  const used: Extract<AgentEvent, { type: "skill_used" }> = {
    type: "skill_used",
    name: input.detected.name,
    source: input.detected.source,
    contentHash,
    origin,
    firstEventId: input.firstEventId,
    ...(pack ? { packId: pack.packId, packVersion: pack.version } : {}),
    ...(configCommitSha ? { configCommitSha } : { configCommitSha: null }),
  };

  const inserted = await db()
    .insert(skill_usage)
    .values({
      org_id: input.orgId,
      run_id: input.runId,
      skill_name: input.detected.name,
      content_hash: contentHash,
      origin,
      pack_id: pack?.packId ?? null,
      pack_version: pack?.version ?? null,
      config_commit_sha: configCommitSha,
      source: input.detected.source,
      first_event_id: input.firstEventId,
      attempt: 1,
    })
    .onConflictDoNothing({
      target: [skill_usage.run_id, skill_usage.skill_name],
    })
    .returning({ id: skill_usage.id });

  if (inserted.length === 0) return;

  await appendWorkRunEvent({
    orgId: input.orgId,
    threadId: input.threadId,
    runId: input.runId,
    event: used,
  });
}

async function lookupPackSkill(
  orgId: string,
  skillName: string,
): Promise<{ packId: string; version: string } | null> {
  const rows = await db()
    .select({
      packId: pack_install.pack_id,
      version: pack_install.version,
    })
    .from(pack_artifact)
    .innerJoin(pack_install, eq(pack_artifact.pack_install_id, pack_install.id))
    .where(
      and(
        eq(pack_artifact.org_id, orgId),
        eq(pack_artifact.artifact_kind, "skill"),
        eq(pack_artifact.target_ref, skillName),
        ne(pack_install.status, "removed"),
      ),
    )
    .limit(1);
  const row = rows[0];
  if (!row) return null;
  return { packId: row.packId, version: row.version };
}

function skillNameFromPaths(values: string[]): string | null {
  for (const value of values) {
    const normalized = value.replace(/\\/g, "/");
    const match = SKILL_MD_PATH_RE.exec(normalized);
    if (match?.[1] && SKILL_NAME_RE.test(match[1])) return match[1];
  }
  return null;
}

function collectStrings(value: unknown, depth = 0): string[] {
  if (depth > 6 || value == null) return [];
  if (typeof value === "string") return [value];
  if (Array.isArray(value)) {
    return value.flatMap((item) => collectStrings(item, depth + 1));
  }
  if (typeof value === "object") {
    return Object.values(value as Record<string, unknown>).flatMap((item) =>
      collectStrings(item, depth + 1),
    );
  }
  return [];
}
