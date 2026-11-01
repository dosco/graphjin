import { mkdir, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";

const SKILL_NAME_RE = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

export type WorkSkillDraft = {
  name: string;
  description: string;
  body: string;
  license?: string;
  compatibility?: string;
  metadata?: Record<string, string>;
  allowedTools?: string;
  files?: Array<{ path: string; content: string }>;
};

export function normalizeSkillName(input: string): string {
  return input
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-|-$/g, "")
    .slice(0, 64)
    .replace(/-$/g, "");
}

export function upsertWorkSkill(skillsRoot: string, draft: WorkSkillDraft): {
  name: string;
  skillPath: string;
} {
  const name = normalizeSkillName(draft.name);
  if (!name || !SKILL_NAME_RE.test(name)) {
    throw new Error("Skill name must use lowercase letters, numbers, and hyphens only.");
  }
  if (!draft.description.trim()) throw new Error("Skill description is required.");
  if (!draft.body.trim()) throw new Error("Skill body is required.");

  const skillPath = resolve(skillsRoot, name);
  if (!skillPath.startsWith(resolve(skillsRoot))) {
    throw new Error("Resolved skill path escapes the skills root.");
  }
  return { name, skillPath };
}

/** Write skill files only; callers outside the sandbox may add versioning. */
export async function writeWorkSkillFiles(
  skillsRoot: string,
  draft: WorkSkillDraft,
): Promise<{ name: string; skillPath: string }> {
  const { name, skillPath } = upsertWorkSkill(skillsRoot, draft);
  await mkdir(skillPath, { recursive: true });
  await writeFile(
    resolve(skillPath, "SKILL.md"),
    renderSkillMarkdown({
      ...draft,
      name,
      description: draft.description.trim(),
      body: draft.body.trim(),
    }),
    "utf8",
  );

  for (const file of draft.files ?? []) {
    const rel = normalizeRelativePath(file.path);
    const full = resolve(skillPath, rel);
    if (!full.startsWith(skillPath)) {
      throw new Error(`Skill file escapes the skill root: ${file.path}`);
    }
    await mkdir(dirname(full), { recursive: true });
    await writeFile(full, file.content, "utf8");
  }

  return { name, skillPath };
}

function renderSkillMarkdown(draft: WorkSkillDraft & { name: string }): string {
  const lines = [
    "---",
    `name: ${draft.name}`,
    `description: ${yamlScalar(draft.description.trim())}`,
  ];

  if (draft.license?.trim()) lines.push(`license: ${yamlScalar(draft.license.trim())}`);
  if (draft.compatibility?.trim()) {
    lines.push(`compatibility: ${yamlScalar(draft.compatibility.trim())}`);
  }
  if (draft.metadata && Object.keys(draft.metadata).length > 0) {
    lines.push("metadata:");
    for (const [key, value] of Object.entries(draft.metadata)) {
      lines.push(`  ${yamlKey(key)}: ${yamlScalar(value)}`);
    }
  }
  if (draft.allowedTools?.trim()) {
    lines.push(`allowed-tools: ${yamlScalar(draft.allowedTools.trim())}`);
  }

  return `${lines.join("\n")}\n---\n\n${draft.body.trim()}\n`;
}

function normalizeRelativePath(input: string): string {
  const rel = input.trim().replace(/\\/g, "/").replace(/^\/+/, "");
  if (!rel) throw new Error("Skill file path is required.");
  const parts = rel.split("/");
  for (const part of parts) {
    if (!part || part === "." || part === "..") {
      throw new Error(`Invalid skill file path: ${input}`);
    }
  }
  return parts.join("/");
}

function yamlScalar(value: string): string {
  return JSON.stringify(value);
}

function yamlKey(value: string): string {
  return value.replace(/[^a-zA-Z0-9_-]/g, "_");
}
