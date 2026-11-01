import { NextResponse } from "next/server";
import { confirmContextRemoteHostKey, ContextRemoteError } from "@neko/llm/config-vcs";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/** The administrator confirms the SSH server host key fingerprints shown on the page. */
export async function POST() {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  try {
    return NextResponse.json({ remote: await confirmContextRemoteHostKey(await getOrgId(), actor.userId) });
  } catch (error) {
    if (error instanceof ContextRemoteError) return NextResponse.json({ error: error.message }, { status: 400 });
    throw error;
  }
}
