import { NextResponse } from "next/server";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { packOAuthCallbackUri } from "@/lib/pack-oauth";
import { requestPackWorker, validPackId } from "@/lib/solution-packs";

type Context = { params: Promise<{ packId: string; connectionKey: string }> };

async function params(context: Context) {
  const value = await context.params;
  return validPackId(value.packId) && validPackId(value.connectionKey) ? value : null;
}

export async function GET(request: Request, context: Context) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const value = await params(context);
  if (!value) return NextResponse.json({ error: "invalid pack OAuth connection" }, { status: 400 });
  const result = await requestPackWorker(`/admin/packs/${encodeURIComponent(value.packId)}/oauth/${encodeURIComponent(value.connectionKey)}/status`);
  const body = result.body && typeof result.body === "object" ? result.body as Record<string, unknown> : {};
  return NextResponse.json({ ...body, callbackUrl: packOAuthCallbackUri(request, value.packId, value.connectionKey) }, { status: result.status });
}

export async function DELETE(_request: Request, context: Context) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const value = await params(context);
  if (!value) return NextResponse.json({ error: "invalid pack OAuth connection" }, { status: 400 });
  const result = await requestPackWorker(`/admin/packs/${encodeURIComponent(value.packId)}/oauth/${encodeURIComponent(value.connectionKey)}/disconnect`, { method: "POST", body: "{}" });
  return NextResponse.json(result.body, { status: result.status });
}

export async function POST(request: Request, context: Context) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const value = await params(context);
  if (!value) return NextResponse.json({ error: "Invalid pack connection" }, { status: 400 });
  const body = await request.json().catch(() => null);
  if (!body || typeof body.clientId !== "string") return NextResponse.json({ error: "Client ID is required" }, { status: 400 });
  const result = await requestPackWorker(`/admin/packs/${encodeURIComponent(value.packId)}/oauth/${encodeURIComponent(value.connectionKey)}/configure`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ clientId: body.clientId, clientSecret: body.clientSecret }) });
  return NextResponse.json(result.body, { status: result.status });
}
