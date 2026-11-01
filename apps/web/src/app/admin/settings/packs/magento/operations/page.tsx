import { redirect } from "next/navigation";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { AdminDenied } from "@/app/admin/AdminShell";
import { requestPackWorker } from "@/lib/solution-packs";
import { MAGENTO_VISUAL_FIXTURE } from "../../magento-visual-fixture";
import MagentoPackAdmin from "../../MagentoPackAdmin";

export default async function MagentoOperationsPage() {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return <AdminDenied />;
  if (process.env.NODE_ENV !== "production" && process.env.OPENNEKO_MAGENTO_VISUAL_TEST === "true") return <MagentoPackAdmin fixture={MAGENTO_VISUAL_FIXTURE} />;
  const result = await requestPackWorker("/admin/packs/magento/status");
  const status = result.body as { status?: string; configuration?: { required?: boolean } };
  if (result.status !== 200 || status.status !== "installed" || status.configuration?.required) redirect("/admin/settings/packs?pack=magento");
  return <MagentoPackAdmin />;
}
