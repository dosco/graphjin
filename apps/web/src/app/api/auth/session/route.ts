/**
 * GET /api/auth/session — read the current user from the session cookie.
 *
 * Returns `{ user: { id, email, name } }` when signed in,
 * `{ user: null }` otherwise. Client components poll this on mount to
 * decide whether to render dashboard chrome or a "Sign in" CTA.
 */

import { NextResponse, type NextRequest } from "next/server";
import { getCurrentUser, getAuthProvider, SESSION_COOKIE_NAME } from "@/lib/auth";
import { getCurrentActor } from "@/lib/actor";

export async function GET(request: NextRequest) {
  const user = await getCurrentUser();
  const actor = user ? await getCurrentActor() : null;
  // `signedIn` says the visitor holds a session cookie, so the app offers
  // Sign out. It stays true while the worker is unreachable.
  const signedIn = Boolean(request?.cookies?.get(SESSION_COOKIE_NAME)?.value);
  return NextResponse.json({
    user,
    role: actor?.role ?? null,
    authEnabled: Boolean(await getAuthProvider()),
    signedIn,
  });
}
