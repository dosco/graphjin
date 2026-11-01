import { NextResponse } from "next/server";
import {
  assertCan,
  buildOperatorProfileSection,
  ForbiddenError,
  getOperatorProfile,
  upsertOperatorProfile,
} from "@neko/llm/work";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

// CV3 — the actor's persona. Solo profile (no session user) reads/writes
// the org-default ('') row; members own their row (K2 personal resource);
// admins may edit any.
export async function GET() {
  const orgId = await getOrgId();
  const actor = await getCurrentActor();
  const profile = await getOperatorProfile(orgId, actor.userId);
  return NextResponse.json({
    profile,
    promptPreview: buildOperatorProfileSection(profile),
  });
}

export async function PUT(request: Request) {
  const orgId = await getOrgId();
  const actor = await getCurrentActor();
  const body = await request.json().catch(() => ({}));

  const targetUserId =
    typeof body.userId === "string" ? body.userId : (actor.userId ?? "");
  try {
    assertCan(
      actor,
      "write",
      // The org-default ('') persona is org configuration; a user row is
      // a personal resource.
      targetUserId === ""
        ? { kind: "org_settings" }
        : { kind: "personal", ownerUserId: targetUserId },
      `persona ${targetUserId || "(org default)"}`,
    );
  } catch (err) {
    if (err instanceof ForbiddenError) {
      return NextResponse.json({ error: err.message }, { status: 403 });
    }
    throw err;
  }

  const profile = await upsertOperatorProfile({
    orgId,
    userId: targetUserId,
    ...(typeof body.displayName === "string" || body.displayName === null
      ? { displayName: body.displayName }
      : {}),
    ...(typeof body.roleTemplate === "string"
      ? { roleTemplate: body.roleTemplate }
      : {}),
    ...(Array.isArray(body.focusAreas)
      ? {
          focusAreas: body.focusAreas.filter(
            (f: unknown): f is string => typeof f === "string" && !!f.trim(),
          ),
        }
      : {}),
    ...(body.answers && typeof body.answers === "object"
      ? { answers: body.answers as Record<string, unknown> }
      : {}),
    ...(typeof body.briefMd === "string" ? { briefMd: body.briefMd } : {}),
  });
  return NextResponse.json({
    profile,
    promptPreview: buildOperatorProfileSection(profile),
  });
}
