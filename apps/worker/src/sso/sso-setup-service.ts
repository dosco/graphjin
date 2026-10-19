// SSO admin-setup orchestration. /admin/settings/sso drives the Scalekit
// workspace MCP tools through the sign-in service: generate an admin portal
// link (guides the operator through IdP setup), then poll connection state
// until it reports COMPLETED. The active environment defaults to Dev (free
// trial); Prod is an explicit opt-in switch. Sign-in credentials (env URL +
// client id + one-time secret) are written into the encrypted vault by the
// admin-gated settings page, which makes Scalekit the sign-in provider.

import { db, eq, organization, sso_setup } from "@neko/db";
import type { SignInService } from "../signin/signin-service.js";

export type SsoSetupStatus =
  | "not_started"
  | "awaiting_portal"
  | "polling"
  | "connected"
  | "failed";

export interface SsoSetupConnection {
  status: string;
  provider: string | null;
  enabled: boolean;
}

export interface SsoSetupStatusResult {
  status: SsoSetupStatus;
  portalLink: string | null;
  portalLinkExpiresAt: string | null;
  provider: string | null;
  connection: SsoSetupConnection | null;
  lastError: string | null;
  setupCompletedAt: string | null;
  environmentId: string | null;
  organizationId: string | null;
  environmentTier: string | null;
  signInConfigured: boolean;
}

const ACTIVE_POLL_STATUSES: ReadonlySet<SsoSetupStatus> = new Set([
  "awaiting_portal",
  "polling",
]);

interface SetupRow {
  status: string;
  portal_link: string | null;
  portal_link_expires_at: Date | null;
  provider: string | null;
  last_error: string | null;
  setup_completed_at: Date | null;
  scalekit_environment_id: string | null;
  scalekit_organization_id: string | null;
  scalekit_environment_tier: string | null;
}

export class SsoSetupService {
  constructor(private readonly signIn: SignInService) {}

  private async invoke(
    kind: string,
    payload: Record<string, unknown>,
    _orgId: string,
  ): Promise<{ text: string }> {
    return { text: await this.signIn.callWorkspaceTool(kind, payload) };
  }

  private async readRow(orgId: string): Promise<SetupRow | null> {
    const [row] = await db()
      .select({
        status: sso_setup.status,
        portal_link: sso_setup.portal_link,
        portal_link_expires_at: sso_setup.portal_link_expires_at,
        provider: sso_setup.provider,
        last_error: sso_setup.last_error,
        setup_completed_at: sso_setup.setup_completed_at,
        scalekit_environment_id: sso_setup.scalekit_environment_id,
        scalekit_organization_id: sso_setup.scalekit_organization_id,
        scalekit_environment_tier: sso_setup.scalekit_environment_tier,
      })
      .from(sso_setup)
      .where(eq(sso_setup.org_id, orgId))
      .limit(1);
    return row ?? null;
  }

  async getStatus(orgId: string): Promise<SsoSetupStatusResult> {
    const row = await this.readRow(orgId);
    const signInConfigured = this.signInConfigured();
    if (!row) {
      return {
        status: "not_started",
        portalLink: null,
        portalLinkExpiresAt: null,
        provider: null,
        connection: null,
        lastError: null,
        setupCompletedAt: null,
        environmentId: null,
        organizationId: null,
        environmentTier: null,
        signInConfigured,
      };
    }
    return {
      status: row.status as SsoSetupStatus,
      portalLink: row.portal_link,
      portalLinkExpiresAt: row.portal_link_expires_at?.toISOString() ?? null,
      provider: row.provider,
      connection: null,
      lastError: row.last_error,
      setupCompletedAt: row.setup_completed_at?.toISOString() ?? null,
      environmentId: row.scalekit_environment_id,
      organizationId: row.scalekit_organization_id,
      environmentTier: row.scalekit_environment_tier,
      signInConfigured,
    };
  }

  private signInConfigured(): boolean {
    const env = this.signIn.providerValues("scalekit");
    return Boolean(
      env.SCALEKIT_ENVIRONMENT_URL &&
        env.SCALEKIT_CLIENT_ID &&
        env.SCALEKIT_CLIENT_SECRET,
    );
  }

  /**
   * Fetch the environment's sign-in credentials via the agent's
   * `get_environment_credentials` tool (returns URL + client id; the
   * secret is dashboard-only by Scalekit's design).
   */
  async getEnvironmentCredentials(
    orgId: string,
    environmentId: string,
  ): Promise<{ environmentUrl: string; clientId: string }> {
    const { text } = await this.invoke(
      "get_environment_credentials",
      { environmentId },
      orgId,
    );
    const environmentUrl = /SCALEKIT_ENVIRONMENT_URL=(https:\/\/\S+)/.exec(
      text,
    )?.[1];
    const clientId = /SCALEKIT_CLIENT_ID=(\S+)/.exec(text)?.[1];
    if (!environmentUrl || !clientId) {
      throw new Error(
        `could not parse credentials from Scalekit's response: ${text.slice(0, 300)}`,
      );
    }
    return { environmentUrl, clientId };
  }

  /**
   * List the workspace's Scalekit environments via the agent's
   * `list_environments` MCP tool, parsed back into structured rows for
   * the settings page picker (no raw-id pasting).
   */
  async listEnvironments(orgId: string): Promise<Array<{
    id: string;
    name: string | null;
    tier: string | null;
    domain: string | null;
  }>> {
    const { text } = await this.invoke("list_environments", {}, orgId);
    return text
      .split("\n")
      .map((row) => {
        const id = /ID:\s*(\S+)/.exec(row)?.[1] ?? null;
        const name = /Name:\s*([^|]+?)\s*\|/.exec(row)?.[1]?.trim() || null;
        const tier = /Type:\s*(\w+)/.exec(row)?.[1] ?? null;
        const domain = /Domain:\s*([^|]+?)\s*\|/.exec(row)?.[1]?.trim() || null;
        return { id: id ?? "", name, tier, domain };
      })
      .filter((r) => r.id.length > 0);
  }

  /** List organizations under one environment via `list_organizations`. */
  async listOrganizations(
    orgId: string,
    environmentId: string,
  ): Promise<Array<{ id: string; name: string | null }>> {
    const { text } = await this.invoke(
      "list_organizations",
      { environmentId },
      orgId,
    );
    const out: Array<{ id: string; name: string | null }> = [];
    for (const block of text.split(/Organization \d+:/)) {
      const id = /ID:\s*(\S+)/.exec(block)?.[1];
      const name = /Name:\s*([^\n]+)/.exec(block)?.[1]?.trim();
      if (id) out.push({ id, name: name || null });
    }
    return out;
  }

  /**
   * Pin the active Scalekit environment/organization (Dev default, Prod opt-in).
   * The organization also becomes SCALEKIT_ORGANIZATION_ID for directory sync.
   */
  async setScalekitIds(
    orgId: string,
    input: { environmentId: string; organizationId: string; tier: string },
  ): Promise<void> {
    if (!input.environmentId || !input.organizationId) {
      throw new Error("environmentId and organizationId are required");
    }
    await db()
      .insert(sso_setup)
      .values({
        org_id: orgId,
        scalekit_environment_id: input.environmentId,
        scalekit_organization_id: input.organizationId,
        scalekit_environment_tier: input.tier,
        updated_at: new Date(),
      })
      .onConflictDoUpdate({
        target: sso_setup.org_id,
        set: {
          scalekit_environment_id: input.environmentId,
          scalekit_organization_id: input.organizationId,
          scalekit_environment_tier: input.tier,
          updated_at: new Date(),
        },
      });
    await db()
      .update(organization)
      .set({ scalekit_org_id: input.organizationId, updated_at: new Date() })
      .where(eq(organization.id, orgId));
    await this.signIn.setProviderValues("scalekit", {
      SCALEKIT_ORGANIZATION_ID: input.organizationId,
      SCALEKIT_ENVIRONMENT_ID: input.environmentId,
    });
  }

  /**
   * Write the sign-in credentials into the encrypted vault (admin-gated)
   * and make Scalekit the sign-in provider.
   */
  async setSignInCredentials(
    orgId: string,
    input: { environmentUrl: string; clientId: string; clientSecret: string },
  ): Promise<void> {
    void orgId;
    if (!input.environmentUrl || !input.clientId || !input.clientSecret) {
      throw new Error("environmentUrl, clientId and clientSecret are required");
    }
    await this.signIn.setProviderValues("scalekit", {
      SCALEKIT_ENVIRONMENT_URL: input.environmentUrl,
      SCALEKIT_CLIENT_ID: input.clientId,
      SCALEKIT_CLIENT_SECRET: input.clientSecret,
    });
    await this.signIn.selectProvider("scalekit");
  }

  private async resolveIds(
    orgId: string,
  ): Promise<{ environmentId: string; organizationId: string }> {
    const row = await this.readRow(orgId);
    if (row?.scalekit_environment_id && row?.scalekit_organization_id) {
      return {
        environmentId: row.scalekit_environment_id,
        organizationId: row.scalekit_organization_id,
      };
    }
    // Fallback: values an operator set directly in the secrets store.
    const env = this.signIn.providerValues("scalekit");
    if (env.SCALEKIT_ENVIRONMENT_ID && env.SCALEKIT_ORGANIZATION_ID) {
      return {
        environmentId: env.SCALEKIT_ENVIRONMENT_ID,
        organizationId: env.SCALEKIT_ORGANIZATION_ID,
      };
    }
    throw new Error(
      "Scalekit environment/organization not selected yet — run `get_environment_credentials` + `list_organizations` via the agent, or set them on the SSO settings page",
    );
  }

  async generatePortalLink(
    orgId: string,
  ): Promise<{ portalLink: string; expiresAt: string | null }> {
    const { environmentId, organizationId } = await this.resolveIds(orgId);
    const { text } = await this.invoke(
      "generate_admin_portal_link",
      { environmentId, organizationId },
      orgId,
    );
    const match = /Link:\s*(https?:\/\/[^\s]+)/.exec(text);
    if (!match) {
      throw new Error(
        `Scalekit returned no portal link (${text.slice(0, 200)}). Verify the organization id.`,
      );
    }
    const portalLink = match[1]!;
    await db()
      .insert(sso_setup)
      .values({
        org_id: orgId,
        status: "awaiting_portal",
        portal_link: portalLink,
        portal_link_expires_at: null,
        last_error: null,
        updated_at: new Date(),
      })
      .onConflictDoUpdate({
        target: sso_setup.org_id,
        set: {
          status: "awaiting_portal",
          portal_link: portalLink,
          portal_link_expires_at: null,
          last_error: null,
          updated_at: new Date(),
        },
      });
    return { portalLink, expiresAt: null };
  }

  async checkConnection(orgId: string): Promise<SsoSetupStatusResult> {
    let connection: SsoSetupConnection | null = null;
    let nextStatus: SsoSetupStatus;
    let lastError: string | null = null;
    try {
      const { environmentId, organizationId } = await this.resolveIds(orgId);
      const { text } = await this.invoke(
        "list_organization_connections",
        { environmentId, organizationId },
        orgId,
      );
      const statuses = [...text.matchAll(/status:\s*(\w+)/g)].map(
        (m) => m[1]!,
      );
      const providerMatch = /provider:\s*(\w+)/.exec(text);
      if (statuses.some((s) => s === "COMPLETED")) {
        connection = {
          status: "connected",
          provider: providerMatch?.[1] ?? null,
          enabled: true,
        };
        nextStatus = "connected";
      } else if (statuses.length > 0) {
        connection = {
          status: "in_progress",
          provider: providerMatch?.[1] ?? null,
          enabled: false,
        };
        nextStatus = "polling";
      } else {
        nextStatus = "awaiting_portal";
      }
    } catch (err) {
      nextStatus = "failed";
      lastError = err instanceof Error ? err.message : String(err);
    }

    const provider = connection?.provider ?? null;
    const completedAt = nextStatus === "connected" ? new Date() : null;
    await db()
      .insert(sso_setup)
      .values({
        org_id: orgId,
        status: nextStatus,
        provider,
        last_polled_at: new Date(),
        last_error: lastError,
        setup_completed_at: completedAt,
        updated_at: new Date(),
      })
      .onConflictDoUpdate({
        target: sso_setup.org_id,
        set: {
          status: nextStatus,
          provider,
          last_polled_at: new Date(),
          last_error: lastError,
          setup_completed_at: completedAt,
          updated_at: new Date(),
        },
      });

    return {
      status: nextStatus,
      portalLink: null,
      portalLinkExpiresAt: null,
      provider,
      connection,
      lastError,
      setupCompletedAt: completedAt?.toISOString() ?? null,
      environmentId: null,
      organizationId: null,
      environmentTier: null,
      signInConfigured: this.signInConfigured(),
    };
  }

  /**
   * Poll only while there is something to watch. Returns immediately
   * (no workspace call) when the row is not in a polling state.
   */
  async poll(orgId: string): Promise<SsoSetupStatusResult> {
    const current = await this.getStatus(orgId);
    if (!ACTIVE_POLL_STATUSES.has(current.status)) {
      return current;
    }
    return this.checkConnection(orgId);
  }
}

export function startSsoSetupPoller(options: {
  service: SsoSetupService;
  orgId: string;
  intervalMs?: number;
}): { stop: () => void } {
  const intervalMs = options.intervalMs ?? 30_000;
  const timer = setInterval(() => {
    options.service.poll(options.orgId).catch((err) => {
      console.warn(
        `[sso-setup] poll failed: ${err instanceof Error ? err.message : err}`,
      );
    });
  }, intervalMs);
  timer.unref?.();
  return {
    stop: () => clearInterval(timer),
  };
}
