import { NextResponse } from "next/server";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { readPackRequest, requestPackWorker } from "@/lib/solution-packs";

export async function POST(request: Request) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  if (request.headers.get("content-type")?.split(";", 1)[0]?.trim() !== "application/zip") {
    return NextResponse.json({ error: "Choose a ZIP pack archive." }, { status: 415 });
  }
  let bytes: Buffer;
  try { bytes = await readPackRequest(request, 16 * 1024 * 1024); }
  catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    return NextResponse.json({ error: message }, { status: message === "request is too large" ? 413 : 400 });
  }
  try {
    const result = await requestPackWorker("/admin/packs/upload", {
      method: "POST", headers: { "Content-Type": "application/zip", "X-OpenNeko-Actor": encodeURIComponent(actor.userId ?? "") },
      body: new Uint8Array(bytes), signal: request.signal,
    });
    return NextResponse.json(result.body, { status: result.status });
  } catch (error) {
    return NextResponse.json({ error: error instanceof Error ? error.message : String(error) }, { status: 502 });
  }
}
