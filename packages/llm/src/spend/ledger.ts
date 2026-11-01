import { pool } from "@neko/db";
import type { NormalizedUsage } from "@neko/telemetry";
import { spendWindows, type SpendSource } from "./admission";
import { checkSpendWarnings, raiseSpendAlert } from "./alerts";
import { loadSpendLimits, microsToUsd, SpendLimitsMissing, usdToMicros, type SpendQueryable } from "./limits";

export type SpendPricing = {
  costMicros: number;
  priced: "billed" | "estimated" | "included" | "fallback";
};

/**
 * Billed cost wins, then an estimate. A usage without a price, including an
 * `unknown` cost status, is charged the full per-run cap so it never counts as zero.
 */
export function priceUsage(usage: NormalizedUsage, runCapMicros: number): SpendPricing {
  if (usage.costStatus !== "unknown") {
    if (typeof usage.billedCostUsd === "number" && Number.isFinite(usage.billedCostUsd)) {
      return { costMicros: usdToMicros(usage.billedCostUsd), priced: "billed" };
    }
    if (typeof usage.estimatedCostUsd === "number" && Number.isFinite(usage.estimatedCostUsd)) {
      return {
        costMicros: usdToMicros(usage.estimatedCostUsd),
        priced: usage.costStatus === "included" ? "included" : "estimated",
      };
    }
  }
  return { costMicros: runCapMicros, priced: "fallback" };
}

type Reservation = { id: string; source: SpendSource; workflow_id: string | null };

export async function recordUsageSpend(input: {
  orgId: string;
  usage: NormalizedUsage;
  workRunId?: string | null;
  reservationId?: string | null;
  source?: SpendSource;
  workflowId?: string | null;
  provider?: string;
  model?: string;
  client?: SpendQueryable;
  now?: Date;
}): Promise<SpendPricing> {
  const client = input.client ?? pool();
  const { rows } = input.reservationId
    ? await client.query<Reservation>(
        "select id, source, workflow_id from spend_reservation where id = $1",
        [input.reservationId],
      )
    : input.workRunId
      ? await client.query<Reservation>(
          "select id, source, workflow_id from spend_reservation where work_run_id = $1",
          [input.workRunId],
        )
      : { rows: [] as Reservation[] };
  const reservation = rows[0];
  let runCapMicros: number;
  try {
    runCapMicros = (await loadSpendLimits(client, input.orgId)).runCapMicros;
  } catch (error) {
    if (!(error instanceof SpendLimitsMissing)) throw error;
    runCapMicros = usdToMicros(5);
  }
  const pricing = priceUsage(input.usage, runCapMicros);
  const tokens = input.usage.totalTokens;
  await client.query(
    `insert into spend_ledger (
       org_id, reservation_id, work_run_id, workflow_id, source, provider, model,
       cost_micros, tokens, priced, cost_source, pricing_version, created_at
     ) values ($1, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
    [
      input.orgId,
      reservation?.id ?? null,
      input.workRunId ?? null,
      reservation?.workflow_id ?? input.workflowId ?? null,
      reservation?.source ?? input.source ?? "system",
      input.provider ?? null,
      input.model ?? null,
      pricing.costMicros,
      typeof tokens === "number" && Number.isFinite(tokens) ? Math.ceil(tokens) : null,
      pricing.priced,
      input.usage.costSource ?? null,
      input.usage.pricingCatalogVersion ?? null,
      input.now ?? new Date(),
    ],
  );
  try {
    if (pricing.priced === "fallback") {
      const now = input.now ?? new Date();
      const model = `${input.provider ?? "unknown"}/${input.model ?? "unknown"}`;
      await raiseSpendAlert({
        orgId: input.orgId,
        kind: "spend.price_unknown",
        subject: `model:${model}`,
        observedMicros: pricing.costMicros,
        thresholdMicros: pricing.costMicros,
        windowSeconds: 86_400,
        windowStart: spendWindows(now).dayStart,
        message: `OpenNeko has no price for ${model}. Each turn on it is charged the $${microsToUsd(pricing.costMicros).toFixed(2)} per-run cap until a price is added.`,
      });
    }
    await checkSpendWarnings(client, {
      orgId: input.orgId,
      workflowId: reservation?.workflow_id ?? input.workflowId ?? null,
      now: input.now,
    });
  } catch (error) {
    console.warn(`[spend] alert check failed: ${error instanceof Error ? error.message : error}`);
  }
  return pricing;
}
