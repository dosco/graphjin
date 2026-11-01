import { NextResponse } from "next/server";
import pkg from "../../../../package.json";

export const dynamic = "force-dynamic";

export function GET() {
  return NextResponse.json(
    { version: pkg.version },
    { headers: { "Cache-Control": "no-store" } },
  );
}
