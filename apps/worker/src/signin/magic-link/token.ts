/**
 * Stateless single-use sign-in tokens.
 *
 * Nothing is stored between begin and complete, so the token itself
 * carries everything verification needs: the email it was issued for,
 * the CSRF state of the browser that requested it, and an expiry —
 * HMAC-SHA256-signed so none of it can be altered.
 *
 * Single-use is enforced by the core, not here: the state cookie is
 * read-and-cleared on the first callback, and a token only verifies
 * against the state it was minted for, so a replayed link finds no
 * matching cookie and dies at the core's login-CSRF check. The same
 * binding means a link only works in the browser that requested it.
 */

import { createHmac, timingSafeEqual } from "node:crypto";

export const MIN_SECRET_LENGTH = 32;
export const DEFAULT_TTL_SECONDS = 600;
export const MIN_TTL_SECONDS = 60;
/** The core's state cookie dies at 10 minutes; longer tokens buy nothing. */
export const MAX_TTL_SECONDS = 600;

export interface TokenClaims {
  email: string;
  state: string;
  expiresAt: number;
}

function b64url(data: Buffer | string): string {
  return Buffer.from(data).toString("base64url");
}

function sign(payload: string, secret: string): string {
  return createHmac("sha256", secret).update(payload).digest("base64url");
}

export function mintToken(input: {
  email: string;
  state: string;
  secret: string;
  ttlSeconds?: number;
  nowMs?: number;
}): string {
  if (input.secret.length < MIN_SECRET_LENGTH) {
    throw new Error(
      `MAGIC_LINK_SIGNING_SECRET must be at least ${MIN_SECRET_LENGTH} characters`,
    );
  }
  const ttl = clampTtl(input.ttlSeconds);
  const claims: TokenClaims = {
    email: input.email,
    state: input.state,
    expiresAt: Math.floor((input.nowMs ?? Date.now()) / 1000) + ttl,
  };
  const payload = b64url(JSON.stringify(claims));
  return `${payload}.${sign(payload, input.secret)}`;
}

export function clampTtl(ttlSeconds: number | undefined): number {
  if (ttlSeconds === undefined || Number.isNaN(ttlSeconds)) {
    return DEFAULT_TTL_SECONDS;
  }
  return Math.min(MAX_TTL_SECONDS, Math.max(MIN_TTL_SECONDS, Math.floor(ttlSeconds)));
}

/**
 * Verify signature, expiry, and state binding. Returns the claims or
 * throws — callers surface the message on the sign-in page, so keep
 * reasons generic (no oracle for which part failed beyond expiry,
 * which the user genuinely needs to know to re-request a link).
 */
export function verifyToken(input: {
  token: string;
  state: string;
  secret: string;
  nowMs?: number;
}): TokenClaims {
  const parts = input.token.split(".");
  if (parts.length !== 2 || !parts[0] || !parts[1]) {
    throw new Error("this sign-in link is not valid");
  }
  const [payload, signature] = parts;
  const expected = sign(payload, input.secret);
  const provided = Buffer.from(signature, "base64url");
  const wanted = Buffer.from(expected, "base64url");
  if (provided.length !== wanted.length || !timingSafeEqual(provided, wanted)) {
    throw new Error("this sign-in link is not valid");
  }
  let claims: TokenClaims;
  try {
    const parsed = JSON.parse(
      Buffer.from(payload, "base64url").toString("utf8"),
    ) as Partial<TokenClaims>;
    if (
      typeof parsed.email !== "string" ||
      parsed.email.length === 0 ||
      typeof parsed.state !== "string" ||
      parsed.state.length === 0 ||
      typeof parsed.expiresAt !== "number"
    ) {
      throw new Error("malformed claims");
    }
    claims = parsed as TokenClaims;
  } catch {
    throw new Error("this sign-in link is not valid");
  }
  if (claims.state !== input.state) {
    // Wrong browser or a replayed link — the state cookie can't match.
    throw new Error(
      "this sign-in link must be opened in the browser that requested it",
    );
  }
  const now = Math.floor((input.nowMs ?? Date.now()) / 1000);
  if (claims.expiresAt <= now) {
    throw new Error("this sign-in link has expired — request a new one");
  }
  return claims;
}
