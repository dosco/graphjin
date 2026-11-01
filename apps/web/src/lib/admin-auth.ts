import { NextResponse } from "next/server";
import type { RunActor } from "@neko/llm/work";
import { getCurrentActor } from "@/lib/actor";
import { getAuthProvider } from "@/lib/auth";

export function adminOnlyJson(): NextResponse {
  return NextResponse.json({ error: "admin only" }, { status: 403 });
}

/**
 * Admin gate for OpenNeko-native administration routes.
 *
 * Solo resolves its persisted local admin automatically. Once SSO sign-in is live (a provider is ready on
 * /admin/auth/status), a request without a valid session is NOT the solo
 * operator — it's an unauthenticated visitor — and is denied. The proxy
 * exempts the SSO setup surfaces from the session gate precisely so this
 * check can take over there.
 */
export async function requireAdminActor(): Promise<RunActor | NextResponse> {
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return adminOnlyJson();
  if (actor.userId === null && (await getAuthProvider())) {
    // SSO is live but the request carries no session: unauthenticated.
    return adminOnlyJson();
  }
  return actor;
}

export function isDenied(
  result: RunActor | NextResponse,
): result is NextResponse {
  return result instanceof Response;
}
