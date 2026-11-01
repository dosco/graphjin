import { NextResponse } from "next/server";
import { getOrgId } from "@/lib/db";
import { listWorkAssets } from "@/lib/work-files";

export async function GET() {
  const assets = await listWorkAssets(await getOrgId());
  return NextResponse.json(assets);
}
