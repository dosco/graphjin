// Lightweight YAML-frontmatter parser for SKILL.md files. We only need
// to extract the agentskills.io spec fields (name, description, license,
// metadata, compatibility, allowed-tools) plus the community
// `prerequisites` extension that the skill-doctor uses to synthesise
// dep entries for community skills installed under ~/.openneko/skills/.
//
// Intentionally NOT a full YAML parser — we tolerate unknown top-level
// keys (some imported skills carry vendor-specific extensions) and
// don't blow up on nested structures we don't recognise. If a key we
// care about can't be parsed cleanly, it's treated as absent.

export interface SkillFrontmatter {
  /** Required by spec. Empty string if missing. */
  name: string;
  /** Required by spec. Empty string if missing. */
  description: string;
  license?: string;
  /** Free-text environment hints. */
  compatibility?: string;
  /** Imported skill metadata retained for round-trip compatibility. */
  allowedTools?: string;
  /** Community extension: prerequisites the skill assumes. */
  prerequisites?: {
    commands?: string[];
    envVars?: string[];
  };
  /** Vendor-namespaced metadata. Plain dict, value strings only. */
  metadata?: Record<string, string>;
  /** Top-level keys we didn't parse — kept for telemetry/future-proofing. */
  unparsed?: string[];
}

const FRONTMATTER_DELIM = "---";

/**
 * Extract the frontmatter block from SKILL.md content. Returns null if
 * the file doesn't open with `---` (treated as a skill with no
 * frontmatter — invalid per spec but we degrade gracefully).
 */
export function extractFrontmatterBlock(content: string): string | null {
  const lines = content.split(/\r?\n/);
  if (lines[0]?.trim() !== FRONTMATTER_DELIM) return null;
  for (let i = 1; i < lines.length; i++) {
    if (lines[i]?.trim() === FRONTMATTER_DELIM) {
      return lines.slice(1, i).join("\n");
    }
  }
  return null;
}

/**
 * Parse the frontmatter into a SkillFrontmatter struct. Unknown keys
 * are recorded in `unparsed[]` so callers can surface "this skill
 * declares fields we don't honour yet" if they care.
 */
export function parseSkillFrontmatter(content: string): SkillFrontmatter {
  const block = extractFrontmatterBlock(content);
  if (!block) {
    return { name: "", description: "" };
  }
  const lines = block.split("\n");
  const result: SkillFrontmatter = { name: "", description: "" };
  const unparsed: string[] = [];

  let i = 0;
  while (i < lines.length) {
    const line = lines[i] ?? "";
    if (!line.trim() || line.trim().startsWith("#")) {
      i++;
      continue;
    }
    // Top-level scalars: `key: value` or `key:` (start of a block).
    const match = /^([A-Za-z_-][A-Za-z0-9_-]*)\s*:\s*(.*)$/.exec(line);
    if (!match) {
      i++;
      continue;
    }
    const [, rawKey, rawValue] = match;
    const key = (rawKey ?? "").trim();
    const value = (rawValue ?? "").trim();

    if (value === "" || value === ">" || value === ">-" || value === "|") {
      // Block scalar (folded or literal) or nested mapping. Collect
      // indented lines that follow.
      const collected = collectIndentedBlock(lines, i + 1);
      const inner = collected.text;
      i = collected.nextIndex;
      if (key === "metadata") {
        result.metadata = parseFlatStringMap(inner);
      } else if (key === "prerequisites") {
        const prereq = parsePrerequisites(inner);
        if (prereq.commands?.length || prereq.envVars?.length) {
          result.prerequisites = prereq;
        }
      } else if (key === "name") {
        result.name = inner.trim();
      } else if (key === "description") {
        result.description = inner.trim();
      } else if (key === "license") {
        result.license = inner.trim();
      } else if (key === "compatibility") {
        result.compatibility = inner.trim();
      } else if (key === "allowed-tools") {
        result.allowedTools = inner.trim();
      } else {
        unparsed.push(key);
      }
      continue;
    }

    const stripped = stripQuotes(value);
    if (key === "name") result.name = stripped;
    else if (key === "description") result.description = stripped;
    else if (key === "license") result.license = stripped;
    else if (key === "compatibility") result.compatibility = stripped;
    else if (key === "allowed-tools") result.allowedTools = stripped;
    else if (key === "prerequisites") {
      // Inline shape `prerequisites: { commands: [foo], env_vars: [BAR] }`.
      result.prerequisites = parseInlinePrerequisites(stripped);
    } else {
      unparsed.push(key);
    }
    i++;
  }

  if (unparsed.length > 0) result.unparsed = unparsed;
  return result;
}

function collectIndentedBlock(
  lines: string[],
  startIndex: number,
): { text: string; nextIndex: number } {
  const collected: string[] = [];
  let i = startIndex;
  for (; i < lines.length; i++) {
    const line = lines[i] ?? "";
    if (line.trim() === "") {
      collected.push("");
      continue;
    }
    if (/^\s/.test(line)) {
      // Indented continuation — preserve the indent so nested parsers
      // can decide what to do with it.
      collected.push(line);
      continue;
    }
    break;
  }
  return { text: collected.join("\n"), nextIndex: i };
}

function parseFlatStringMap(block: string): Record<string, string> {
  // metadata: a flat key: value dict (spec says values must be strings).
  // We tolerate nested keys by joining the path with `.` so e.g.
  // `metadata.openneko.tags` collapses to a single key. Lists become
  // comma-joined string values.
  const out: Record<string, string> = {};
  const stack: string[] = [];
  const lines = block.split("\n");
  let prevIndent = -1;
  for (const line of lines) {
    if (!line.trim()) continue;
    const indent = line.match(/^(\s*)/)?.[1]?.length ?? 0;
    while (stack.length > 0 && indent <= prevIndent) {
      stack.pop();
      prevIndent -= 2;
    }
    const m = /^\s*([A-Za-z_-][A-Za-z0-9_-]*)\s*:\s*(.*)$/.exec(line);
    if (!m) continue;
    const [, rawKey, rawVal] = m;
    const key = (rawKey ?? "").trim();
    const val = (rawVal ?? "").trim();
    const path = [...stack, key].join(".");
    if (val) {
      out[path] = stripQuotes(val);
    } else {
      stack.push(key);
      prevIndent = indent;
    }
  }
  return out;
}

function parsePrerequisites(block: string): { commands?: string[]; envVars?: string[] } {
  // Two shapes community skills use:
  //   prerequisites:
  //     commands: [foo, bar]
  //     env_vars: [API_KEY]
  // or:
  //   prerequisites:
  //     commands:
  //       - foo
  //       - bar
  const out: { commands?: string[]; envVars?: string[] } = {};
  const lines = block.split("\n");
  let cursor: "commands" | "envVars" | null = null;
  for (const line of lines) {
    if (!line.trim()) {
      cursor = null;
      continue;
    }
    const inlineMatch = /^\s*(commands|env_vars)\s*:\s*\[(.*)\]\s*$/.exec(line);
    if (inlineMatch) {
      const key = inlineMatch[1] === "commands" ? "commands" : "envVars";
      const items = (inlineMatch[2] ?? "")
        .split(",")
        .map((s) => stripQuotes(s.trim()))
        .filter(Boolean);
      out[key] = items;
      cursor = null;
      continue;
    }
    const blockMatch = /^\s*(commands|env_vars)\s*:\s*$/.exec(line);
    if (blockMatch) {
      cursor = blockMatch[1] === "commands" ? "commands" : "envVars";
      out[cursor] = [];
      continue;
    }
    if (cursor) {
      const itemMatch = /^\s*-\s*(.+)$/.exec(line);
      if (itemMatch) {
        out[cursor]!.push(stripQuotes((itemMatch[1] ?? "").trim()));
      }
    }
  }
  return out;
}

function parseInlinePrerequisites(value: string): {
  commands?: string[];
  envVars?: string[];
} {
  // `{ commands: [foo, bar], env_vars: [BAZ] }` — flow-style YAML.
  const out: { commands?: string[]; envVars?: string[] } = {};
  const commands = /commands\s*:\s*\[([^\]]*)\]/.exec(value);
  if (commands) {
    out.commands = (commands[1] ?? "")
      .split(",")
      .map((s) => stripQuotes(s.trim()))
      .filter(Boolean);
  }
  const envVars = /env_vars\s*:\s*\[([^\]]*)\]/.exec(value);
  if (envVars) {
    out.envVars = (envVars[1] ?? "")
      .split(",")
      .map((s) => stripQuotes(s.trim()))
      .filter(Boolean);
  }
  return out;
}

function stripQuotes(s: string): string {
  if (s.length >= 2) {
    if ((s.startsWith('"') && s.endsWith('"')) || (s.startsWith("'") && s.endsWith("'"))) {
      return s.slice(1, -1);
    }
  }
  return s;
}
