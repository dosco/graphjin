import { createHash } from "node:crypto";
import { cp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { fingerprintSkillTree } from "./workspace";

export const SKILL_OVERLAYS_DIR = "skill-overlays";
export const LEARNED_FILE = "LEARNED.md";
export const LEARNED_START = "<!-- openneko-learned:start -->";
export const LEARNED_END = "<!-- openneko-learned:end -->";

export type LearnedOverlayStatus = "applied" | "stale" | "disabled";

export type LearnedOverlay = {
  skillName: string;
  baseHash: string;
  status: LearnedOverlayStatus;
  learnEventId?: string;
  body: string;
};

export function skillOverlayDir(orgRoot: string, skillName: string): string {
  return join(orgRoot, SKILL_OVERLAYS_DIR, skillName);
}

export async function readLearnedOverlay(
  orgRoot: string,
  skillName: string,
): Promise<LearnedOverlay | null> {
  try {
    const raw = await readFile(
      join(skillOverlayDir(orgRoot, skillName), LEARNED_FILE),
      "utf8",
    );
    return parseLearnedMarkdown(raw, skillName);
  } catch {
    return null;
  }
}

export async function writeLearnedOverlay(
  orgRoot: string,
  overlay: LearnedOverlay,
): Promise<string> {
  const dir = skillOverlayDir(orgRoot, overlay.skillName);
  await mkdir(dir, { recursive: true });
  const path = join(dir, LEARNED_FILE);
  await writeFile(path, renderLearnedMarkdown(overlay), "utf8");
  return path;
}

export function stripLearnedSection(skillMarkdown: string): string {
  const start = skillMarkdown.indexOf(LEARNED_START);
  if (start < 0) return skillMarkdown.trimEnd();
  const end = skillMarkdown.indexOf(LEARNED_END, start);
  if (end < 0) return skillMarkdown.slice(0, start).trimEnd();
  return `${skillMarkdown.slice(0, start).trimEnd()}\n${skillMarkdown
    .slice(end + LEARNED_END.length)
    .trimStart()}`.trimEnd();
}

export function appendLearnedSection(skillMarkdown: string, body: string): string {
  const base = stripLearnedSection(skillMarkdown).trimEnd();
  const guidance = body.trim();
  if (!guidance) return base;
  return `${base}\n\n${LEARNED_START}\n## Learned guidance\n\n${guidance}\n${LEARNED_END}\n`;
}

export async function overlayAppliesToBase(
  overlay: LearnedOverlay | null,
  baseHash: string,
): Promise<boolean> {
  if (!overlay) return false;
  if (overlay.status === "disabled") return false;
  return overlay.status === "applied" && overlay.baseHash === baseHash;
}

export async function fingerprintEffectiveSkill(
  orgRoot: string,
  skillName: string,
  baseDir: string,
): Promise<string> {
  const baseHash = await fingerprintSkillTree(baseDir);
  const overlay = await readLearnedOverlay(orgRoot, skillName);
  if (!(await overlayAppliesToBase(overlay, baseHash))) return baseHash;
  return createHash("sha256")
    .update(baseHash)
    .update("\0")
    .update(overlay!.body)
    .digest("hex");
}

export async function composeSkillTree(input: {
  baseDir: string;
  destDir: string;
  overlay: LearnedOverlay | null;
}): Promise<"applied" | "base-only"> {
  await rm(input.destDir, { recursive: true, force: true });
  await mkdir(input.destDir, { recursive: true });
  await cp(input.baseDir, input.destDir, { recursive: true, force: true });
  const baseHash = await fingerprintSkillTree(input.baseDir);
  if (!(await overlayAppliesToBase(input.overlay, baseHash))) return "base-only";
  const skillPath = join(input.destDir, "SKILL.md");
  const current = await readFile(skillPath, "utf8");
  await writeFile(
    skillPath,
    appendLearnedSection(current, input.overlay!.body),
    "utf8",
  );
  return "applied";
}

function parseLearnedMarkdown(
  raw: string,
  fallbackName: string,
): LearnedOverlay | null {
  const match = raw.match(/^---\s*\n([\s\S]*?)\n---\s*\n?([\s\S]*)$/);
  if (!match) return null;
  const fm = match[1] ?? "";
  const body = (match[2] ?? "").trim();
  const baseHash = scalar(fm, "base_hash");
  if (!baseHash) return null;
  const status = scalar(fm, "status") ?? "applied";
  if (status !== "applied" && status !== "stale" && status !== "disabled") {
    return null;
  }
  return {
    skillName: scalar(fm, "skill_name") || fallbackName,
    baseHash,
    status,
    ...(scalar(fm, "learn_event_id")
      ? { learnEventId: scalar(fm, "learn_event_id") }
      : {}),
    body,
  };
}

function renderLearnedMarkdown(overlay: LearnedOverlay): string {
  const lines = [
    "---",
    `skill_name: ${overlay.skillName}`,
    `base_hash: ${overlay.baseHash}`,
    `status: ${overlay.status}`,
  ];
  if (overlay.learnEventId) lines.push(`learn_event_id: ${overlay.learnEventId}`);
  lines.push("---", "", overlay.body.trim(), "");
  return lines.join("\n");
}

function scalar(frontmatter: string, key: string): string | undefined {
  const match = frontmatter.match(new RegExp(`^${key}:\\s*(.+)$`, "m"));
  return match?.[1]?.trim();
}
