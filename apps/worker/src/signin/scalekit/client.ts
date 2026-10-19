// Thin Scalekit OIDC client. Authorization-code flow with client_secret
// auth at the token endpoint. No SDK dep so the bundled runner stays
// small. Endpoints follow the Scalekit Auth API:
//   GET  {env}/oauth/authorize    — browser redirect (we build the URL)
//   POST {env}/oauth/token        — code → tokens
//   GET  {env}/userinfo           — access_token → identity claims
// See https://docs.scalekit.com/sso/quickstart.

export const DEFAULT_TIMEOUT_MS = 20_000;
const SCALEKIT_OIDC_SCOPES = "openid profile email";

export class ScalekitApiError extends Error {
  constructor(
    message: string,
    public readonly status: number | null,
    public readonly providerError: string | null,
    public override readonly cause?: unknown,
  ) {
    super(message);
    this.name = "ScalekitApiError";
  }
}

export interface ScalekitClientOptions {
  environmentUrl: string;
  clientId: string;
  clientSecret: string;
  fetchImpl?: typeof fetch;
  timeoutMs?: number;
}

export interface ScalekitTokenResponse {
  access_token: string;
  id_token?: string;
  token_type: string;
  expires_in?: number;
  scope?: string;
}

/**
 * Subset of the OIDC userinfo claims OpenNeko relies on. Scalekit
 * forwards whatever the downstream IdP supplies plus its own
 * organization id under `organization_id` (a Scalekit extension).
 */
export interface ScalekitUserinfo {
  sub: string;
  email?: string;
  email_verified?: boolean;
  name?: string;
  given_name?: string;
  family_name?: string;
  organization_id?: string;
  groups?: string[];
  roles?: string[];
  [k: string]: unknown;
}

export interface AuthorizationUrlOptions {
  redirectUri: string;
  state: string;
  loginHint?: string | null | undefined;
  /** Override the default `openid profile email` scope set. */
  scope?: string;
}

export interface ScalekitClient {
  buildAuthorizationUrl(opts: AuthorizationUrlOptions): string;
  exchangeCode(opts: {
    code: string;
    redirectUri: string;
  }): Promise<ScalekitTokenResponse>;
  fetchUserinfo(accessToken: string): Promise<ScalekitUserinfo>;
}

export function normalizeEnvironmentUrl(raw: string): string {
  const trimmed = raw.trim().replace(/\/+$/, "");
  if (!trimmed) {
    throw new ScalekitApiError(
      "SCALEKIT_ENVIRONMENT_URL is empty",
      null,
      null,
    );
  }
  if (!/^https:\/\//i.test(trimmed)) {
    throw new ScalekitApiError(
      `SCALEKIT_ENVIRONMENT_URL must be an https URL (got ${trimmed})`,
      null,
      null,
    );
  }
  return trimmed;
}

export async function scalekitRequest<T>(
  fetchImpl: typeof fetch,
  timeoutMs: number,
  url: string,
  init: RequestInit,
  description: string,
): Promise<T> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  let response: Response;
  try {
    response = await fetchImpl(url, { ...init, signal: controller.signal });
  } catch (err) {
    if ((err as Error)?.name === "AbortError") {
      throw new ScalekitApiError(
        `Scalekit ${description} timed out after ${timeoutMs}ms`,
        null,
        "timeout",
      );
    }
    throw new ScalekitApiError(
      `Scalekit ${description} network error: ${err instanceof Error ? err.message : String(err)}`,
      null,
      null,
      err,
    );
  } finally {
    clearTimeout(timer);
  }
  const text = await response.text().catch(() => "");
  let body: unknown;
  if (text) {
    try {
      body = JSON.parse(text);
    } catch (err) {
      throw new ScalekitApiError(
        `Scalekit ${description} returned non-JSON (HTTP ${response.status}): ${text.slice(0, 200)}`,
        response.status,
        null,
        err,
      );
    }
  }
  if (!response.ok) {
    const providerError =
      (body as { error?: string } | undefined)?.error ?? null;
    const providerDescription =
      (body as { error_description?: string } | undefined)?.error_description ?? null;
    throw new ScalekitApiError(
      `Scalekit ${description} returned HTTP ${response.status} (${providerError ?? "no error code"}${providerDescription ? `: ${providerDescription}` : ""})`,
      response.status,
      providerError,
    );
  }
  return (body ?? {}) as T;
}

export function createScalekitClient(
  options: ScalekitClientOptions,
): ScalekitClient {
  const envUrl = normalizeEnvironmentUrl(options.environmentUrl);
  const clientId = options.clientId;
  const clientSecret = options.clientSecret;
  if (!clientId) {
    throw new ScalekitApiError("SCALEKIT_CLIENT_ID is empty", null, null);
  }
  if (!clientSecret) {
    throw new ScalekitApiError(
      "SCALEKIT_CLIENT_SECRET is empty",
      null,
      null,
    );
  }
  const fetchImpl = options.fetchImpl ?? fetch;
  const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;

  const call = <T>(url: string, init: RequestInit, description: string) =>
    scalekitRequest<T>(fetchImpl, timeoutMs, url, init, description);

  return {
    buildAuthorizationUrl({ redirectUri, state, loginHint, scope }) {
      if (!redirectUri) {
        throw new ScalekitApiError("redirectUri is required", null, null);
      }
      if (!state) {
        throw new ScalekitApiError("state is required", null, null);
      }
      const url = new URL("/oauth/authorize", envUrl);
      url.searchParams.set("response_type", "code");
      url.searchParams.set("client_id", clientId);
      url.searchParams.set("redirect_uri", redirectUri);
      url.searchParams.set("state", state);
      url.searchParams.set("scope", scope ?? SCALEKIT_OIDC_SCOPES);
      if (loginHint) {
        // Scalekit uses login_hint for both email-based connection
        // discovery and direct IdP routing — pass the operator's
        // typed value through verbatim.
        url.searchParams.set("login_hint", loginHint);
      }
      return url.toString();
    },

    async exchangeCode({ code, redirectUri }) {
      if (!code) {
        throw new ScalekitApiError("code is required", null, null);
      }
      if (!redirectUri) {
        throw new ScalekitApiError("redirectUri is required", null, null);
      }
      const body = new URLSearchParams();
      body.set("grant_type", "authorization_code");
      body.set("code", code);
      body.set("redirect_uri", redirectUri);
      const basic = Buffer.from(`${clientId}:${clientSecret}`).toString(
        "base64",
      );
      const tokens = await call<ScalekitTokenResponse>(
        new URL("/oauth/token", envUrl).toString(),
        {
          method: "POST",
          headers: {
            Authorization: `Basic ${basic}`,
            "Content-Type": "application/x-www-form-urlencoded",
            Accept: "application/json",
          },
          body: body.toString(),
        },
        "token exchange",
      );
      if (!tokens.access_token) {
        throw new ScalekitApiError(
          "Scalekit token exchange returned no access_token",
          null,
          null,
        );
      }
      return tokens;
    },

    async fetchUserinfo(accessToken) {
      if (!accessToken) {
        throw new ScalekitApiError("access_token is required", null, null);
      }
      const info = await call<ScalekitUserinfo>(
        new URL("/userinfo", envUrl).toString(),
        {
          method: "GET",
          headers: {
            Authorization: `Bearer ${accessToken}`,
            Accept: "application/json",
          },
        },
        "userinfo",
      );
      if (!info.sub) {
        throw new ScalekitApiError(
          "Scalekit userinfo returned no sub",
          null,
          null,
        );
      }
      return info;
    },
  };
}
