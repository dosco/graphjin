import { getCurrentUser, getAuthProvider } from "@/lib/auth";
import { personalConnectionsFixture, pluginConnectionsFixture } from "./visual-fixture";
import { connection } from "next/server";
import { redirect } from "next/navigation";
import { getCurrentActor } from "@/lib/actor";
import { loadIntegrations } from "./load";
import IntegrationsList from "./IntegrationsList";

export default async function IntegrationsPage({ searchParams }: { searchParams: Promise<{ state?: string }> }) {
  await connection();
  const query = await searchParams;
  if (process.env.NODE_ENV !== "production" && process.env.OPENNEKO_RECORDS_VISUAL_TEST === "true" && query.state === "plugins") return <IntegrationsList key="preview-plugins" preview initial={pluginConnectionsFixture} />;
  if (process.env.NODE_ENV !== "production" && process.env.OPENNEKO_RECORDS_VISUAL_TEST === "true" && ["connected", "unconfigured", "disconnected"].includes(query.state ?? "")) return <IntegrationsList key={`preview-${query.state}`} preview initial={{ workspace: [], connectors: [], personal: await personalConnectionsFixture(query.state!) }} isAdmin={false} />;
  const actor = await getCurrentActor();
  const user = await getCurrentUser();
  if (!user && await getAuthProvider()) {
    redirect("/signin?returnTo=/integrations");
  }
  const { workspace, connectors, personal } = await loadIntegrations(actor, user);
  return <IntegrationsList key="live" initial={{ workspace, connectors, personal }} isAdmin={actor.role === "admin"} />;
}
