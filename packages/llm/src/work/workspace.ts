import { createHash } from "node:crypto";
import type { Dirent } from "node:fs";
import {
  cp,
  lstat,
  mkdir,
  mkdtemp,
  readdir,
  readFile,
  readlink,
  rm,
  symlink,
  writeFile,
} from "node:fs/promises";
import { constants as fsConstants, existsSync } from "node:fs";
import { access } from "node:fs/promises";
import { homedir, tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import type { AgentWorkspace } from "../agent-backend";
import {
  composeSkillTree,
  overlayAppliesToBase,
  readLearnedOverlay,
} from "./skill-overlay";

export function resolveBuiltinSkillsRoot(
  moduleUrl = import.meta.url,
  configuredRoot = process.env.OPENNEKO_BUILTIN_SKILLS_ROOT,
  pathExists: (candidate: string) => boolean = existsSync,
): string {
  if (configuredRoot?.trim()) return resolve(configuredRoot);

  const moduleDir = dirname(fileURLToPath(moduleUrl));
  // esbuild collapses this module into /app/agent-entry.js in the sandbox
  // image, alongside assets/. Source and server builds retain the package
  // layout and use the second candidate.
  const bundled = resolve(moduleDir, "assets", "builtin-skills");
  if (pathExists(bundled)) return bundled;
  return resolve(
    // Without the tracer hint, Next treats the dynamic package-relative read
    // as a request to include the entire monorepo in every server route's NFT
    // closure.
    /* turbopackIgnore: true */ moduleDir,
    "..",
    "..",
    "assets",
    "builtin-skills",
  );
}

export const SKILL_ORIGIN_FILE = ".openneko-origin";

export type SkillOriginRecord = {
  kind: "builtin";
  sourceHash: string;
};

const BUILTIN_SKILLS_ROOT = resolveBuiltinSkillsRoot();

export function getBuiltinSkillsRoot(): string {
  return BUILTIN_SKILLS_ROOT;
}

function useHostWebDevelopmentHome(): boolean {
  return (
    process.env.OPENNEKO_HOST_WEB_DEV === "1" &&
    process.env.NODE_ENV === "development" &&
    process.env.OPENNEKO_STACK_MODE !== "demo" &&
    process.env.NEXT_PUBLIC_DEMO !== "true" &&
    process.env.DEMO !== "true"
  );
}

function getHome(): string {
  const developmentHome = useHostWebDevelopmentHome()
    ? process.env.OPENNEKO_AGENT_HOME?.trim()
    : undefined;
  return developmentHome || process.env.HOME || homedir();
}

function safeSegment(raw: string): string {
  return raw.replace(/[^a-zA-Z0-9._-]/g, "_");
}

export function getOrgAgentRoot(orgId: string): string {
  return join(
    getHome(),
    ".config",
    "openneko",
    "agents",
    "orgs",
    safeSegment(orgId),
  );
}

export type OrgWorkspaceRoots = Omit<
  AgentWorkspace,
  "threadUploadsRoot" | "runRoot" | "artifactRoot" | "binRoot"
>;

export async function ensureOrgWorkspace(orgId: string): Promise<OrgWorkspaceRoots> {
  const orgRoot = getOrgAgentRoot(orgId);
  const skillsRoot = join(orgRoot, "skills");
  const memoryRoot = join(orgRoot, "memory");
  const knowledgeRoot = join(orgRoot, "knowledge");
  const uploadsRoot = join(orgRoot, "uploads");
  const runsRoot = join(orgRoot, "runs");
  const skillOverlaysRoot = join(orgRoot, "skill-overlays");

  for (const dir of [
    orgRoot,
    skillsRoot,
    memoryRoot,
    knowledgeRoot,
    uploadsRoot,
    runsRoot,
    skillOverlaysRoot,
  ]) {
    await mkdir(dir, { recursive: true });
  }

  await materializeBuiltinSkills(skillsRoot);
  await ensureKnowledgeFiles(knowledgeRoot);

  return {
    orgRoot,
    skillsRoot,
    memoryRoot,
    knowledgeRoot,
    uploadsRoot,
    runsRoot,
  };
}

export async function ensureWorkWorkspace(
  orgId: string,
  threadId: string,
  runId: string,
): Promise<AgentWorkspace> {
  const base = await ensureOrgWorkspace(orgId);
  const threadUploadsRoot = join(base.uploadsRoot, safeSegment(threadId));
  const runRoot = join(base.runsRoot, safeSegment(runId));
  const artifactRoot = join(runRoot, "artifacts");
  const binRoot = join(runRoot, "bin");

  for (const dir of [threadUploadsRoot, runRoot, artifactRoot, binRoot]) {
    await mkdir(dir, { recursive: true });
  }

  return {
    ...base,
    threadUploadsRoot,
    runRoot,
    artifactRoot,
    binRoot,
  };
}

/**
 * A throw-away workspace for non-interactive agent jobs.
 *
 * Job agents must never receive the durable org workspace: it can contain
 * member memories, uploads, prior run state, and client credentials from
 * unrelated turns. The launcher stages this empty tree instead,
 * then the caller exposes only the current run's explicit brokered
 * capabilities. Source endpoints and service credentials stay host-side.
 */
export async function ensureIsolatedJobWorkspace(
  label: string,
): Promise<{ workspace: AgentWorkspace; cleanup: () => Promise<void> }> {
  const safeLabel = safeSegment(label).slice(0, 48) || "job";
  const orgRoot = await mkdtemp(
    join(tmpdir(), `openneko-agent-${safeLabel}-`),
  );
  const skillsRoot = join(orgRoot, "skills");
  const memoryRoot = join(orgRoot, "memory");
  const knowledgeRoot = join(orgRoot, "knowledge");
  const uploadsRoot = join(orgRoot, "uploads");
  const runsRoot = join(orgRoot, "runs");
  const threadUploadsRoot = join(uploadsRoot, "job");
  const runRoot = join(runsRoot, "current");
  const artifactRoot = join(runRoot, "artifacts");
  const binRoot = join(runRoot, "bin");

  for (const dir of [
    skillsRoot,
    memoryRoot,
    knowledgeRoot,
    uploadsRoot,
    runsRoot,
    threadUploadsRoot,
    runRoot,
    artifactRoot,
    binRoot,
  ]) {
    await mkdir(dir, { recursive: true });
  }

  const workspace: AgentWorkspace = {
    orgRoot,
    skillsRoot,
    memoryRoot,
    knowledgeRoot,
    uploadsRoot,
    runsRoot,
    threadUploadsRoot,
    runRoot,
    artifactRoot,
    binRoot,
  };

  return {
    workspace,
    cleanup: async () => {
      await rm(orgRoot, { recursive: true, force: true });
    },
  };
}

export async function listSkillNames(skillsRoot: string): Promise<string[]> {
  try {
    const entries = await readdir(skillsRoot, { withFileTypes: true });
    return entries
      .filter((entry) => entry.isDirectory())
      .map((entry) => entry.name)
      .sort();
  } catch {
    return [];
  }
}

export type InstalledSkill = { name: string; description: string };

// Parse SKILL.md frontmatter from each skill dir. Used to build a catalog for
// the agent prompt.
export async function listInstalledSkills(
  skillsRoot: string,
): Promise<InstalledSkill[]> {
  let names: string[];
  try {
    const entries = await readdir(skillsRoot, { withFileTypes: true });
    names = entries.filter((e) => e.isDirectory()).map((e) => e.name).sort();
  } catch {
    return [];
  }
  const out: InstalledSkill[] = [];
  for (const name of names) {
    try {
      const md = await readFile(join(skillsRoot, name, "SKILL.md"), "utf8");
      const match = md.match(/^---\s*\n([\s\S]*?)\n---/);
      if (!match) continue;
      const fm = match[1];
      const descMatch = fm.match(/^description:\s*(.+)$/m);
      const description = descMatch ? descMatch[1].trim() : "";
      out.push({ name, description });
    } catch {
      // skill dir without readable SKILL.md — skip
    }
  }
  return out;
}

/**
 * Make image-baked skills available at the workspace path agents see.
 * Existing directories win, so an organization-created skill or a modified
 * built-in skill can override the image copy by name.
 */
export async function materializeBuiltinSkills(
  skillsRoot: string,
  allowedSkills?: readonly string[],
): Promise<void> {
  await mkdir(skillsRoot, { recursive: true });
  await seedBuiltinSkills(skillsRoot, allowedSkills);
}

const builtinSkillFingerprints = new Map<string, Promise<string>>();

export async function fingerprintSkillTree(root: string): Promise<string> {
  const hash = createHash("sha256");
  const visit = async (dir: string, prefix: string): Promise<void> => {
    const entries = await readdir(dir, { withFileTypes: true });
    entries.sort((a, b) => a.name.localeCompare(b.name));
    for (const entry of entries) {
      if (!prefix && entry.name === SKILL_ORIGIN_FILE) continue;
      const rel = prefix ? `${prefix}/${entry.name}` : entry.name;
      const absolute = join(dir, entry.name);
      if (entry.isDirectory()) {
        hash.update(`d:${rel}\0`);
        await visit(absolute, rel);
      } else if (entry.isSymbolicLink()) {
        hash.update(`l:${rel}\0${await readlink(absolute)}\0`);
      } else if (entry.isFile()) {
        hash.update(`f:${rel}\0`);
        hash.update(await readFile(absolute));
        hash.update("\0");
      }
    }
  };
  await visit(root, "");
  return hash.digest("hex");
}

export async function readSkillOrigin(
  skillDir: string,
): Promise<SkillOriginRecord | null> {
  try {
    const raw = await readFile(join(skillDir, SKILL_ORIGIN_FILE), "utf8");
    const parsed = JSON.parse(raw) as Partial<SkillOriginRecord>;
    if (parsed.kind !== "builtin") return null;
    if (typeof parsed.sourceHash !== "string" || !parsed.sourceHash) return null;
    return { kind: "builtin", sourceHash: parsed.sourceHash };
  } catch {
    return null;
  }
}

async function writeBuiltinOrigin(
  skillDir: string,
  sourceHash: string,
): Promise<void> {
  await writeFile(
    join(skillDir, SKILL_ORIGIN_FILE),
    `${JSON.stringify({ kind: "builtin", sourceHash } satisfies SkillOriginRecord)}\n`,
    "utf8",
  );
}

function builtinFingerprint(name: string, root: string): Promise<string> {
  let pending = builtinSkillFingerprints.get(name);
  if (!pending) {
    pending = fingerprintSkillTree(root);
    builtinSkillFingerprints.set(name, pending);
  }
  return pending;
}

/** Whether a workspace skill still matches the built-in copy it was seeded from. */
async function isUnmodifiedBuiltin(skillDir: string, name: string): Promise<boolean> {
  try {
    const workspaceHash = await fingerprintSkillTree(skillDir);
    const origin = await readSkillOrigin(skillDir);
    if (origin?.kind === "builtin") return origin.sourceHash === workspaceHash;
    return workspaceHash === (await builtinFingerprint(name, join(BUILTIN_SKILLS_ROOT, name)));
  } catch {
    return false;
  }
}

/** Built-in skills in a workspace that nobody has changed. */
export async function unmodifiedBuiltinSkills(skillsRoot: string): Promise<string[]> {
  const builtins = await readdir(BUILTIN_SKILLS_ROOT, { withFileTypes: true }).catch(() => [] as Dirent[]);
  const names: string[] = [];
  for (const entry of builtins) {
    if (entry.isDirectory() && (await isUnmodifiedBuiltin(join(skillsRoot, entry.name), entry.name))) names.push(entry.name);
  }
  return names.sort();
}

/**
 * Copy only skills that the agent image cannot reconstruct itself: custom
 * organization skills and locally modified built-ins. Full skill bodies stay
 * off the per-turn transfer for the common, unmodified built-in case. A
 * security-required system skill is copied from the current bundled source,
 * so an old durable workspace cannot pin its behavior across upgrades.
 */
export async function copySkillOverrides(
  skillsRoot: string,
  destinationRoot: string,
  forceNames: readonly string[] = [],
  allowedSkills?: readonly string[],
): Promise<string[]> {
  await mkdir(destinationRoot, { recursive: true });
  const forced = new Set(forceNames);
  const allowed = allowedSkills ? new Set([...allowedSkills, ...forceNames]) : null;
  let entries: Dirent[];
  try {
    entries = await readdir(skillsRoot, { withFileTypes: true });
  } catch {
    return [];
  }

  const copied: string[] = [];
  for (const entry of entries.sort((a, b) => a.name.localeCompare(b.name))) {
    if (!entry.isDirectory()) continue;
    if (allowed && !allowed.has(entry.name)) continue;
    const workspaceSource = join(skillsRoot, entry.name);
    const bundled = join(BUILTIN_SKILLS_ROOT, entry.name);
    const source = forced.has(entry.name) ? bundled : workspaceSource;
    let isUnmodifiedBuiltin = false;
    let workspaceHash = "";
    if (!forced.has(entry.name)) {
      try {
        workspaceHash = await fingerprintSkillTree(workspaceSource);
        const imageHash = await builtinFingerprint(entry.name, bundled);
        const origin = await readSkillOrigin(workspaceSource);
        if (origin?.kind === "builtin" && origin.sourceHash === workspaceHash) {
          // Unchanged since seed, including a stale image copy.
          isUnmodifiedBuiltin = true;
        } else if (!origin) {
          isUnmodifiedBuiltin = workspaceHash === imageHash;
        }
      } catch {
        // A missing bundled directory identifies an organization-created skill.
      }
    }
    const orgRoot = dirname(skillsRoot);
    const overlay = await readLearnedOverlay(orgRoot, entry.name);
    const overlayLive = await overlayAppliesToBase(
      overlay,
      workspaceHash || (await fingerprintSkillTree(workspaceSource).catch(() => "")),
    );
    if (isUnmodifiedBuiltin && !overlayLive) continue;

    const destination = join(destinationRoot, entry.name);
    const composeBase = isUnmodifiedBuiltin ? bundled : source;
    await composeSkillTree({
      baseDir: composeBase,
      destDir: destination,
      overlay: overlayLive ? overlay : null,
    });
    copied.push(entry.name);
  }
  const missing = [...forced].filter((name) => !copied.includes(name));
  if (missing.length > 0) {
    throw new Error(`required sandbox skill is unavailable: ${missing.sort().join(", ")}`);
  }
  return copied;
}

async function seedBuiltinSkills(skillsRoot: string, allowedSkills?: readonly string[]): Promise<void> {
  const allowed = allowedSkills ? new Set(allowedSkills) : null;
  const entries = await readdir(BUILTIN_SKILLS_ROOT, { withFileTypes: true });
  for (const entry of entries) {
    if (!entry.isDirectory()) continue;
    if (allowed && !allowed.has(entry.name)) continue;
    const dest = join(skillsRoot, entry.name);
    try {
      await access(dest, fsConstants.F_OK);
      continue;
    } catch {
      const source = join(BUILTIN_SKILLS_ROOT, entry.name);
      await cp(source, dest, {
        recursive: true,
        errorOnExist: false,
      });
      const sourceHash = await builtinFingerprint(entry.name, source);
      await writeBuiltinOrigin(dest, sourceHash);
    }
  }
}

async function ensureKnowledgeFiles(root: string): Promise<void> {
  const files: Array<[string, string]> = [
    [
      join(root, "INDEX.md"),
      [
        "# OpenNeko Knowledge",
        "",
        "This directory is reserved for org-specific durable knowledge files.",
        "Use these paths when you need reusable facts beyond the active chat.",
        "",
        "- schema.json",
        "- insights.json",
        "- syntax.json",
      ].join("\n"),
    ],
    [join(root, "schema.json"), "{}\n"],
    [join(root, "insights.json"), "{}\n"],
    [join(root, "syntax.json"), "{}\n"],
  ];

  for (const [path, content] of files) {
    try {
      await access(path, fsConstants.F_OK);
    } catch {
      await writeFile(path, content, "utf8");
    }
  }
}

async function ensureLink(linkPath: string, target: string): Promise<void> {
  await mkdir(dirname(linkPath), { recursive: true });

  try {
    const stat = await lstat(linkPath);
    if (stat.isSymbolicLink()) return;
    return;
  } catch {
    // create below
  }

  await symlink(target, linkPath, "dir");
}
