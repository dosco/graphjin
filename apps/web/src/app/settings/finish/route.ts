import { NextResponse } from "next/server";
import { revalidatePath } from "next/cache";
import { db, eq, organization } from "@neko/db";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import { hasDataSourceSetup } from "@/lib/data-source-settings";
import { hasPrimaryProviderSetup } from "@/lib/provider-settings";
import {
  AGENT_RUNTIME_UNAVAILABLE_CODE,
  AgentRuntimeUnavailableError,
  requireAgentRuntimeReady,
} from "@/lib/agent-runtime-readiness";

/**
 * Marks admin setup complete on the org. Data, provider configuration, and a
 * live check that openneko serve answers are re-checked server-side so neither a
 * direct API call nor a fresh-install race can send users into onboarding with
 * an unusable agent. Research remains optional.
 */
export async function POST() {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;

  const orgId = await getOrgId();
  const [dataReady, primaryReady] = await Promise.all([
    hasDataSourceSetup(orgId),
    hasPrimaryProviderSetup(orgId),
  ]);
  if (!dataReady) {
    return NextResponse.json(
      { error: "Data source not configured." },
      { status: 400 },
    );
  }
  if (!primaryReady) {
    return NextResponse.json(
      { error: "Primary model provider not configured." },
      { status: 400 },
    );
  }

  try {
    await requireAgentRuntimeReady(orgId);
  } catch (error) {
    if (error instanceof AgentRuntimeUnavailableError) {
      return NextResponse.json(
        { error: error.message, code: AGENT_RUNTIME_UNAVAILABLE_CODE },
        { status: 503 },
      );
    }
    throw error;
  }

  await db()
    .update(organization)
    .set({ setup_complete_at: new Date(), updated_at: new Date() })
    .where(eq(organization.id, orgId));

  // Invalidate cached RSC payloads on the gates so the next navigation
  // sees the fresh setup_complete_at value. Wrapped because tests call
  // this handler outside Next's request context, where revalidatePath
  // throws "static generation store missing".
  try {
    revalidatePath("/onboarding");
    revalidatePath("/settings");
    revalidatePath("/admin/settings");
  } catch {}

  return NextResponse.json({ ok: true });
}
