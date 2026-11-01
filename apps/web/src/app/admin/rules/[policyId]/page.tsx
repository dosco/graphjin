import { connection } from "next/server";
import { AdminDenied } from "@/app/admin/AdminShell";
import { getCurrentActor } from "@/lib/actor";
import PolicyDetailClient from "./PolicyDetailClient";

type PageProps = {
  params: Promise<{ policyId: string }>;
};

export default async function PolicyDetailPage({ params }: PageProps) {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;

  const { policyId } = await params;
  return <PolicyDetailClient policyId={policyId} />;
}
