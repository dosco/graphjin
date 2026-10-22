/**
 * GET /api/auth/callback — finish the SSO flow.
 *
 * Handles the redirect from the IdP. Steps:
 *   1. Read the state cookie. If absent / forged, fail closed.
 *   2. Verify `state` query param matches the cookie (login-CSRF gate).
 *   3. Ask the sign-in provider to complete with the code, get an
 *      identity assertion.
 *   4. Upsert app_user from the identity.
 *   5. Set the signed session cookie, redirect to the original
 *      destination.
 *
 * Errors at any step surface as a plain text 4xx/5xx — the sign-in
 * page picks them up if the user re-tries.
 */

import { NextRequest } from "next/server";
import {
  buildRedirectUri,
  completeAuth,
  readAndClearStateCookie,
  upsertUserFromIdentity,
  writeSessionCookie,
} from "@/lib/auth";
import { appRedirect } from "@/lib/public-url";

export async function GET(request: NextRequest) {
  const url = new URL(request.url);
  const error = url.searchParams.get("error");
  if (error) {
    // The IdP itself rejected the request (consent denied, user
    // unprovisioned, ...). Bounce back to the sign-in page with the
    // error so the operator sees the reason rather than a blank
    // dashboard.
    const description = url.searchParams.get("error_description") ?? error;
    return appRedirect(`/signin?error=${encodeURIComponent(description)}`);
  }

  const code = url.searchParams.get("code");
  const state = url.searchParams.get("state");
  if (!code || !state) {
    return appRedirect("/signin?error=missing+code+or+state+on+callback");
  }

  const stored = await readAndClearStateCookie();
  if (!stored) {
    return appRedirect("/signin?error=state+cookie+missing+or+expired");
  }
  if (stored.state !== state) {
    // Login-CSRF check: an attacker tricking the user into clicking
    // an IdP callback URL with attacker-supplied code cannot succeed
    // here without also having forced the matching state cookie.
    return appRedirect("/signin?error=state+mismatch");
  }

  try {
    const identity = await completeAuth({
      code,
      redirectUri: buildRedirectUri(request.url),
      state,
    });
    const user = await upsertUserFromIdentity(identity);
    await writeSessionCookie(user.id);
    return appRedirect(stored.returnPath);
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    return appRedirect(`/signin?error=${encodeURIComponent(message)}`);
  }
}
