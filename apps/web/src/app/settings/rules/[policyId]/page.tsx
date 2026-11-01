import { redirect } from "next/navigation";

type PageProps = {
  params: Promise<{ policyId: string }>;
};

export default async function SettingsRuleRedirectPage({ params }: PageProps) {
  const { policyId } = await params;
  redirect(`/admin/rules/${policyId}`);
}
