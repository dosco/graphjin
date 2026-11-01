import {
  readFullSecretsFileSoft,
  type FullSecretsFile,
  type SecretsStore,
} from "./secrets-store";
import type { SecretsResolver } from "./secrets-resolver";

/**
 * SEC3 — Infisical-backed deployment-wide env bags, over the SEC2
 * resolver seam. Residency split per SECRETS_INFISICAL_PLAN:
 *   - env bags (API keys shared by the deployment) live in a self-hosted
 *     Infisical project, one folder per section with '/' encoded as '__'
 *     (e.g. /channel.slack/SLACK_BOT_TOKEN);
 *   - per-operator OAuth credentials STAY in the local secrets file
 *     (enc:v1 at rest) — they're operator-scoped, not deployment-scoped.
 * Universal Auth machine identity; the access token is cached and
 * secrets are re-fetched on a short TTL, so a rotation in Infisical
 * surfaces on the next refresh without a restart.
 */
export type InfisicalConfig = {
  /** Self-hosted Infisical base URL, e.g. https://infisical.internal. */
  siteUrl: string;
  projectId: string;
  /** Infisical environment slug (default "prod"). */
  environment?: string;
  /** Universal Auth machine identity. Bootstrap via env when omitted:
   *  INFISICAL_UNIVERSAL_AUTH_CLIENT_ID / _CLIENT_SECRET. */
  clientId?: string;
  clientSecret?: string;
  /** Secrets cache TTL ms (default 60s). */
  cacheTtlMs?: number;
};

type Fetcher = typeof fetch;

const FOLDER_ENCODED_SLASH = "__";

export function sectionToFolder(section: string): string {
  return section.replaceAll("/", FOLDER_ENCODED_SLASH);
}

export function folderToSection(folder: string): string {
  return folder.replaceAll(FOLDER_ENCODED_SLASH, "/");
}

export class InfisicalSecretsResolver implements SecretsResolver {
  private token: { value: string; expiresAt: number } | null = null;
  private cache: { env: SecretsStore; expiresAt: number } | null = null;

  constructor(
    private readonly config: InfisicalConfig,
    /** Test seam. */
    private readonly fetcher: Fetcher = fetch,
    /** Local file fallback/merge source for the operators section. */
    private readonly localOverrideDir?: string,
  ) {}

  async resolveFullSecrets(
    warn?: (line: string) => void,
  ): Promise<FullSecretsFile> {
    // Per-operator credentials are always local (enc:v1 at rest).
    const local = await readFullSecretsFileSoft(this.localOverrideDir, warn);
    try {
      const env = await this.fetchEnvBags();
      return { env, operators: local.operators };
    } catch (err) {
      warn?.(
        `infisical resolver failed (${err instanceof Error ? err.message : err}); falling back to the local secrets file`,
      );
      return local;
    }
  }

  private async fetchEnvBags(): Promise<SecretsStore> {
    if (this.cache && this.cache.expiresAt > Date.now()) {
      return this.cache.env;
    }
    const token = await this.login();
    const url = new URL("/api/v3/secrets/raw", this.config.siteUrl);
    url.searchParams.set("workspaceId", this.config.projectId);
    url.searchParams.set("environment", this.config.environment ?? "prod");
    url.searchParams.set("secretPath", "/");
    url.searchParams.set("recursive", "true");
    const res = await this.fetcher(url, {
      headers: { authorization: `Bearer ${token}` },
    });
    if (!res.ok) {
      throw new Error(`infisical secrets fetch -> ${res.status}`);
    }
    const body = (await res.json()) as {
      secrets?: Array<{
        secretKey?: string;
        secretValue?: string;
        secretPath?: string;
      }>;
    };
    const env: SecretsStore = {};
    for (const s of body.secrets ?? []) {
      if (!s.secretKey || typeof s.secretValue !== "string") continue;
      const folder = (s.secretPath ?? "/").replaceAll("/", "");
      if (!folder) continue; // root-level secrets aren't section bags
      const section = folderToSection(folder);
      (env[section] ??= {})[s.secretKey] = s.secretValue;
    }
    this.cache = {
      env,
      expiresAt: Date.now() + (this.config.cacheTtlMs ?? 60_000),
    };
    return env;
  }

  private async login(): Promise<string> {
    if (this.token && this.token.expiresAt > Date.now()) {
      return this.token.value;
    }
    const clientId =
      this.config.clientId ??
      process.env.INFISICAL_UNIVERSAL_AUTH_CLIENT_ID ??
      "";
    const clientSecret =
      this.config.clientSecret ??
      process.env.INFISICAL_UNIVERSAL_AUTH_CLIENT_SECRET ??
      "";
    if (!clientId || !clientSecret) {
      throw new Error(
        "missing Universal Auth identity (INFISICAL_UNIVERSAL_AUTH_CLIENT_ID / _CLIENT_SECRET)",
      );
    }
    const res = await this.fetcher(
      new URL("/api/v1/auth/universal-auth/login", this.config.siteUrl),
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ clientId, clientSecret }),
      },
    );
    if (!res.ok) {
      throw new Error(`infisical universal-auth login -> ${res.status}`);
    }
    const body = (await res.json()) as {
      accessToken?: string;
      expiresIn?: number;
    };
    if (!body.accessToken) throw new Error("infisical login returned no token");
    this.token = {
      value: body.accessToken,
      // Refresh a minute early.
      expiresAt: Date.now() + Math.max(60, (body.expiresIn ?? 3600) - 60) * 1000,
    };
    return this.token.value;
  }
}
