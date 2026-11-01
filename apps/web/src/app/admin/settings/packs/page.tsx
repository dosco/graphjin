import { connection } from "next/server";
import { AdminDenied } from "@/app/admin/AdminShell";
import { getCurrentActor } from "@/lib/actor";
import AppHeader from "@/components/AppHeader";
import PageHeading from "@/components/PageHeading";
import CustomPacksAdmin from "./CustomPacksAdmin";

export default async function SettingsPacksPage({ searchParams }: { searchParams: Promise<{ pack?: string; connected?: string }> }) {
  await connection();
  const query = await searchParams;
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;
  const initialPack = /^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/.test(query.pack ?? "") ? query.pack : undefined;
  return <div className="root is-narrow">
    <AppHeader back={{ href: "/admin/settings", label: "Settings" }} />
    <PageHeading title="Packs" description="Install included packs, then configure their connections and capabilities." />
    <CustomPacksAdmin initialPack={initialPack} connected={query.connected} />
  </div>;
}
