import "server-only";

import { db, eq, organization } from "@neko/db";

export async function getSetupCompleteAt(orgId: string): Promise<Date | null> {
  const rows = await db()
    .select({ setup_complete_at: organization.setup_complete_at })
    .from(organization)
    .where(eq(organization.id, orgId))
    .limit(1);
  return rows[0]?.setup_complete_at ?? null;
}
