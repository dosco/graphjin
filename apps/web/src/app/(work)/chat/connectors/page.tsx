import { connection } from "next/server";
import { getCurrentActor } from "@/lib/actor";
import { getCurrentUser } from "@/lib/auth";
import IntegrationsList from "@/app/integrations/IntegrationsList";
import { loadIntegrations } from "@/app/integrations/load";

export default async function ConnectorsPage() {
  await connection();
  const [actor, user] = await Promise.all([getCurrentActor(), getCurrentUser()]);
  const { connectors, personal } = await loadIntegrations(actor, user);
  // Org-wide connections stay in Admin; this screen holds the person's own.
  return <IntegrationsList variant="chat" initial={{ workspace: [], connectors, personal }} isAdmin={false} canSetUp={actor.role === "admin"} />;
}
