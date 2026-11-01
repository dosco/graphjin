import { NextRequest, NextResponse } from "next/server";
import {
  archiveWorkMemory,
  getWorkMemory,
  overrideWorkMemoryForUser,
} from "@neko/llm/work";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { holdsItem } from "@/lib/entitlements";

type RouteContext = {
  params: Promise<{ id: string }>;
};

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(_request: NextRequest, context: RouteContext) {
  const { id } = await context.params;
  const orgId = await getOrgId();
  const memory = await getWorkMemory(orgId, id);
  if (!memory) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }
  const actor = await getCurrentActor();
  if (
    actor.role !== "admin" &&
    !memory.userId &&
    memory.scope !== "thread" &&
    !(await holdsItem("team_memory", memory.scope === "database" ? `database:${memory.scopeId ?? ""}` : "global"))
  ) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }
  if (
    actor.role !== "admin" &&
    memory.userId &&
    memory.userId !== actor.userId
  ) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }
  return NextResponse.json({ memory });
}

// CV2: members personalize a memory — body { text } edits their copy,
// { suppress: true } hides it for them. The team row is never modified.
export async function PATCH(request: NextRequest, context: RouteContext) {
  const { id } = await context.params;
  const actor = await getCurrentActor();
  if (actor.role !== "member" || !actor.userId) {
    return NextResponse.json(
      { error: "personal overrides are for members; admins edit the team layer" },
      { status: 403 },
    );
  }
  const body = await request.json().catch(() => ({}));
  const text = typeof body.text === "string" ? body.text : undefined;
  const suppress = body.suppress === true;
  if (!text && !suppress) {
    return NextResponse.json(
      { error: "provide text or suppress" },
      { status: 400 },
    );
  }
  const orgId = await getOrgId();
  const existing = await getWorkMemory(orgId, id);
  if (existing && !existing.userId && existing.scope !== "thread" && !(await holdsItem("team_memory", existing.scope === "database" ? `database:${existing.scopeId ?? ""}` : "global"))) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }
  try {
    const memory = await overrideWorkMemoryForUser({
      orgId,
      userId: actor.userId,
      memoryId: id,
      text,
      suppress,
    });
    return NextResponse.json({ memory });
  } catch {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }
}

export async function DELETE(request: NextRequest, context: RouteContext) {
  const { id } = await context.params;
  const body = await request.json().catch(() => ({}));
  const reason = typeof body.reason === "string" ? body.reason : undefined;
  const orgId = await getOrgId();
  const actor = await getCurrentActor();

  const target = await getWorkMemory(orgId, id);
  if (
    actor.role !== "admin" &&
    target?.userId &&
    target.userId !== actor.userId
  ) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }

  if (actor.role === "member" && actor.userId) {
    const memory = target;
    if (!memory || memory.archivedAt) {
      return NextResponse.json(
        { error: "not found or already archived" },
        { status: 404 },
      );
    }
    // Forgetting a team memory as a member = suppress it for them only;
    // their own personal rows archive for real.
    if (!memory.userId) {
      try {
        await overrideWorkMemoryForUser({
          orgId,
          userId: actor.userId,
          memoryId: id,
          suppress: true,
        });
        return NextResponse.json({ ok: true, suppressed: true });
      } catch {
        return NextResponse.json({ error: "not found" }, { status: 404 });
      }
    }
    const ok = await archiveWorkMemory(orgId, id, {
      reason,
      userId: actor.userId,
    });
    if (!ok) {
      return NextResponse.json(
        { error: "not found or already archived" },
        { status: 404 },
      );
    }
    return NextResponse.json({ ok: true });
  }

  const ok = await archiveWorkMemory(orgId, id, { reason });
  if (!ok) {
    return NextResponse.json(
      { error: "not found or already archived" },
      { status: 404 },
    );
  }
  return NextResponse.json({ ok: true });
}
