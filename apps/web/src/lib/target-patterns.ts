export const MAX_TARGET_PATTERNS = 200;

const DOMAIN = /^(\*\.)?[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$/;

export type TargetKind = "value" | "email_domain";

/**
 * Splits a list typed one per line (or comma-separated) into target patterns.
 * Email-domain lists are lowercased and must be domains or `*.domain`.
 */
export function parseTargetPatterns(
  input: unknown,
  kind: TargetKind = "value",
): { ok: true; patterns: string[] } | { ok: false; error: string } {
  const raw = Array.isArray(input) ? input : typeof input === "string" ? input.split(/[\n,]/) : null;
  if (!raw) return { ok: false, error: "patterns must be a list of strings" };
  const patterns: string[] = [];
  for (const item of raw) {
    if (typeof item !== "string") return { ok: false, error: "patterns must be a list of strings" };
    const trimmed = kind === "email_domain" ? item.trim().toLowerCase().replace(/^@/, "") : item.trim();
    if (!trimmed) continue;
    if (trimmed.length > 500 || /\s/.test(trimmed)) return { ok: false, error: `"${trimmed}" is not a valid pattern` };
    if (kind === "email_domain" && !DOMAIN.test(trimmed)) {
      return { ok: false, error: `"${trimmed}" is not a domain. Use acme.com, or *.acme.com for its subdomains.` };
    }
    if (!patterns.includes(trimmed)) patterns.push(trimmed);
  }
  if (patterns.length > MAX_TARGET_PATTERNS) return { ok: false, error: `use at most ${MAX_TARGET_PATTERNS} patterns` };
  return { ok: true, patterns };
}

export function approvedTargetsRuleName(kind: string): string {
  return `Approved targets: ${kind}`;
}
