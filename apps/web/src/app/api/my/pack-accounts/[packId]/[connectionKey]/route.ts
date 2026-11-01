import { NextResponse } from "next/server";
import { beginPackUserConnection, disconnectPackUserConnection } from "@neko/llm/graphjin/pack-user-connections";
import { personalPackActor } from "@/lib/personal-pack-accounts";
import { newPkceVerifier, newStateToken, pkceChallenge } from "@/lib/integrations";
import { packOAuthCallbackUri, writePackOAuthState } from "@/lib/pack-oauth";
import { validPackId } from "@/lib/solution-packs";
import { holdsItem } from "@/lib/entitlements";

type Context = { params: Promise<{ packId: string; connectionKey: string }> };
async function manage(request: Request, context: Context) {
  try {
    const actor = await personalPackActor();
    const { packId, connectionKey } = await context.params;
    if (!validPackId(packId) || !validPackId(connectionKey)) throw new Error("Invalid pack connection");
    if (request.method !== "DELETE" && !(await holdsItem("pack", packId))) return NextResponse.json({ error: "Pack not found" }, { status: 404 });
    const redirectUri = packOAuthCallbackUri(request, packId, connectionKey);
    if (request.headers.get("origin") !== new URL(redirectUri).origin) return NextResponse.json({ error: "Invalid request origin" }, { status: 403 });
    if (request.method === "DELETE") {
      await disconnectPackUserConnection(actor, packId, connectionKey);
      return NextResponse.json({ disconnected: true });
    }
    const state = newStateToken();
    const codeVerifier = newPkceVerifier();
    const result = await beginPackUserConnection(actor, packId, connectionKey, { state, codeChallenge: pkceChallenge(codeVerifier), redirectUri });
    await writePackOAuthState({ packId, connectionKey, state, codeVerifier, returnPath: new URL(request.url).searchParams.get("returnTo") === "/chat/connectors" ? "/chat/connectors" : "/integrations", ...actor, personal: true });
    return NextResponse.json(result, { headers: { "Cache-Control": "private, no-store" } });
  } catch (error) {
    return NextResponse.json({ error: error instanceof Error ? error.message : "Connection failed" }, { status: 400 });
  }
}
export const POST = manage;
export const DELETE = manage;
