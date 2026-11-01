import { NextRequest, NextResponse } from "next/server";
import { getCurrentUser } from "@/lib/auth";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import {
  DEPLOYMENT_CONNECT_OPERATOR_ID,
  disconnectConnector,
  listConnectProviders,
} from "@/lib/integrations";

/**
 * POST /api/integrations/disconnect/[plugin] — drop the current
 * operator's credential. Deployment-scoped connectors drop the shared
 * org-wide credential instead, and that path is admin-gated.
 */
export async function POST(
  _request: NextRequest,
  { params }: { params: Promise<{ plugin: string }> },
) {
  const { plugin } = await params;
  const pluginName = decodeURIComponent(plugin);
  if (!pluginName) {
    return NextResponse.json({ error: "plugin param required" }, { status: 400 });
  }
  const user = await getCurrentUser();
  const providers = await listConnectProviders();
  const provider = providers.find((p) => p.pluginName === pluginName);
  if (provider?.credentialScope === "deployment") {
    // Org-level credential: admin-gated, and allowed in solo mode (no
    // session yet — the admin may disconnect before SSO is configured).
    const actor = await requireAdminActor();
    if (isDenied(actor)) return actor;
  } else if (!user) {
    return NextResponse.json({ error: "not signed in" }, { status: 401 });
  }
  try {
    const operatorId =
      provider?.credentialScope === "deployment"
        ? DEPLOYMENT_CONNECT_OPERATOR_ID
        : (user?.id ?? "");
    const removed = await disconnectConnector(pluginName, operatorId);
    return NextResponse.json({ removed });
  } catch (err) {
    return NextResponse.json(
      { error: err instanceof Error ? err.message : String(err) },
      { status: 502 },
    );
  }
}

