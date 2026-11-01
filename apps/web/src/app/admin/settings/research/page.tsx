import { connection } from "next/server";
import { AdminDenied } from "@/app/admin/AdminShell";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { getProviderSettingsPayload } from "@/lib/provider-settings";
import ResearchForm from "./ResearchForm";

export default async function SettingsResearchPage() {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;

  const providers = await getProviderSettingsPayload((await getOrgId()));
  return <ResearchForm initial={providers} />;
}
