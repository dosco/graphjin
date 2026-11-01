import { resolveUserGroups } from "@neko/db";
import type { RunActor } from "@neko/llm/work";
import { getCurrentUser } from "@/lib/auth";
import { getOrgId } from "@/lib/db";

/**
 * getCurrentUser without a request-scope requirement: outside a Next
 * request (tests, jobs) `cookies()` throws — treat that as "no session"
 * without granting anonymous admin access.
 */
export async function getCurrentUserSafe(): Promise<Awaited<
  ReturnType<typeof getCurrentUser>
> | null> {
  try {
    return await getCurrentUser();
  } catch {
    return null;
  }
}

/** Solo and SSO work both carry the persisted acting user's ID. */
export async function getCurrentActor(): Promise<RunActor> {
  const user = await getCurrentUserSafe();
  if (!user) return { userId: null, role: "member" };
  const groups = await resolveUserGroups(await getOrgId(), user.id);
  return { userId: user.id, role: groups.administrator ? "admin" : "member" };
}
