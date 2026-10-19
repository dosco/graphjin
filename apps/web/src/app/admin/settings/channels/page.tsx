import { connection } from "next/server";
import { AdminDenied, AdminShell } from "@/app/admin/AdminShell";
import { getCurrentActor } from "@/lib/actor";
import ChannelSettingsPanel from "./ChannelSettingsPanel";

export default async function SettingsChannelsPage() {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;

  return (
    <AdminShell
      title="Channels"
      subtitle="Slack, Telegram, and other places where people talk to OpenNeko. A solution pack turns a channel on."
      back={{ href: "/admin/settings", label: "Settings" }}
    >
      <ChannelSettingsPanel />
    </AdminShell>
  );
}
