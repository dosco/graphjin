import { NextResponse } from "next/server";
import {
  ContextRemoteError,
  deleteContextRemote,
  getContextRemote,
  saveContextRemote,
  type ContextRemoteDraft,
} from "@neko/llm/config-vcs";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import { readJsonBody } from "@/lib/groups-admin";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET() {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  return NextResponse.json({ remote: await getContextRemote(await getOrgId()) });
}

export async function PUT(request: Request) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const body = await readJsonBody(request);
  if (!body) return NextResponse.json({ error: "A JSON body is required." }, { status: 400 });
  try {
    const remote = await saveContextRemote(await getOrgId(), actor.userId, body as ContextRemoteDraft);
    return NextResponse.json({ remote });
  } catch (error) {
    if (error instanceof ContextRemoteError) return NextResponse.json({ error: error.message }, { status: 400 });
    throw error;
  }
}

export async function DELETE() {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  await deleteContextRemote(await getOrgId(), actor.userId);
  return NextResponse.json({ remote: null });
}
