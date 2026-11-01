import { connection } from "next/server";
import { AdminDenied } from "@/app/admin/AdminShell";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { getAgentSettingsPayload } from "@/lib/agent-backend-settings";
import { getGraphjinAgentSettingsPayload } from "@/lib/graphjin-agent-settings";
import { getProviderSettingsPayload } from "@/lib/provider-settings";
import AgentForm from "./AgentForm";

export default async function SettingsAgentPage() {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;

  const orgId = await getOrgId();
  const [agent, providers, graphjinAgent] = await Promise.all([
    getAgentSettingsPayload(orgId),
    getProviderSettingsPayload(orgId),
    getGraphjinAgentSettingsPayload(orgId),
  ]);
  return <AgentForm initial={{ agent, providers, graphjinAgent }} />;
}
