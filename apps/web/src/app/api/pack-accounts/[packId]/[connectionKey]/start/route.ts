import { getOrgId } from "@neko/db";
import { NextRequest, NextResponse } from "next/server";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { newPkceVerifier, newStateToken, pkceChallenge } from "@/lib/integrations";
import { packOAuthCallbackUri, writePackOAuthState } from "@/lib/pack-oauth";
import { requestPackWorker, validPackId } from "@/lib/solution-packs";

export async function POST(request: NextRequest, context: { params: Promise<{ packId: string; connectionKey: string }> }) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const { packId, connectionKey } = await context.params;
  if (!validPackId(packId) || !validPackId(connectionKey)) return NextResponse.json({ error: "invalid pack OAuth connection" }, { status: 400 });
  const body = await request.json().catch(() => null) as Record<string, unknown> | null;
  const clientId = typeof body?.clientId === "string" ? body.clientId : "";
  const clientSecret = typeof body?.clientSecret === "string" ? body.clientSecret : "";
  if (!clientId) return NextResponse.json({ error: "clientId is required" }, { status: 400 });
  const state = newStateToken();
  const codeVerifier = newPkceVerifier();
  const returnPath = typeof body?.returnTo === "string" && body.returnTo.startsWith("/") && !body.returnTo.startsWith("//")
    ? body.returnTo : "/admin/settings/packs";
  const redirectUri = packOAuthCallbackUri(request, packId, connectionKey);
  await writePackOAuthState({ packId, connectionKey, state, codeVerifier, returnPath, userId: actor.userId ?? undefined, orgId: await getOrgId() });
  const result = await requestPackWorker(`/admin/packs/${encodeURIComponent(packId)}/oauth/${encodeURIComponent(connectionKey)}/begin`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ clientId, clientSecret, redirectUri, state, codeChallenge: pkceChallenge(codeVerifier) }),
  });
  return NextResponse.json(result.body, { status: result.status });
}
