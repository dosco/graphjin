/**
 * GET /api/action-descriptors — the operator-facing snapshot of the
 * worker's built-in governed action kinds and their
 * seeded default mode. Used by /admin/rules to show which kinds the
 * agent can call.
 *
 * Thin proxy over the worker admin endpoint (/admin/action-descriptors)
 * — same 2s timeout, same graceful-empty-list fallback so a temporarily
 * unreachable worker doesn't break the rules page.
 */

import { NextResponse } from "next/server";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getActionDescriptors } from "@/lib/auth";

export async function GET() {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  const descriptors = await getActionDescriptors();
  return NextResponse.json({ descriptors });
}
