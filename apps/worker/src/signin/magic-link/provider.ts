/**
 * Passwordless email sign-in with manual provisioning: the web only starts
 * this flow for emails an admin pre-provisioned, and never creates users
 * from it. This module's job is narrow — mint a signed token, email the
 * link, verify the token on callback:
 *
 *   begin    → HMAC-sign {email, state, expiry}, email the link
 *              `${redirectUri}?code=<token>&state=<state>`, and point the
 *              browser at the "check your email" notice.
 *   complete → verify signature + expiry + state binding, return the
 *              identity (groups always empty — role and access assignment
 *              is the core's job, and nothing self-asserted influences it).
 *
 * Single-use and same-browser properties come from the web's read-and-clear
 * state cookie; see token.ts for the exact argument.
 */

import type { AuthIdentity, BeginAuthParams, CompleteAuthParams, SignInSetting } from "../types.js";
import { buildSignInEmail, resolveProvider, type EmailSender } from "./email.js";
import { clampTtl, mintToken, MIN_SECRET_LENGTH, verifyToken } from "./token.js";

export type MagicLinkEnv = Readonly<Record<string, string | undefined>>;

export const MAGIC_LINK_SETTINGS: SignInSetting[] = [
  { key: "MAGIC_LINK_SIGNING_SECRET", required: true, secret: true, autogenerate: true },
  { key: "MAGIC_LINK_FROM", required: true, secret: false },
  { key: "RESEND_API_KEY", required: false, secret: true },
  { key: "POSTMARK_SERVER_TOKEN", required: false, secret: true },
  { key: "SENDGRID_API_KEY", required: false, secret: true },
  { key: "MAGIC_LINK_TTL_SECONDS", required: false, secret: false },
];

export class MagicLinkError extends Error {
  constructor(message: string, public override readonly cause?: unknown) {
    super(message);
    this.name = "MagicLinkError";
  }
}

/** Test seam: inject a fake sender / clock instead of the real ones. */
export interface MagicLinkOptions {
  send?: EmailSender;
  nowMs?: number;
}

function resolveEnv(env: MagicLinkEnv): { secret: string; from: string; ttlSeconds: number } {
  const secret = env.MAGIC_LINK_SIGNING_SECRET ?? "";
  if (secret.length < MIN_SECRET_LENGTH) {
    throw new MagicLinkError(`MAGIC_LINK_SIGNING_SECRET must be set to at least ${MIN_SECRET_LENGTH} characters`);
  }
  const from = env.MAGIC_LINK_FROM?.trim() ?? "";
  if (!from) throw new MagicLinkError("MAGIC_LINK_FROM must be set");
  const rawTtl = env.MAGIC_LINK_TTL_SECONDS;
  return { secret, from, ttlSeconds: clampTtl(rawTtl ? Number(rawTtl) : undefined) };
}

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export async function beginMagicLinkAuth(
  params: BeginAuthParams,
  env: MagicLinkEnv,
  options: MagicLinkOptions = {},
): Promise<{ authorizationUrl: string }> {
  if (!params.redirectUri) throw new MagicLinkError("params.redirectUri is required");
  if (!params.state) throw new MagicLinkError("params.state is required");
  const email = params.loginHint?.trim().toLowerCase() ?? "";
  if (!EMAIL_RE.test(email)) {
    // The sign-in page requires the email field (loginHintRequired), so
    // reaching here means a hand-built request.
    throw new MagicLinkError("magic link sign-in requires the email address as loginHint");
  }

  let callback: URL;
  try {
    callback = new URL(params.redirectUri);
  } catch {
    throw new MagicLinkError("params.redirectUri must be an absolute URL");
  }

  const resolved = resolveEnv(env);
  const token = mintToken({
    email,
    state: params.state,
    secret: resolved.secret,
    ttlSeconds: resolved.ttlSeconds,
    nowMs: options.nowMs,
  });

  const link = new URL(callback.toString());
  link.searchParams.set("code", token);
  link.searchParams.set("state", params.state);

  const send = options.send ?? resolveProvider({
    resendApiKey: env.RESEND_API_KEY,
    postmarkServerToken: env.POSTMARK_SERVER_TOKEN,
    sendgridApiKey: env.SENDGRID_API_KEY,
  }).send;
  await send(
    buildSignInEmail({
      from: resolved.from,
      to: email,
      link: link.toString(),
      ttlMinutes: Math.max(1, Math.round(resolved.ttlSeconds / 60)),
    }),
  );

  // No IdP to redirect to — bounce the browser to the sign-in page's "check
  // your email" notice. The web shows the identical notice when it drops an
  // unprovisioned email before ever calling here.
  return { authorizationUrl: `${callback.origin}/signin?notice=link-sent` };
}

export async function completeMagicLinkAuth(
  params: CompleteAuthParams,
  env: MagicLinkEnv,
  options: MagicLinkOptions = {},
): Promise<AuthIdentity> {
  if (!params.code) throw new MagicLinkError("params.code is required");
  if (!params.state) throw new MagicLinkError("params.state is required");
  const resolved = resolveEnv(env);
  const claims = verifyToken({ token: params.code, state: params.state, secret: resolved.secret, nowMs: options.nowMs });
  return {
    // Deterministic per mailbox so repeat sign-ins hit the same app_user row.
    sub: `magic-link:${claims.email}`,
    email: claims.email,
    name: null,
    orgId: null,
    // Self-asserted flows must never influence role mapping.
    groups: [],
  };
}
