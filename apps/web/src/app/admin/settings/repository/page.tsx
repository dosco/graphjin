import { connection } from "next/server";
import { getContextRemote } from "@neko/llm/config-vcs";
import { AdminDenied } from "@/app/admin/AdminShell";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import RepositoryForm from "./RepositoryForm";

export default async function SettingsRepositoryPage() {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;

  return <RepositoryForm initial={await getContextRemote(await getOrgId())} />;
}
