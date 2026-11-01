import { connection } from "next/server";
import { AdminDenied, AdminShell } from "@/app/admin/AdminShell";
import { getCurrentActor } from "@/lib/actor";
import SignInSetupCard from "./SignInSetupCard";

export default async function SettingsSignInPage() {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;

  return (
    <AdminShell
      narrow
      title="Email-link sign-in"
      subtitle="Passwordless magic-link sign-in for provisioned users."
      back={{ href: "/admin/settings", label: "Settings" }}
    >
      <SignInSetupCard />
    </AdminShell>
  );
}
