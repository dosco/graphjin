import { NextResponse } from "next/server";
import { and, briefing_finding_pin, db, eq } from "@neko/db";
import { getOrgId } from "@/lib/db";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type RouteContext = {
  params: Promise<{ pinId: string }>;
};

export async function DELETE(_: Request, context: RouteContext) {
  const { pinId } = await context.params;
  const orgId = await getOrgId();
  const result = await db()
    .delete(briefing_finding_pin)
    .where(
      and(
        eq(briefing_finding_pin.org_id, orgId),
        eq(briefing_finding_pin.id, pinId),
      ),
    )
    .returning({ id: briefing_finding_pin.id });
  if (result.length === 0) {
    return NextResponse.json({ error: "pin not found" }, { status: 404 });
  }
  return NextResponse.json({ ok: true });
}
