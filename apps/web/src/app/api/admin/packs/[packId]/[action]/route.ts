import { randomUUID } from "node:crypto";
import { NextRequest, NextResponse } from "next/server";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import {
  isPackReadAction,
  isPackWriteAction,
  requestPackWorker,
  readPackRequest,
  validPackId,
} from "@/lib/solution-packs";

type RouteContext = {
  params: Promise<{ packId: string; action: string }>;
};

const MAX_BODY_BYTES = 64 * 1024;

export async function GET(request: NextRequest, context: RouteContext) {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  const { packId, action } = await context.params;
  if (!validPackId(packId) || !isPackReadAction(action)) {
    return NextResponse.json({ error: "unknown pack operation" }, { status: 404 });
  }
  try {
    const version = request.nextUrl.searchParams.get("version");
    const result = await requestPackWorker(
      `/admin/packs/${encodeURIComponent(packId)}/${action}${version ? `?version=${encodeURIComponent(version)}` : ""}`,
    );
    return NextResponse.json(result.body, { status: result.status });
  } catch (error) {
    return NextResponse.json(
      { error: error instanceof Error ? error.message : String(error) },
      { status: 502 },
    );
  }
}

export async function POST(request: NextRequest, context: RouteContext) {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  const { packId, action } = await context.params;
  if (!validPackId(packId) || !isPackWriteAction(action)) {
    return NextResponse.json({ error: "unknown pack operation" }, { status: 404 });
  }
  const declaredLength = Number(request.headers.get("content-length") ?? "0");
  if (Number.isFinite(declaredLength) && declaredLength > MAX_BODY_BYTES) {
    return NextResponse.json({ error: "request is too large" }, { status: 413 });
  }
  let parsed: unknown;
  try {
    const raw = (await readPackRequest(request, MAX_BODY_BYTES)).toString("utf8");
    parsed = raw ? JSON.parse(raw) as unknown : {};
  } catch (error) {
    if (error instanceof Error && error.message === "request is too large") return NextResponse.json({ error: error.message }, { status: 413 });
    return NextResponse.json({ error: "request body must be valid JSON" }, { status: 400 });
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    return NextResponse.json({ error: "request body must be an object" }, { status: 400 });
  }
  try {
    const body = {
      ...(parsed as Record<string, unknown>),
      actorUserId: allowed.userId,
      idempotencyKey:
        typeof (parsed as Record<string, unknown>).idempotencyKey === "string"
          ? (parsed as Record<string, unknown>).idempotencyKey
          : randomUUID(),
    };
    const result = await requestPackWorker(
      `/admin/packs/${encodeURIComponent(packId)}/${action}`,
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      },
    );
    return NextResponse.json(result.body, { status: result.status });
  } catch (error) {
    return NextResponse.json(
      { error: error instanceof Error ? error.message : String(error) },
      { status: 502 },
    );
  }
}
