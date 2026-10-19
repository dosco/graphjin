/**
 * Install-policy read path, shared between apps/web (admin UI) and
 * apps/worker (the CLI's install-policy endpoint). Persisted in
 * llm_provider_config with scope="install-policy" — same overload
 * pattern as the "agent" scope.
 *
 * The write path lives in apps/web/src/lib/install-policy-settings.ts
 * (admin-only, has UI affordances). The worker only reads.
 */

import { db, and, eq } from "./index";
import { llm_provider_config } from "./schema";

export const INSTALL_POLICY_SCOPE = "install-policy";

export type InstallPolicy = {
  /** Allow `openneko install <git-url>` for community skills. */
  allowGitUrlInstalls: boolean;
};

export const DEFAULT_INSTALL_POLICY: InstallPolicy = {
  allowGitUrlInstalls: false,
};

export function policyFromConfig(
  config: Record<string, unknown> | null,
): InstallPolicy {
  const value = config?.allowGitUrlInstalls;
  return {
    allowGitUrlInstalls:
      typeof value === "boolean" ? value : DEFAULT_INSTALL_POLICY.allowGitUrlInstalls,
  };
}

/**
 * Read the install policy for an org. Returns DEFAULT_INSTALL_POLICY
 * when no row exists (fresh deployment hasn't customized).
 */
export async function getInstallPolicyForOrg(orgId: string): Promise<InstallPolicy> {
  const rows = await db()
    .select({ config: llm_provider_config.config })
    .from(llm_provider_config)
    .where(
      and(
        eq(llm_provider_config.org_id, orgId),
        eq(llm_provider_config.scope, INSTALL_POLICY_SCOPE),
      ),
    )
    .limit(1);
  return policyFromConfig((rows[0]?.config ?? null) as Record<string, unknown> | null);
}
