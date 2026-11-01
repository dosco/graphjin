import { NextResponse } from "next/server";
import { getCurrentUser } from "@/lib/auth";
import { heldItemIds } from "@/lib/entitlements";
import {
  getDeploymentConnectStatus,
  getOperatorConnectStatus,
  listConnectProviders,
} from "@/lib/integrations";

/**
 * GET /api/integrations/list — combined view of installed connect
 * plugins + connection status. Deployment-scoped connectors surface in
 * a separate "workspace" section (one org-wide credential); the rest
 * are per-operator. Used by the /integrations page in one round-trip.
 */
export async function GET() {
  const user = await getCurrentUser();
  if (!user) {
    return NextResponse.json({ error: "not signed in" }, { status: 401 });
  }
  const [providers, status, deploymentStatus] = await Promise.all([
    listConnectProviders(),
    getOperatorConnectStatus(user.id),
    getDeploymentConnectStatus(),
  ]);
  const heldIntegrations = await heldItemIds("integration");
  const held = (pluginName: string) => heldIntegrations === "*" || heldIntegrations.has(pluginName);
  const connectedByPlugin = new Map(status.map((s) => [s.pluginName, s]));
  const deploymentConnectedByPlugin = new Map(
    deploymentStatus.map((s) => [s.pluginName, s]),
  );
  const workspace = providers
    .filter((p) => p.credentialScope === "deployment" && held(p.pluginName))
    .map((p) => ({
      pluginId: p.pluginId,
      pluginName: p.pluginName,
      providerLabel: p.providerLabel,
      scopes: p.scopes,
      flow: p.flow,
      credentialScope: p.credentialScope,
      connected: deploymentConnectedByPlugin.has(p.pluginName),
      connectedAt:
        deploymentConnectedByPlugin.get(p.pluginName)?.connectedAt ?? null,
    }));
  const connectors = providers
    .filter((p) => p.credentialScope !== "deployment" && held(p.pluginName))
    .map((p) => ({
      pluginId: p.pluginId,
      pluginName: p.pluginName,
      providerLabel: p.providerLabel,
      scopes: p.scopes,
      flow: p.flow,
      credentialScope: p.credentialScope,
      connected: connectedByPlugin.has(p.pluginName),
      connectedAt: connectedByPlugin.get(p.pluginName)?.connectedAt ?? null,
    }));
  return NextResponse.json({ workspace, connectors });
}
