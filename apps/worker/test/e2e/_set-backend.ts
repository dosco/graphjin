/**
 * Apply a RunPlan to the throwaway org by upserting both provider rows:
 *   - llm_provider_config (scope='primary')  ← provider/model/apiKey
 *   - llm_provider_config (scope='agent')    ← backend
 *
 * Used between sub-suites when iterating plans against the same org.
 * After this returns, callers should call `provisionHostConfig(orgId)` to
 * push the new state to host config (gateway provider sync and
 * graphjin client.json).
 */

import { and, db, eq, llm_provider_config } from "@neko/db";
import { maybeEncryptSecret } from "@neko/llm/secrets";
import type { RunPlan } from "./_can-run";

async function upsertPrimary(orgId: string, plan: RunPlan): Promise<void> {
  const existing = await db()
    .select({ id: llm_provider_config.id })
    .from(llm_provider_config)
    .where(
      and(
        eq(llm_provider_config.org_id, orgId),
        eq(llm_provider_config.scope, "primary"),
      ),
    )
    .limit(1);

  const secrets = { apiKey: maybeEncryptSecret(plan.apiKey) };

  if (existing[0]) {
    await db()
      .update(llm_provider_config)
      .set({
        provider: plan.primaryProvider,
        model: plan.primaryModel,
        enabled: true,
        secrets,
        updated_at: new Date(),
      })
      .where(eq(llm_provider_config.id, existing[0].id));
  } else {
    await db().insert(llm_provider_config).values({
      org_id: orgId,
      scope: "primary",
      provider: plan.primaryProvider,
      model: plan.primaryModel,
      enabled: true,
      config: {},
      secrets,
    });
  }
}

async function upsertAgent(orgId: string, plan: RunPlan): Promise<void> {
  const existing = await db()
    .select({ id: llm_provider_config.id, config: llm_provider_config.config })
    .from(llm_provider_config)
    .where(
      and(
        eq(llm_provider_config.org_id, orgId),
        eq(llm_provider_config.scope, "agent"),
      ),
    )
    .limit(1);

  const prevConfig = (existing[0]?.config ?? {}) as Record<string, unknown>;
  const nextConfig = { ...prevConfig, backend: plan.backend };

  if (existing[0]) {
    await db()
      .update(llm_provider_config)
      .set({
        provider: plan.backend,
        config: nextConfig,
        updated_at: new Date(),
      })
      .where(eq(llm_provider_config.id, existing[0].id));
  } else {
    await db().insert(llm_provider_config).values({
      org_id: orgId,
      scope: "agent",
      provider: plan.backend,
      enabled: true,
      config: nextConfig,
      secrets: {},
    });
  }
}

export async function applyPlan(orgId: string, plan: RunPlan): Promise<void> {
  await upsertPrimary(orgId, plan);
  await upsertAgent(orgId, plan);
}
