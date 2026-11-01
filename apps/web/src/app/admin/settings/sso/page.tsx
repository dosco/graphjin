import { connection } from "next/server";
import { AdminDenied } from "@/app/admin/AdminShell";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { getDeploymentConnectStatus } from "@/lib/integrations";
import { getSsoSetupStatus } from "@/lib/sso-setup";
import SsoSetupForm from "./SsoSetupForm";

export default async function SettingsSsoPage() {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;

  const orgId = await getOrgId();
  const [status, deployment] = await Promise.all([
    getSsoSetupStatus(orgId).catch(
      (err) =>
        ({
          status: "not_started",
          portalLink: null,
          portalLinkExpiresAt: null,
          provider: null,
          connection: null,
          lastError: err instanceof Error ? err.message : String(err),
          setupCompletedAt: null,
          environmentId: null,
          organizationId: null,
          environmentTier: null,
          signInConfigured: false,
        }) as Awaited<ReturnType<typeof getSsoSetupStatus>>,
    ),
    getDeploymentConnectStatus(),
  ]);
  return (
    <SsoSetupForm
      initial={status}
      loadError={null}
      workspaceConnected={deployment.some(
        (d) => d.pluginName === "scalekit",
      )}
    />
  );
}
