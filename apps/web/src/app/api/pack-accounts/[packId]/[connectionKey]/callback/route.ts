import { completePackUserConnection } from "@neko/llm/graphjin/pack-user-connections";
import { personalPackActor } from "@/lib/personal-pack-accounts";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@neko/db";
import { NextRequest, NextResponse } from "next/server";
import { packOAuthCallbackUri, readAndClearPackOAuthState } from "@/lib/pack-oauth";
import { requestPackWorker } from "@/lib/solution-packs";

export async function GET(request: NextRequest, context: { params: Promise<{ packId: string; connectionKey: string }> }) {
  const { packId, connectionKey } = await context.params;
  const saved = await readAndClearPackOAuthState();
  const code = request.nextUrl.searchParams.get("code") ?? "";
  const state = request.nextUrl.searchParams.get("state") ?? "";
  if (!saved || saved.packId !== packId || saved.connectionKey !== connectionKey || saved.state !== state || !code) {
    return NextResponse.json({ error: "OAuth callback state is missing or invalid" }, { status: 400 });
  }
  if (saved.personal) {
    try {
      const actor = await personalPackActor();
      if (actor.userId !== saved.userId || actor.orgId !== saved.orgId) throw new Error("Sign in as the user who started this connection");
      await completePackUserConnection(actor, packId, connectionKey, { code, state, codeVerifier: saved.codeVerifier, redirectUri: packOAuthCallbackUri(request, packId, connectionKey) });
      return NextResponse.redirect(new URL("/integrations?connected=" + encodeURIComponent(connectionKey), packOAuthCallbackUri(request, packId, connectionKey)));
    } catch (error) {
      const target = new URL("/integrations", packOAuthCallbackUri(request, packId, connectionKey));
      target.searchParams.set("error", error instanceof Error ? error.message : "Connection failed");
      return NextResponse.redirect(target);
    }
  }
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  if ((actor.userId ?? undefined) !== saved.userId || await getOrgId() !== saved.orgId) return NextResponse.json({ error: "OAuth session changed" }, { status: 403 });
  const result = await requestPackWorker(`/admin/packs/${encodeURIComponent(packId)}/oauth/${encodeURIComponent(connectionKey)}/complete`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ code, codeVerifier: saved.codeVerifier, redirectUri: packOAuthCallbackUri(request, packId, connectionKey) }),
  });
  if (result.status !== 200) return NextResponse.json(result.body, { status: result.status });
  const target = new URL(saved.returnPath, packOAuthCallbackUri(request, packId, connectionKey));
  target.searchParams.set("pack", packId);
  target.searchParams.set("connected", connectionKey);
  return NextResponse.redirect(target);
}
