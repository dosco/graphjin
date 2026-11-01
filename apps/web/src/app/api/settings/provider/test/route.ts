import { NextRequest, NextResponse } from "next/server";
import { testPrimaryProvider, testResearchProvider } from "@neko/llm";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";

/**
 * POST /api/settings/provider/test
 *
 * Validates a primary or research provider draft by attempting a real
 * one-shot LLM call against it. Calls @neko/llm in-process — was an HTTP
 * hop to the worker before the LLM-package extraction.
 */
export async function POST(request: NextRequest) {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  try {
    const draft = await request.json();
    if (!draft?.scope) {
      return NextResponse.json({ error: "missing draft scope" }, { status: 400 });
    }
    // Strip blank secret fields the UI may send when the user hasn't typed
    // anything; we don't want them overwriting stored values during a test.
    const normalizedDraft = {
      ...draft,
      secrets: Object.fromEntries(
        Object.entries(draft.secrets ?? {}).filter(
          ([, value]) => typeof value === "string" && value.trim().length > 0,
        ),
      ),
    };
    const result =
      normalizedDraft.scope === "primary"
        ? await testPrimaryProvider((await getOrgId()), normalizedDraft)
        : await testResearchProvider((await getOrgId()), normalizedDraft);
    return NextResponse.json({ ok: true, ...result });
  } catch (e) {
    const message = e instanceof Error ? e.message : String(e);
    return NextResponse.json({ error: message }, { status: 500 });
  }
}
