import Link from "next/link";
import { connection } from "next/server";
import { pool } from "@neko/db";
import { listWorkflows } from "@neko/llm/workflows";
import AppHeader from "@/components/AppHeader";
import PageHeading from "@/components/PageHeading";
import SectionNav from "@/components/SectionNav";
import { Button } from "@/components/ui/button";
import { AdminDenied } from "@/app/admin/AdminShell";
import { WorkflowApiAccessPanel } from "@/app/(work)/workflows/WorkflowApiAccessPanel";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { getPublicUrlSetting } from "@/lib/public-url-settings";
import PublicUrlForm from "./PublicUrlForm";
import WorkflowOrgLimitsForm from "./WorkflowOrgLimitsForm";

export default async function WorkflowSettingsPage({
  searchParams,
}: {
  searchParams: Promise<{ workflow?: string }>;
}) {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;
  const orgId = await getOrgId();
  const [workflows, orgLimits, publicUrl] = await Promise.all([
    listWorkflows(orgId),
    pool().query<{ rolling_token_budget: number; rolling_cost_micros_budget: string }>(
      "select rolling_token_budget, rolling_cost_micros_budget from workflow_api_org_limits where org_id = $1",
      [orgId],
    ),
    getPublicUrlSetting(orgId),
  ]);
  const selected = (await searchParams).workflow;
  const workflow = workflows.find((item) => item.id === selected) ?? workflows[0];
  const limits = orgLimits.rows[0];

  return (
    <div className="root">
      <AppHeader back={{ href: "/admin/settings", label: "Settings" }}>
        <SectionNav current="admin" />
      </AppHeader>
      <PageHeading
        title="Workflow API"
        description="Set the public URL, the organization budget, and each workflow's API execution limits. Runtime also sets the agent's time allowance for API runs."
      />
      <PublicUrlForm initial={publicUrl} />
      <WorkflowOrgLimitsForm initial={{
        rollingTokenBudget: limits?.rolling_token_budget ?? Number(process.env.OPENNEKO_WORKFLOW_API_ORG_ROLLING_TOKEN_BUDGET ?? 1_000_000),
        rollingCostMicrosBudget: Number(limits?.rolling_cost_micros_budget ?? process.env.OPENNEKO_WORKFLOW_API_ORG_ROLLING_COST_MICROS_BUDGET ?? 50_000_000),
      }} />
      <section className="settings-card mt-6 grid gap-4">
        <div>
          <h2 className="settings-card-title">Per-workflow limits</h2>
          <p className="settings-card-copy">Select a workflow to set its runtime, call, token, and artifact limits.</p>
        </div>
        {workflows.length === 0 ? (
          <p className="text-text2">Create a workflow before setting its API limits.</p>
        ) : (
          <nav aria-label="Choose workflow" className="flex flex-wrap gap-2">
            {workflows.map((item) => (
              <Button
                key={item.id}
                asChild
                size="sm"
                variant={item.id === workflow?.id ? "primary" : "secondary"}
              >
                <Link
                  href={`/admin/settings/workflows?workflow=${item.id}`}
                  aria-current={item.id === workflow?.id ? "page" : undefined}
                >
                  {item.name}
                </Link>
              </Button>
            ))}
          </nav>
        )}
        {workflow ? (
          <div className="settings-workflow-api">
            <WorkflowApiAccessPanel key={workflow.id} workflowId={workflow.id} />
          </div>
        ) : null}
      </section>
    </div>
  );
}
