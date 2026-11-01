export type UpstreamProviderErrorKind = "5xx" | "auth";

export class UpstreamProviderError extends Error {
  readonly kind: UpstreamProviderErrorKind;
  readonly provider?: string;
  readonly statusCode?: number;
  constructor(
    message: string,
    opts?: { kind?: UpstreamProviderErrorKind; provider?: string; statusCode?: number },
  ) {
    super(message);
    this.name = "UpstreamProviderError";
    this.kind = opts?.kind ?? "5xx";
    this.provider = opts?.provider;
    this.statusCode = opts?.statusCode;
  }
}

export class DataSourceAuthorizationError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "DataSourceAuthorizationError";
  }
}

const PATTERNS: ReadonlyArray<{
  regex: RegExp;
  kind?: UpstreamProviderErrorKind;
  provider?: string;
}> = [
  { regex: /Authentication token has expired/i, kind: "auth" },
  { regex: /must authenticate again/i, kind: "auth" },
  { regex: /\bHTTP\s+401\b/i, kind: "auth" },
  { regex: /\binvalid[_\s-]*api[_\s-]*key\b/i, kind: "auth" },

  { regex: /Gemini\s+HTTP\s+(5\d{2})\b/i, kind: "5xx", provider: "google-gemini" },
  { regex: /Anthropic\s+HTTP\s+(5\d{2})\b/i, kind: "5xx", provider: "anthropic" },
  { regex: /OpenAI\s+HTTP\s+(5\d{2})\b/i, kind: "5xx", provider: "openai" },
  { regex: /API call failed after \d+ retries:\s*HTTP\s+(5\d{2})\b/i, kind: "5xx" },
  { regex: /\bHTTP\s+(5\d{2})\b.*?(UNAVAILABLE|INTERNAL|SERVICE_UNAVAILABLE|RESOURCE_EXHAUSTED)?/i, kind: "5xx" },
];

function headingFor(kind: UpstreamProviderErrorKind, provider?: string): string {
  const providerLabel = provider ? ` (${provider})` : "";
  if (kind === "auth") {
    return `Provider authentication failed${providerLabel} — re-check the API key in /admin/settings/agent`;
  }
  return `Upstream provider unavailable${providerLabel}`;
}

export function detectDataSourceAuthorizationError(
  stdout: string,
): DataSourceAuthorizationError | null {
  const head = stdout.slice(0, 1500).trim();
  if (head.startsWith("{") || head.startsWith("```")) return null;
  if (!/\b(?:blocked for role|graphjin\b[\s\S]*\bunauthorized)\b/i.test(head)) {
    return null;
  }
  const detail = head.slice(0, 300).replace(/\s+/g, " ").trim();
  return new DataSourceAuthorizationError(
    `Data source authorization failed — verify the authentication mode in /admin/settings/data: ${detail}`,
  );
}

export function detectUpstreamError(stdout: string): UpstreamProviderError | null {
  const head = stdout.slice(0, 1500).trim();
  if (head.startsWith("{") || head.startsWith("```")) return null;

  for (const { regex, kind = "5xx", provider } of PATTERNS) {
    const m = head.match(regex);
    if (!m) continue;
    const statusRaw = m[1] ? Number(m[1]) : NaN;
    const statusCode = Number.isFinite(statusRaw) ? statusRaw : undefined;
    const detail = head.slice(0, 300).replace(/\s+/g, " ").trim();
    return new UpstreamProviderError(
      `${headingFor(kind, provider)}: ${detail}`,
      { kind, provider, statusCode },
    );
  }
  return null;
}

export function detectAgentExecutionError(stdout: string): Error | null {
  return detectDataSourceAuthorizationError(stdout) ?? detectUpstreamError(stdout);
}
