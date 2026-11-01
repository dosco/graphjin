import { connection } from "next/server";
import { AdminDenied } from "@/app/admin/AdminShell";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { getInstallPolicyPayload } from "@/lib/install-policy-settings";
import SecurityForm from "./SecurityForm";

export default async function SettingsSecurityPage() {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;

  const orgId = await getOrgId();
  const payload = await getInstallPolicyPayload(orgId);
  return <SecurityForm initial={payload} />;
}
