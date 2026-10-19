/**
 * POST /api/admin/signin/provider — choose the sign-in provider.
 * Body: { provider: "scalekit" | "magic-link" | null }. The provider goes
 * live once its required settings are stored; null turns sign-in off.
 */

import { NextRequest, NextResponse } from "next/server";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { selectSignInProvider } from "@/lib/auth";

export async function POST(request: NextRequest) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;

  let body: { provider?: unknown };
  try {
    body = await request.json();
  } catch {
    return NextResponse.json({ error: "invalid JSON body" }, { status: 400 });
  }
  const provider = body.provider;
  if (provider !== null && provider !== "scalekit" && provider !== "magic-link") {
    return NextResponse.json(
      { error: "provider must be scalekit, magic-link, or null" },
      { status: 400 },
    );
  }
  try {
    await selectSignInProvider(provider);
    return NextResponse.json({ ok: true });
  } catch (err) {
    return NextResponse.json(
      { error: err instanceof Error ? err.message : String(err) },
      { status: 502 },
    );
  }
}
