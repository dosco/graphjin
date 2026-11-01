/**
 * Shared helpers for DB integration tests.
 *
 * - dbReachable() — auto-skip predicate; same shape used by drizzle-crud
 *   and pg-boss-flow tests since the start of the project.
 * - uniqueOrgId() — generates a per-test org id so parallel/repeated runs
 *   don't collide. Use cascading delete via the organization FK to clean up.
 * - withTestOrg() — convenience wrapper: creates the org row, runs the body,
 *   deletes everything cascade-attached to it.
 * - seed{DataSource,PrimaryProvider,AgentBackend}() — minimal fixture writers
 *   that other tests can compose without re-deriving column shapes.
 */

import {
  and,
  data_source,
  db,
  eq,
  llm_provider_config,
  organization,
  pool,
} from "../../src";

export async function dbReachable(): Promise<boolean> {
  try {
    await pool().query("select 1");
    return true;
  } catch {
    return false;
  }
}

export function uniqueOrgId(label = "vitest"): string {
  return `${label}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

export async function createTestOrg(orgId: string, name = "Vitest Org"): Promise<void> {
  await db().insert(organization).values({ id: orgId, name });
}

export async function deleteTestOrg(orgId: string): Promise<void> {
  // Cascade FKs clean up child rows.
  await db().delete(organization).where(eq(organization.id, orgId));
}

export async function withTestOrg<T>(
  fn: (orgId: string) => Promise<T>,
  label = "vitest",
): Promise<T> {
  const orgId = uniqueOrgId(label);
  await createTestOrg(orgId);
  try {
    return await fn(orgId);
  } finally {
    await deleteTestOrg(orgId);
  }
}

export async function seedDataSource(
  orgId: string,
  overrides: {
    graphqlUrl?: string;
    mcpUrl?: string | null;
    label?: string | null;
    kind?: string;
  } = {},
): Promise<void> {
  await db().insert(data_source).values({
    org_id: orgId,
    kind: overrides.kind ?? "graphjin",
    graphql_url: overrides.graphqlUrl ?? "http://localhost:8080/api/v1/graphql",
    mcp_url: overrides.mcpUrl ?? "http://localhost:8080/api/v1/mcp",
    label: overrides.label ?? "primary",
  });
}

export type ProviderSeed = {
  scope:
    | "primary"
    | "research"
    | "agent"
    | "install-policy"
    | "graphjin-config"
    | "graphjin-agent";
  provider: string;
  model?: string | null;
  enabled?: boolean;
  config?: Record<string, unknown>;
  secrets?: Record<string, unknown>;
};

export async function seedProvider(orgId: string, seed: ProviderSeed): Promise<void> {
  await db().insert(llm_provider_config).values({
    org_id: orgId,
    scope: seed.scope,
    provider: seed.provider,
    model: seed.model ?? null,
    enabled: seed.enabled ?? true,
    config: seed.config ?? {},
    secrets: seed.secrets ?? {},
  });
}

export async function clearProvider(
  orgId: string,
  scope:
    | "primary"
    | "research"
    | "agent"
    | "install-policy"
    | "graphjin-config"
    | "graphjin-agent",
): Promise<void> {
  await db()
    .delete(llm_provider_config)
    .where(
      and(
        eq(llm_provider_config.org_id, orgId),
        eq(llm_provider_config.scope, scope),
      ),
    );
}
