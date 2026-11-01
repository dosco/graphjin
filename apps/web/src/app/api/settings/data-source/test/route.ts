import { NextRequest, NextResponse } from "next/server";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { testDataSourceDraft } from "@/lib/data-source-settings";

export async function POST(request: NextRequest) {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  try {
    const body = await request.json();
    const result = await testDataSourceDraft({
      graphqlUrl: String(body.graphqlUrl ?? ""),
      mcpUrl: body.mcpUrl == null ? null : String(body.mcpUrl),
    });
    return NextResponse.json(result);
  } catch (e) {
    const message = e instanceof Error ? e.message : String(e);
    return NextResponse.json({ error: message }, { status: 400 });
  }
}
