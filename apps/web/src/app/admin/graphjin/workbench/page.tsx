import { connection } from "next/server";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { graphjinAdminGet } from "@/lib/graphjin-admin";
import { AdminDenied } from "../../AdminShell";
import { GraphjinShell } from "../GraphjinAdmin";
import WorkbenchClient from "./WorkbenchClient";

type AgentStatus = { ready: boolean; model?: string; read_only: boolean; message?: string };

export default async function AdminGraphjinWorkbenchPage() {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;

  const agent = await graphjinAdminGet<AgentStatus>(
    { orgId: await getOrgId(), userId: actor.userId },
    "agent/status",
  );

  return (
    <GraphjinShell
      current="/admin/graphjin/workbench"
      title="Workbench"
      subtitle="Run GraphQL against GraphJin as an admin, or ask its built-in agent."
    >
      <WorkbenchClient
        agent={
          agent.ok
            ? {
                ready: agent.data.ready,
                detail: agent.data.ready
                  ? [agent.data.model, agent.data.read_only ? "read-only" : "can write"].filter(Boolean).join(" · ")
                  : agent.data.message ?? "The agent is not ready.",
              }
            : { ready: false, detail: agent.error }
        }
      />
    </GraphjinShell>
  );
}
