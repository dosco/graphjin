const SECRET_RE =
  /(api[_-]?key|secret|password|bearer\s+[a-z0-9._-]+|sk-[a-z0-9]{16,})/i;
const EMAIL_RE = /\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b/i;
const FRONTMATTER_RE = /^---\s*$/m;
const TOOL_PERM_RE = /\ballowed-tools\s*:/i;
const BOUNDARY_RE = /^\s*Boundary\s*:/m;

export type LearnedScanHit = {
  code: "secret" | "pii" | "frontmatter" | "tools" | "boundary";
  detail: string;
};

export function scanLearnedText(text: string): LearnedScanHit[] {
  const hits: LearnedScanHit[] = [];
  if (SECRET_RE.test(text)) {
    hits.push({ code: "secret", detail: "candidate looks like a secret" });
  }
  if (EMAIL_RE.test(text)) {
    hits.push({ code: "pii", detail: "candidate contains an email address" });
  }
  if (FRONTMATTER_RE.test(text.trimStart()) || /^\s*name\s*:/m.test(text)) {
    hits.push({
      code: "frontmatter",
      detail: "candidate tries to set skill frontmatter",
    });
  }
  if (TOOL_PERM_RE.test(text)) {
    hits.push({ code: "tools", detail: "candidate edits tool permissions" });
  }
  if (BOUNDARY_RE.test(text)) {
    hits.push({
      code: "boundary",
      detail: "candidate edits a safety boundary",
    });
  }
  return hits;
}

export function assertAdditiveLearnedBody(text: string): string | null {
  const trimmed = text.trim();
  if (!trimmed) return "learned body is empty";
  if (trimmed.length > 8000) return "learned body is too long";
  const hits = scanLearnedText(trimmed);
  if (hits.length > 0) return hits.map((hit) => hit.detail).join("; ");
  return null;
}
