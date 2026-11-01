import { NextResponse } from "next/server";
import { effectiveAccess, resolveUserGroups } from "@neko/db";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";

export async function GET(_request: Request, context: { params: Promise<{ userId: string }> }) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const { userId } = await context.params;
  const orgId = await getOrgId();
  const [access, groups] = await Promise.all([effectiveAccess(orgId, userId), resolveUserGroups(orgId, userId)]);
  return NextResponse.json({ ...access, groupSlugs: groups.slugs });
}
