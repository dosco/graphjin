import { NextResponse } from "next/server";
import {
  and,
  briefing_card,
  db,
  eq,
  observation,
} from "@neko/db";
import { getOrgId } from "@/lib/db";
import { workflowIdVisibility } from "@/lib/entitlements";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

// OL2 — observation-elevation. POST promotes an observation onto the
// Briefing as a first-class card; PATCH dismisses a card. The Briefing
// findings payload includes active cards.
export async function POST(request: Request) {
  const orgId = await getOrgId();
  const body = await request.json().catch(() => ({}));
  const observationId =
    typeof body.observationId === "string" ? body.observationId : "";
  if (!observationId) {
    return NextResponse.json(
      { error: "observationId is required" },
      { status: 400 },
    );
  }

  const [obs] = await db()
    .select({
      id: observation.id,
      title: observation.title,
      body: observation.body,
      mood: observation.mood,
      consumerWorkflowId: observation.consumer_workflow_id,
    })
    .from(observation)
    .where(and(eq(observation.org_id, orgId), eq(observation.id, observationId)));
  if (!obs || !(await workflowIdVisibility())(obs.consumerWorkflowId)) {
    return NextResponse.json({ error: "observation not found" }, { status: 404 });
  }

  const [card] = await db()
    .insert(briefing_card)
    .values({
      org_id: orgId,
      source_observation_id: obs.id,
      title: obs.title,
      body: obs.body,
      mood: obs.mood,
      elevated_by: "operator",
    })
    .onConflictDoUpdate({
      target: [briefing_card.org_id, briefing_card.source_observation_id],
      set: { status: "active", updated_at: new Date() },
    })
    .returning({ id: briefing_card.id });

  return NextResponse.json({ ok: true, cardId: card!.id });
}

export async function PATCH(request: Request) {
  const orgId = await getOrgId();
  const body = await request.json().catch(() => ({}));
  const cardId = typeof body.cardId === "string" ? body.cardId : "";
  if (!cardId) {
    return NextResponse.json({ error: "cardId is required" }, { status: 400 });
  }
  const result = await db()
    .update(briefing_card)
    .set({ status: "dismissed", updated_at: new Date() })
    .where(and(eq(briefing_card.org_id, orgId), eq(briefing_card.id, cardId)))
    .returning({ id: briefing_card.id });
  if (result.length === 0) {
    return NextResponse.json({ error: "card not found" }, { status: 404 });
  }
  return NextResponse.json({ ok: true });
}
