import { redirect } from "next/navigation";

type PageProps = {
  params: Promise<{ policyId: string }>;
};

export default async function AdminSettingsRuleRedirectPage({
  params,
}: PageProps) {
  const { policyId } = await params;
  redirect(`/admin/rules/${policyId}`);
}
