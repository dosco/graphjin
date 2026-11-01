import { connection } from "next/server";
import { notFound } from "next/navigation";
import { isItemType } from "@neko/db";
import { ItemAccessPanel } from "@/components/admin/ItemAccessPanel";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { ITEM_TYPE_LABELS, listItemOptions } from "@/lib/groups-admin";
import { AdminDenied, AdminShell } from "../AdminShell";

export default async function ItemAccessPage({ searchParams }: { searchParams: Promise<{ type?: string; id?: string }> }) {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;
  const { type, id } = await searchParams;
  if (!isItemType(type) || !id) notFound();
  const option = (await listItemOptions(await getOrgId(), type).catch(() => [])).find((item) => item.id === id);
  const label = option?.label ?? id;
  return (
    <AdminShell
      title={`${ITEM_TYPE_LABELS[type].label}: ${label}`}
      subtitle={option?.detail ?? "Choose which groups hold this item."}
      back={{ href: "/admin/users?tab=groups", label: "Groups" }}
    >
      <ItemAccessPanel itemType={type} itemId={id} label={label} />
    </AdminShell>
  );
}
