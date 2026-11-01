/**
 * GET /api/auth/status — is a sign-in provider live?
 *
 * The sign-in page calls this on mount to decide whether to render a
 * "Sign in with <provider>" button. Result is the sign-in provider's
 * id + human-readable label.
 * If sign-in is not set up, returns `{ provider: null }` and the
 * sign-in page renders the legacy local-auth notice.
 *
 * The web process doesn't query the worker on every request — the
 * worker's /admin/auth/status is itself a cheap in-memory read, and
 * @/lib/auth wraps it with a 1-second TTL cache so concurrent loads
 * don't fan out.
 */

import { NextResponse } from "next/server";
import { getAuthGateStatus } from "@/lib/auth";

export async function GET() {
  const { provider, pending } = await getAuthGateStatus();
  // Public route: expose the live provider, and of a pending one only
  // its label — which env vars are missing is admin-only information.
  return NextResponse.json({
    provider,
    pending: pending ? { providerLabel: pending.providerLabel } : null,
  });
}
