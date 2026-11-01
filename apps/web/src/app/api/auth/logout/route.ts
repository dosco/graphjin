/**
 * POST /api/auth/logout — clear the session cookie.
 *
 * Stateless — no DB write, no IdP single-logout call. (Single-logout
 * adds appreciable complexity for marginal benefit; if the operator
 * needs it, the IdP's own session cleanup handles it for them.)
 */

import { NextResponse } from "next/server";
import { clearSessionCookie } from "@/lib/auth";

export async function POST() {
  await clearSessionCookie();
  return NextResponse.json({ ok: true });
}
