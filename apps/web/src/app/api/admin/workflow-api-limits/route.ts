import { NextResponse } from "next/server";
import { pool } from "@neko/db";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

async function readLimits(orgId: string) {
  const { rows } = await pool().query<{
    rolling_token_budget: number;
    rolling_cost_micros_budget: string;
  }>(
    "select rolling_token_budget, rolling_cost_micros_budget from workflow_api_org_limits where org_id = $1",
    [orgId],
  );
  return {
    rollingTokenBudget: rows[0]?.rolling_token_budget ?? Number(process.env.OPENNEKO_WORKFLOW_API_ORG_ROLLING_TOKEN_BUDGET ?? 1_000_000),
    rollingCostMicrosBudget: Number(rows[0]?.rolling_cost_micros_budget ?? process.env.OPENNEKO_WORKFLOW_API_ORG_ROLLING_COST_MICROS_BUDGET ?? 50_000_000),
  };
}

export async function GET() {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  return NextResponse.json({ limits: await readLimits(await getOrgId()) });
}

export async function PUT(request: Request) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const body = await request.json().catch(() => null);
  const rollingTokenBudget = body?.rollingTokenBudget;
  const rollingCostMicrosBudget = body?.rollingCostMicrosBudget;
  if (
    !Number.isSafeInteger(rollingTokenBudget) || rollingTokenBudget < 1_000 || rollingTokenBudget > 100_000_000 ||
    !Number.isSafeInteger(rollingCostMicrosBudget) || rollingCostMicrosBudget < 1_000 || rollingCostMicrosBudget > 10_000_000_000
  ) {
    return NextResponse.json({ error: "Enter a token budget from 1,000 to 100,000,000 and a spend budget from $0.001 to $10,000." }, { status: 400 });
  }
  const orgId = await getOrgId();
  await pool().query(
    `insert into workflow_api_org_limits (org_id, rolling_token_budget, rolling_cost_micros_budget)
     values ($1, $2, $3)
     on conflict (org_id) do update set
       rolling_token_budget = excluded.rolling_token_budget,
       rolling_cost_micros_budget = excluded.rolling_cost_micros_budget,
       updated_at = now()`,
    [orgId, rollingTokenBudget, rollingCostMicrosBudget],
  );
  return NextResponse.json({ limits: await readLimits(orgId) });
}
