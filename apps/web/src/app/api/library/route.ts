import { NextResponse } from "next/server";
import { browseLibrary, parseLibraryBrowseOptions } from "@neko/llm/work";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/** The current actor's Library documents. */
export async function GET(request: Request) {
  let options;
  try {
    options = parseLibraryBrowseOptions(new URL(request.url).searchParams);
  } catch {
    return NextResponse.json({ error: "Invalid library filters." }, { status: 400 });
  }
  const [orgId, actor] = await Promise.all([getOrgId(), getCurrentActor()]);
  try {
    return NextResponse.json(await browseLibrary({ orgId, userId: actor.userId }, options));
  } catch {
    return NextResponse.json({ error: "Library could not be loaded. Try again." }, { status: 503 });
  }
}
