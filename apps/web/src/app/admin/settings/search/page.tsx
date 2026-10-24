import { connection } from "next/server";
import { getSemanticSearchSetting } from "@neko/llm/semantic-search";
import { AdminDenied } from "@/app/admin/AdminShell";
import { getCurrentActor } from "@/lib/actor";
import SemanticSearchForm from "./SemanticSearchForm";

export default async function SettingsSearchPage() {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;
  return <SemanticSearchForm initial={await getSemanticSearchSetting()} />;
}
