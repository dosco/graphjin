import "server-only";

import { and, db, eq, llm_provider_config } from "@neko/db";

/**
 * Deployment-wide policy gate for community-skill installs.
 *
 * The switch is a hard floor: when off, `openneko install <git-url>`
 * is refused. Defaults are secure-by-default — an operator opts in
 * explicitly to widen the install surface.
 *
 * Storage lives in `llm_provider_config` with scope="install-policy",
 * single row per org (which IS the deployment in OpenNeko's model —
 * setup writes exactly one org row). Same pattern as
 * agent-backend-settings / provider-settings reuses the table.
 */
export type InstallPolicy = {
  /** Allow `openneko install <git-url>` for community skills. */
  allowGitUrlInstalls: boolean;
};

/** Returned to the settings UI alongside the policy itself. */
export type InstallPolicyPayload = {
  policy: InstallPolicy;
  /** "org" when an operator saved a row; "default" when the row is absent. */
  source: "org" | "default";
};

export const INSTALL_POLICY_SCOPE = "install-policy";
const PROVIDER_TAG = "install-policy";

/** Secure-by-default: new deployments don't allow git-URL skill installs. */
export const DEFAULT_POLICY: InstallPolicy = {
  allowGitUrlInstalls: false,
};

async function loadRow(orgId: string): Promise<{
  id: string;
  config: Record<string, unknown> | null;
} | null> {
  const rows = await db()
    .select({
      id: llm_provider_config.id,
      config: llm_provider_config.config,
    })
    .from(llm_provider_config)
    .where(
      and(
        eq(llm_provider_config.org_id, orgId),
        eq(llm_provider_config.scope, INSTALL_POLICY_SCOPE),
      ),
    )
    .limit(1);
  return (
    (rows[0] as { id: string; config: Record<string, unknown> | null } | undefined) ?? null
  );
}

function policyFromConfig(config: Record<string, unknown> | null): InstallPolicy {
  const value = config?.allowGitUrlInstalls;
  return {
    allowGitUrlInstalls: typeof value === "boolean" ? value : DEFAULT_POLICY.allowGitUrlInstalls,
  };
}

export async function getInstallPolicy(orgId: string): Promise<InstallPolicy> {
  const row = await loadRow(orgId);
  return policyFromConfig(row?.config ?? null);
}

export async function getInstallPolicyPayload(
  orgId: string,
): Promise<InstallPolicyPayload> {
  const row = await loadRow(orgId);
  return {
    policy: policyFromConfig(row?.config ?? null),
    source: row ? "org" : "default",
  };
}

/** Partial draft from the UI. Anything absent leaves the existing value alone. */
export type InstallPolicyDraft = {
  allowGitUrlInstalls?: boolean;
};

export async function saveInstallPolicyDraft(
  orgId: string,
  draft: InstallPolicyDraft,
): Promise<InstallPolicy> {
  const existing = await loadRow(orgId);
  const current = policyFromConfig(existing?.config ?? null);
  const merged: InstallPolicy = {
    allowGitUrlInstalls:
      draft.allowGitUrlInstalls !== undefined
        ? draft.allowGitUrlInstalls
        : current.allowGitUrlInstalls,
  };

  if (existing) {
    await db()
      .update(llm_provider_config)
      .set({ config: merged, updated_at: new Date() })
      .where(eq(llm_provider_config.id, existing.id));
  } else {
    await db().insert(llm_provider_config).values({
      org_id: orgId,
      scope: INSTALL_POLICY_SCOPE,
      provider: PROVIDER_TAG,
      enabled: true,
      config: merged,
      secrets: {},
    });
  }

  return merged;
}
