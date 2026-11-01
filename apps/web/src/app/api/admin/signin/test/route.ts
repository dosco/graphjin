/**
 * POST /api/admin/signin/test — fire a delivery test through the real
 * sign-in path (mint token → provider API → mailbox). The email that
 * arrives is a genuine sign-in email, but its link is bound to a
 * synthetic state no browser holds, so it cannot complete a sign-in —
 * this proves delivery, nothing else. Admin-only.
 *
 * Body: { email: string }
 */

import { randomBytes } from "node:crypto";
import { NextRequest, NextResponse } from "next/server";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { beginAuth, buildRedirectUri } from "@/lib/auth";

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export async function POST(request: NextRequest) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;

  let body: { email?: unknown };
  try {
    body = await request.json();
  } catch {
    return NextResponse.json({ error: "invalid JSON body" }, { status: 400 });
  }
  const email =
    typeof body.email === "string" ? body.email.trim().toLowerCase() : "";
  if (!EMAIL_RE.test(email)) {
    return NextResponse.json(
      { error: "a valid email address is required" },
      { status: 400 },
    );
  }

  try {
    await beginAuth({
      redirectUri: buildRedirectUri(request.url),
      state: `delivery-test-${randomBytes(12).toString("base64url")}`,
      loginHint: email,
    });
    return NextResponse.json({ ok: true });
  } catch (err) {
    // Admin-only surface: the provider's real rejection is the whole
    // point of the test, pass it through.
    return NextResponse.json(
      { error: err instanceof Error ? err.message : String(err) },
      { status: 502 },
    );
  }
}
