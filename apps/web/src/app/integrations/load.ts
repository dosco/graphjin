import { getOrgId } from "@neko/db";
import { listPackUserConnections } from "@neko/llm/graphjin/pack-user-connections";
import type { getCurrentActor } from "@/lib/actor";
import type { getCurrentUser } from "@/lib/auth";
import { heldItemIds } from "@/lib/entitlements";
import {
  getDeploymentConnectStatus,
  getOperatorConnectStatus,
  listConnectProviders,
} from "@/lib/integrations";

/** The connections the current person can see: org-wide ones for admins, and their own. */
export async function loadIntegrations(
  actor: Awaited<ReturnType<typeof getCurrentActor>>,
  user: Awaited<ReturnType<typeof getCurrentUser>>,
) {
  const [providers, status, deploymentStatus] = await Promise.all([
    actor.role === "admin" ? listConnectProviders() : Promise.resolve([]),
    actor.userId ? getOperatorConnectStatus(actor.userId) : Promise.resolve([]),
    actor.role === "admin" ? getDeploymentConnectStatus() : Promise.resolve([]),
  ]);
  const connectedByPlugin = new Map(status.map((s) => [s.pluginName, s]));
  const deploymentConnectedByPlugin = new Map(
    deploymentStatus.map((s) => [s.pluginName, s]),
  );
  const rowFor = (
    p: (typeof providers)[number],
    map: Map<string, { connectedAt: string; scopes?: string[] }>,
  ) => ({
    pluginId: p.pluginId,
    pluginName: p.pluginName,
    providerLabel: p.providerLabel,
    scopes: p.scopes,
    flow: p.flow,
    credentialScope: p.credentialScope,
    connected: map.has(p.pluginName),
    connectedAt: map.get(p.pluginName)?.connectedAt ?? null,
  });
  const workspace = providers
    .filter((p) => p.credentialScope === "deployment")
    .map((p) => rowFor(p, deploymentConnectedByPlugin));
  const connectors = providers
    .filter((p) => p.credentialScope !== "deployment")
    .map((p) => rowFor(p, connectedByPlugin));
  const heldPacks = await heldItemIds("pack");
  const personal = (user ? await listPackUserConnections({ orgId: await getOrgId(), userId: user.id }) : [])
    .filter((connection) => heldPacks === "*" || heldPacks.has(connection.packId));
  return { workspace, connectors, personal };
}
