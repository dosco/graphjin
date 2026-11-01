import { readFile } from "node:fs/promises";
import { join } from "node:path";
import type { SkillLearnProposal, SkillUsageSnapshot } from "./skill-learn";

export type SkillLearnLlm = (prompt: string) => Promise<string>;

const PROPOSAL_SHAPE = `{
  "lesson": "one repeated procedure, short",
  "rationale": "why this lesson, citing run ids",
  "learnedMarkdown": "additive guidance only. no frontmatter, no secrets, no Boundary: line",
  "evidenceRunIds": ["run-id", "run-id"]
}`;

export function parseSkillLearnProposal(
  raw: string,
  allowedRunIds: readonly string[],
): SkillLearnProposal | null {
  const json = extractJsonObject(raw);
  if (!json) return null;
  const lesson = stringField(json, "lesson");
  const rationale = stringField(json, "rationale");
  const learnedMarkdown = stringField(json, "learnedMarkdown");
  const evidence = json.evidenceRunIds;
  if (!lesson || !rationale || !learnedMarkdown) return null;
  if (!Array.isArray(evidence) || evidence.length === 0) return null;
  const allowed = new Set(allowedRunIds);
  const evidenceRunIds = evidence
    .filter((id): id is string => typeof id === "string" && allowed.has(id));
  if (evidenceRunIds.length === 0) return null;
  return { lesson, rationale, learnedMarkdown, evidenceRunIds };
}

export async function proposeSkillLearn(input: {
  orgId: string;
  orgRoot: string;
  skillName: string;
  baseHash: string;
  usages: SkillUsageSnapshot[];
  llm?: SkillLearnLlm;
}): Promise<SkillLearnProposal | null> {
  const allowedRunIds = input.usages.map((usage) => usage.runId);
  const skillExcerpt = await readFile(
    join(input.orgRoot, "skills", input.skillName, "SKILL.md"),
    "utf8",
  ).catch(() => "");
  const prompt = [
    "You extract one repeated skill lesson from settled agent runs.",
    "Return only JSON matching this shape:",
    PROPOSAL_SHAPE,
    "Do not call tools. Do not rewrite SKILL.md frontmatter.",
    `skillName: ${input.skillName}`,
    `baseHash: ${input.baseHash}`,
    `runIds: ${JSON.stringify(allowedRunIds)}`,
    "skill excerpt:",
    skillExcerpt.slice(0, 4000),
  ].join("\n");
  const llm = input.llm ?? (await defaultSkillLearnLlm(input.orgId));
  const raw = await llm(prompt);
  return parseSkillLearnProposal(raw, allowedRunIds);
}

async function defaultSkillLearnLlm(orgId: string): Promise<SkillLearnLlm> {
  const [{ ax }, { buildLlm }] = await Promise.all([
    import("@ax-llm/ax"),
    import("../llm"),
  ]);
  const llm = await buildLlm(orgId);
  const generator = ax(
    `learningPrompt:string "instructions plus skill excerpt and run ids" -> skillLearningProposal:string "a JSON object with lesson, rationale, learnedMarkdown, evidenceRunIds"`,
    {
      description:
        "You propose one additive skill lesson. Reply with JSON only. No tools.",
    },
  );
  return async (prompt: string) => {
    const result = await generator.forward(llm, { learningPrompt: prompt });
    return String(result.skillLearningProposal ?? "");
  };
}

function extractJsonObject(raw: string): Record<string, unknown> | null {
  const start = raw.indexOf("{");
  const end = raw.lastIndexOf("}");
  if (start < 0 || end <= start) return null;
  try {
    const parsed = JSON.parse(raw.slice(start, end + 1)) as unknown;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return null;
    return parsed as Record<string, unknown>;
  } catch {
    return null;
  }
}

function stringField(
  value: Record<string, unknown>,
  key: string,
): string | null {
  const field = value[key];
  return typeof field === "string" && field.trim() ? field.trim() : null;
}
