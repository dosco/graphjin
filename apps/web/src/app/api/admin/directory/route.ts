import { NextResponse } from "next/server";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { requestWorker } from "@/lib/groups-admin";

export async function GET() {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const result = await requestWorker("/admin/directory/status").catch(() => ({ status: 503, body: { error: "worker is unavailable" } }));
  return NextResponse.json(result.body, { status: result.status });
}

export async function POST() {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const result = await requestWorker("/admin/directory/sync", {}).catch(() => ({ status: 503, body: { error: "worker is unavailable" } }));
  return NextResponse.json(result.body, { status: result.status });
}
