import { NextResponse } from "next/server";

/**
 * Without a sign-in provider nobody can sign in, so a user row promises an
 * account that cannot exist. An installation open to the public then
 * collects real addresses through a form that does nothing.
 */
export const NO_SIGN_IN_MESSAGE =
  "set up sign-in before you add people";

/** A fresh response per request: a Response body is read only once. */
export function noSignInResponse(): NextResponse {
  return NextResponse.json({ error: NO_SIGN_IN_MESSAGE }, { status: 409 });
}
