import { connection } from "next/server";
import { AdminDenied } from "@/app/admin/AdminShell";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { getDataSourceSettings } from "@/lib/data-source-settings";
import DataSourceForm from "./DataSourceForm";

export default async function SettingsDataPage() {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;

  const initial = await getDataSourceSettings((await getOrgId()));
  return <DataSourceForm initial={initial} />;
}
