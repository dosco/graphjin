import { NextResponse } from "next/server";
import { ContextRemoteError, diffOrgPublishItem, getContextRemote, previewOrgPublish, publishOrgContext } from "@neko/llm/config-vcs";
import { getOrgAgentRoot } from "@neko/llm/work";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/**
 * What a publish would send now: each changed skill, workflow or other item.
 * With ?kind=…&name=…, the diff for that one item.
 */
export async function GET(request: Request) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const orgId = await getOrgId();
  const params = new URL(request.url).searchParams;
  const kind = params.get("kind");
  const name = params.get("name");
  try {
    if (kind && name) return NextResponse.json(await diffOrgPublishItem(orgId, getOrgAgentRoot(orgId), kind, name));
    return NextResponse.json({ items: await previewOrgPublish(orgId, getOrgAgentRoot(orgId)) });
  } catch (error) {
    if (error instanceof ContextRemoteError) return NextResponse.json({ error: error.message }, { status: 400 });
    throw error;
  }
}

export async function POST() {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const orgId = await getOrgId();
  try {
    const result = await publishOrgContext(orgId, getOrgAgentRoot(orgId), actor.userId);
    return NextResponse.json({ result, remote: await getContextRemote(orgId) });
  } catch (error) {
    if (error instanceof ContextRemoteError) {
      return NextResponse.json({ error: error.message, remote: await getContextRemote(orgId) }, { status: 400 });
    }
    throw error;
  }
}
