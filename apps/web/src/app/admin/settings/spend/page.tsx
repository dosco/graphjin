import { connection } from "next/server";
import { getSpendSettings } from "@neko/llm/spend";
import { AdminDenied } from "@/app/admin/AdminShell";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import SpendForm from "./SpendForm";

export default async function SettingsSpendPage() {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;

  return <SpendForm initial={await getSpendSettings(await getOrgId())} />;
}
