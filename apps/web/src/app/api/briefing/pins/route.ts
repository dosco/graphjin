import { NextRequest, NextResponse } from "next/server";
import { and, briefing_finding_pin, db, desc, eq } from "@neko/db";
import { getOrgId } from "@/lib/db";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET() {
  const orgId = await getOrgId();
  const rows = await db()
    .select()
    .from(briefing_finding_pin)
    .where(eq(briefing_finding_pin.org_id, orgId))
    .orderBy(
      briefing_finding_pin.sort_order,
      desc(briefing_finding_pin.pinned_at),
    );
  return NextResponse.json({
    pins: rows.map((r) => ({
      id: r.id,
      outputId: r.output_id,
      sortOrder: r.sort_order,
      pinnedAt: r.pinned_at.toISOString(),
    })),
  });
}

export async function POST(request: NextRequest) {
  const body = await request.json().catch(() => ({}));
  const outputId =
    typeof body.outputId === "string" && body.outputId.length > 0
      ? body.outputId
      : null;
  if (!outputId) {
    return NextResponse.json({ error: "outputId required" }, { status: 400 });
  }
  const orgId = await getOrgId();

  // Idempotent upsert — the unique (org_id, output_id) constraint covers
  // duplicates; double-pin returns the existing row.
  const existing = await db()
    .select()
    .from(briefing_finding_pin)
    .where(
      and(
        eq(briefing_finding_pin.org_id, orgId),
        eq(briefing_finding_pin.output_id, outputId),
      ),
    )
    .limit(1);

  if (existing[0]) {
    return NextResponse.json({
      pin: {
        id: existing[0].id,
        outputId: existing[0].output_id,
        sortOrder: existing[0].sort_order,
        pinnedAt: existing[0].pinned_at.toISOString(),
      },
      created: false,
    });
  }

  const [row] = await db()
    .insert(briefing_finding_pin)
    .values({
      org_id: orgId,
      output_id: outputId,
    })
    .returning();

  return NextResponse.json({
    pin: {
      id: row.id,
      outputId: row.output_id,
      sortOrder: row.sort_order,
      pinnedAt: row.pinned_at.toISOString(),
    },
    created: true,
  });
}
