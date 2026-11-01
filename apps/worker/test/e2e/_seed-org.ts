/**
 * Auto-seed a throwaway org with everything the metric agent needs:
 *   - data_source pointing at the running GraphJin
 *   - llm_provider_config (scope='primary') = the plan's provider + key
 *   - llm_provider_config (scope='agent')   = the plan's backend
 *
 * The same org is reused across all plans in a suite; `applyPlan` flips
 * both the primary and agent rows between sub-suites. Cascading FKs clean
 * up data_source + provider rows when the org is deleted.
 *
 * Stays inside the existing dev `neko` database — no temp container
 * needed since cascade-on-delete keeps the dev's main org untouched.
 */

import { db, eq, organization } from "@neko/db";
import { maybeEncryptSecret } from "@neko/llm/secrets";
import {
  createTestOrg,
  deleteTestOrg,
  seedDataSource,
  seedProvider,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import type { RunPlan } from "./_can-run";

export type E2ESeedFixture = {
  orgId: string;
  cleanup: () => Promise<void>;
};

export async function seedE2ETestOrg(opts: {
  graphqlUrl: string;
  initialPlan: RunPlan;
}): Promise<E2ESeedFixture> {
  const orgId = uniqueOrgId("e2e");
  await createTestOrg(orgId, "E2E Throwaway");

  // Stamp setup_complete_at so the rest of the app treats this org as
  // fully bootstrapped. The agent doesn't read this column itself, but
  // any downstream helpers that do won't bounce us back to /setup.
  await db()
    .update(organization)
    .set({ setup_complete_at: new Date() })
    .where(eq(organization.id, orgId));

  // The graphqlUrl points at the host's running GraphJin. mcp_url is the
  // sister endpoint by convention; the metric agent shells out to graphjin
  // CLI for queries so mcp_url isn't load-bearing here, but we set it for
  // correctness against the snapshot writer / profiler if they're ever
  // exercised in the same test process.
  const baseUrl = opts.graphqlUrl.replace(/\/api\/v1\/graphql\/?$/, "");
  await seedDataSource(orgId, {
    graphqlUrl: opts.graphqlUrl,
    mcpUrl: `${baseUrl}/api/v1/mcp`,
    label: "primary",
  });

  await seedProvider(orgId, {
    scope: "primary",
    provider: opts.initialPlan.primaryProvider,
    model: opts.initialPlan.primaryModel,
    enabled: true,
    secrets: { apiKey: maybeEncryptSecret(opts.initialPlan.apiKey) },
  });

  await seedProvider(orgId, {
    scope: "agent",
    provider: opts.initialPlan.backend,
    enabled: true,
    config: { backend: opts.initialPlan.backend },
  });

  return {
    orgId,
    cleanup: async () => {
      await deleteTestOrg(orgId);
    },
  };
}
