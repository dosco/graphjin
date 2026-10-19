/**
 * GET /api/auth/begin — start the SSO flow.
 *
 * Mint a CSRF state token, persist it (plus the return path) in an
 * httpOnly cookie, ask the sign-in provider for its authorization URL,
 * and 302 the user there. The IdP eventually bounces back to
 * /api/auth/callback with `code` + `state`, where the cookie's state
 * is checked against the IdP-echoed state to defeat login-CSRF.
 *
 * Providers with `provisioning: "manual"` (magic link) get an extra
 * gate here: the flow only reaches the provider when the typed email
 * belongs to a pre-provisioned, non-disabled user. Unknown emails get
 * the exact same "link sent" redirect without any provider call, so the
 * endpoint can't be used to enumerate accounts or spam mailboxes.
 *
 * Query params:
 *   ?returnTo=/some/internal/path  — optional, defaults to "/"
 *   ?loginHint=user@example.com    — forwarded to the IdP as login_hint
 */

import { NextRequest, NextResponse } from "next/server";
import { and, app_user, db, eq, isNull, sql } from "@neko/db";
import { getOrgId } from "@/lib/db";
import {
  beginAuth,
  buildRedirectUri,
  getAuthProvider,
  newStateToken,
  writeStateCookie,
} from "@/lib/auth";
import { appRedirect } from "@/lib/public-url";

/** The neutral outcome both known and unknown emails resolve to. */
function linkSentRedirect(): NextResponse {
  return appRedirect("/signin?notice=link-sent");
}

async function isProvisionedUser(email: string): Promise<boolean> {
  const orgId = await getOrgId();
  const [row] = await db()
    .select({ id: app_user.id })
    .from(app_user)
    .where(
      and(
        eq(app_user.org_id, orgId),
        sql`lower(${app_user.email}) = ${email.toLowerCase()}`,
        isNull(app_user.disabled_at),
      ),
    )
    .limit(1);
  return Boolean(row);
}

export async function GET(request: NextRequest) {
  const provider = await getAuthProvider();
  if (!provider) {
    return NextResponse.json(
      { error: "sign-in is not set up" },
      { status: 503 },
    );
  }
  const url = new URL(request.url);
  const returnTo = url.searchParams.get("returnTo") ?? "/";
  const loginHint = url.searchParams.get("loginHint")?.trim() ?? "";

  if (provider.loginHintRequired && !loginHint) {
    return appRedirect("/signin?error=enter+your+email+address+to+sign+in");
  }

  const manual = provider.provisioning === "manual";
  if (manual && !(await isProvisionedUser(loginHint))) {
    // Same response as the happy path — reveal nothing, send nothing.
    return linkSentRedirect();
  }

  const state = newStateToken();
  await writeStateCookie(state, returnTo);

  const redirectUri = buildRedirectUri(request.url);
  try {
    const { authorizationUrl } = await beginAuth({
      redirectUri,
      state,
      loginHint: loginHint.length > 0 ? loginHint : null,
    });
    return NextResponse.redirect(authorizationUrl, { status: 302 });
  } catch (err) {
    if (manual) {
      // A failed send must stay indistinguishable from an unknown email
      // to the visitor; the operator diagnoses via server logs.
      console.error(
        `[auth] magic-link begin failed: ${err instanceof Error ? err.message : err}`,
      );
      return linkSentRedirect();
    }
    return NextResponse.json(
      { error: err instanceof Error ? err.message : String(err) },
      { status: 502 },
    );
  }
}
