import { createHash } from "node:crypto";
import { join } from "node:path";
import { assertAdditiveLearnedBody } from "./skill-learn-scan";
import {
  readLearnedOverlay,
  writeLearnedOverlay,
  type LearnedOverlay,
} from "./skill-overlay";
import { proposeSkillLearn } from "./skill-learn-propose";
import { fingerprintSkillTree } from "./workspace";

export const DEFAULT_MIN_REPEATS = 5;
export const DEFAULT_SETTLEMENT_MS = 2 * 60 * 60 * 1000;

export type SkillLearnDecision =
  | "applied"
  | "skipped"
  | "stale"
  | "rejected";

export type SkillLearnProposal = {
  lesson: string;
  rationale: string;
  learnedMarkdown: string;
  evidenceRunIds: string[];
};

export type SkillLearnTrace = {
  lesson?: string;
  rationale?: string;
  evidenceRunIds?: string[];
  contentHash?: string;
  baseHash?: string;
  tokenUsage?: { input?: number; output?: number };
  skipReason?: string;
};

export type SkillLearnResult = {
  decision: SkillLearnDecision;
  reason: string;
  overlay?: LearnedOverlay;
  trace: SkillLearnTrace;
  diff?: string;
};

export type SkillUsageSnapshot = {
  runId: string;
  skillName: string;
  contentHash: string;
  createdAt: Date;
};

export type SkillLearnContext = {
  orgId: string;
  orgRoot: string;
  skillName: string;
  orgEnabled: boolean;
  skillEnabled: boolean;
  usages: SkillUsageSnapshot[];
  now?: Date;
  minRepeats?: number;
  settlementMs?: number;
  currentBaseHash?: string;
  propose?: (input: {
    skillName: string;
    baseHash: string;
    usages: SkillUsageSnapshot[];
  }) => Promise<SkillLearnProposal | null>;
  llm?: import("./skill-learn-propose").SkillLearnLlm;
};

export async function runSkillLearn(
  ctx: SkillLearnContext,
): Promise<SkillLearnResult> {
  const now = ctx.now ?? new Date();
  const minRepeats = ctx.minRepeats ?? DEFAULT_MIN_REPEATS;
  const settlementMs = ctx.settlementMs ?? DEFAULT_SETTLEMENT_MS;
  const skillDir = join(ctx.orgRoot, "skills", ctx.skillName);

  if (!ctx.orgEnabled || !ctx.skillEnabled) {
    return skip("flags_off", { skipReason: "org or skill learning is off" });
  }

  const settled = ctx.usages.filter(
    (usage) => now.getTime() - usage.createdAt.getTime() >= settlementMs,
  );
  if (settled.length < minRepeats) {
    return skip("below_repeat_threshold", {
      skipReason: `settled ${settled.length} < ${minRepeats}`,
      evidenceRunIds: settled.map((usage) => usage.runId),
    });
  }

  const byHash = new Map<string, SkillUsageSnapshot[]>();
  for (const usage of settled) {
    const list = byHash.get(usage.contentHash) ?? [];
    list.push(usage);
    byHash.set(usage.contentHash, list);
  }
  const cohort = [...byHash.values()].sort(
    (a, b) => b.length - a.length,
  )[0];
  if (!cohort || cohort.length < minRepeats) {
    return skip("below_repeat_threshold", {
      skipReason: "no content_hash cohort reached the repeat threshold",
      evidenceRunIds: settled.map((usage) => usage.runId),
    });
  }

  let baseHash: string;
  try {
    baseHash = ctx.currentBaseHash ?? (await fingerprintSkillTree(skillDir));
  } catch {
    return skip("missing_skill", { skipReason: "skill directory is missing" });
  }

  if (ctx.currentBaseHash && ctx.currentBaseHash !== baseHash) {
    return {
      decision: "stale",
      reason: "base_hash mismatch",
      trace: { baseHash, contentHash: cohort[0]?.contentHash },
    };
  }

  const proposal = ctx.propose
    ? await ctx.propose({
        skillName: ctx.skillName,
        baseHash,
        usages: cohort,
      })
    : await proposeSkillLearn({
        orgId: ctx.orgId,
        orgRoot: ctx.orgRoot,
        skillName: ctx.skillName,
        baseHash,
        usages: cohort,
        llm: ctx.llm,
      });
  if (!proposal) {
    return skip("no_proposal", {
      baseHash,
      contentHash: cohort[0]?.contentHash,
      evidenceRunIds: cohort.map((usage) => usage.runId),
      skipReason: "proposer returned no lesson",
    });
  }

  const scanError = assertAdditiveLearnedBody(proposal.learnedMarkdown);
  if (scanError) {
    return {
      decision: "rejected",
      reason: scanError,
      trace: {
        lesson: proposal.lesson,
        rationale: proposal.rationale,
        evidenceRunIds: proposal.evidenceRunIds,
        baseHash,
        contentHash: cohort[0]?.contentHash,
        skipReason: scanError,
      },
    };
  }

  const existing = await readLearnedOverlay(ctx.orgRoot, ctx.skillName);
  if (existing && existing.baseHash !== baseHash) {
    return {
      decision: "stale",
      reason: "existing overlay base_hash does not match current base",
      trace: { baseHash, contentHash: cohort[0]?.contentHash },
    };
  }

  const overlay: LearnedOverlay = {
    skillName: ctx.skillName,
    baseHash,
    status: "applied",
    body: proposal.learnedMarkdown.trim(),
  };
  await writeLearnedOverlay(ctx.orgRoot, overlay);
  const diff = learnedDiff(existing?.body ?? "", overlay.body);
  return {
    decision: "applied",
    reason: "applied additive learned guidance",
    overlay,
    diff,
    trace: {
      lesson: proposal.lesson,
      rationale: proposal.rationale,
      evidenceRunIds: proposal.evidenceRunIds,
      baseHash,
      contentHash: cohort[0]?.contentHash,
    },
  };
}

export function learnedBodyHash(body: string): string {
  return createHash("sha256").update(body).digest("hex");
}

function skip(reason: string, trace: SkillLearnTrace): SkillLearnResult {
  return { decision: "skipped", reason, trace };
}

function learnedDiff(before: string, after: string): string {
  return `--- before\n${before}\n+++ after\n${after}\n`;
}
