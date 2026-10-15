import Link from "next/link";
import { connection } from "next/server";
import { data_source, db, eq, hasCustomPassword, pool } from "@neko/db";
import { ArrowUpRight } from "lucide-react";
import AppHeader from "@/components/AppHeader";
import PageHeading from "@/components/PageHeading";
import SectionNav from "@/components/SectionNav";
import { AdminDenied } from "@/app/admin/AdminShell";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { getSetupCompleteAt } from "@/lib/org-state";
import {
  getDataSourceSettings,
  hasDataSourceSetup,
} from "@/lib/data-source-settings";
import {
  getProviderSettingsPayload,
  hasPrimaryProviderSetup,
  resolveResearchStatus,
} from "@/lib/provider-settings";
import {
  getAgentSettingsPayload,
} from "@/lib/agent-backend-settings";
import { getGraphjinConfigSettingsPayload } from "@/lib/graphjin-config-settings";
import { getAuthGateStatus } from "@/lib/auth";
import { getInstallPolicy } from "@/lib/install-policy-settings";
import { getContextRemote } from "@neko/llm/config-vcs";
import { getSpendSettings } from "@neko/llm/spend";
import SetupWizard from "./SetupWizard";

/**
 * Single admin surface — wizard until first-run is finished, then a
 * card index for ongoing edits. The wizard's gating (linear steps,
 * required-prereqs check on Finish) is preserved; the only thing
 * collapsed is the URL surface — admins no longer juggle /setup +
 * /settings as separate pages.
 *
 * The branch is decided server-side by setup_complete_at, so admins
 * can't bypass first-run gating by hitting a different URL.
 */
export default async function SettingsPage() {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;

  const orgId = await getOrgId();
  const setupCompleteAt = await getSetupCompleteAt(orgId);

  // ── First-run mode: render the linear wizard. ──
  if (!setupCompleteAt) {
    const [dataSource, providers, agent] = await Promise.all([
      getDataSourceSettings(orgId),
      getProviderSettingsPayload(orgId),
      getAgentSettingsPayload(orgId),
    ]);
    return (
      <SetupWizard
        initial={{
          dataSource,
          providers,
          agent,
          passwordChanged: hasCustomPassword(),
        }}
      />
    );
  }

  // ── Ongoing-edits mode: card index linking to focused sub-pages. ──
  const [
    dataReady,
    primaryReady,
    researchStatus,
    sources,
    graphjinConfig,
    spend,
    authGate,
    installPolicy,
    workflowOrgLimits,
    contextRemote,
  ] = await Promise.all([
    hasDataSourceSetup(orgId),
    hasPrimaryProviderSetup(orgId),
    resolveResearchStatus(orgId),
    db()
      .select({
        id: data_source.id,
        authMode: data_source.auth_mode,
        enabled: data_source.enabled,
      })
      .from(data_source)
      .where(eq(data_source.org_id, orgId)),
    getGraphjinConfigSettingsPayload(orgId),
    getSpendSettings(orgId),
    getAuthGateStatus(),
    getInstallPolicy(orgId),
    pool().query<{ org_id: string }>(
      "select org_id from workflow_api_org_limits where org_id = $1",
      [orgId],
    ),
    getContextRemote(orgId),
  ]);
  const signInPlugin = authGate.provider?.pluginName ?? null;
  const pendingPlugin = authGate.pending?.pluginName ?? null;
  const authStatus = (plugin: string) =>
    signInPlugin === plugin
      ? { status: "Live", statusTone: "success" as const }
      : pendingPlugin === plugin
        ? { status: "Setup pending", statusTone: "watch" as const }
        : { status: "Not installed", statusTone: "neutral" as const };
  const extraMarketplaces = installPolicy.allowedMarketplaces.length;
  const securityStatus = installPolicy.allowUnverified || installPolicy.allowGitUrlInstalls
    ? { status: "Exceptions allowed", statusTone: "watch" as const }
    : extraMarketplaces > 0
      ? { status: `${extraMarketplaces + 1} marketplaces`, statusTone: "success" as const }
      : { status: "Official marketplace only", statusTone: "success" as const };

  const enabledSources = sources.filter((source) => source.enabled);
  const jwtSources = enabledSources.filter(
    (source) => source.authMode === "jwt",
  ).length;

  type SettingsCard = {
    href: string;
    title: string;
    copy: string;
    status?: string;
    statusTone?: "success" | "watch" | "neutral";
  };
  const spentToday = spend.org.day.spentUsd + spend.org.day.heldUsd;
  const groups: Array<{ title: string; cards: SettingsCard[] }> = [
    {
      title: "Connections",
      cards: [
        {
          href: "/admin/settings/data",
          title: "Data source",
          copy: "The connection OpenNeko uses to read your business data.",
          status: dataReady ? "Connected" : "Not set",
          statusTone: dataReady ? "success" : "watch",
        },
        {
          href: "/admin/settings/packs",
          title: "Solution packs",
          copy: "Install and manage packs for applications such as Magento.",
          status: "Magento ready",
          statusTone: "success",
        },
        {
          href: "/admin/settings/graphjin",
          title: "Data access rules",
          copy: "How OpenNeko signs in to each source, and what each role can read.",
          status: graphjinConfig.settings.sourceConfigEnabled
            ? enabledSources.length === 0
              ? "Ask-based config on · No source"
              : `Ask-based config on · ${jwtSources} of ${enabledSources.length} signed`
            : "Ask-based config off",
          statusTone: !graphjinConfig.settings.sourceConfigEnabled
            ? "neutral"
            : enabledSources.length > 0 && jwtSources === enabledSources.length
              ? "success"
              : "watch",
        },
      ],
    },
    {
      title: "Agent",
      cards: [
        {
          href: "/admin/settings/agent",
          title: "Model and provider",
          copy: "The AI model OpenNeko uses, and the provider that runs it.",
          status: primaryReady ? "Ready" : "Model not set",
          statusTone: primaryReady ? "success" : "watch",
        },
        {
          href: "/admin/settings/repository",
          title: "Context repository",
          copy: "Version history of skills and workflows, published to GitHub, GitLab or another git host.",
          status: contextRemote
            ? contextRemote.lastPublish?.status === "failed"
              ? "Last publish failed"
              : new URL(contextRemote.url).hostname
            : "Not connected",
          statusTone: contextRemote ? (contextRemote.lastPublish?.status === "failed" ? "watch" : "success") : "neutral",
        },
        {
          href: "/admin/settings/research",
          title: "Industry research",
          copy: "Optional research on your industry that OpenNeko runs during setup.",
          status: researchStatus === "enabled" ? "On" : "Off",
          statusTone: researchStatus === "enabled" ? "success" : "neutral",
        },
      ],
    },
    {
      title: "Access",
      cards: [
        {
          href: "/admin/settings/sso",
          title: "Single sign-on",
          copy: "People sign in with Okta, Entra ID, or another identity provider. Their groups map to roles.",
          ...authStatus("@open-neko/plugin-scalekit"),
        },
        {
          href: "/admin/settings/signin",
          title: "Email-link sign-in",
          copy: "Invited people sign in with a one-time link sent to their email.",
          ...authStatus("@open-neko/plugin-magic-link"),
        },
        {
          href: "/admin/settings/security",
          title: "Install policy",
          copy: "The marketplaces that plugins and skills can come from.",
          ...securityStatus,
        },
      ],
    },
    {
      title: "Spending",
      cards: [
        {
          href: "/admin/settings/spend",
          title: "Spending limits",
          copy: "Caps on AI model spend per run, per hour, and per day.",
          status:
            spend.alerts.length > 0
              ? `${spend.alerts.length} open ${spend.alerts.length === 1 ? "alert" : "alerts"}`
              : `$${spentToday.toFixed(2)} of $${spend.org.day.limitUsd.toFixed(2)} today`,
          statusTone:
            spend.alerts.length > 0 || spentToday >= (spend.org.day.limitUsd * spend.limits.warnPercent) / 100
              ? "watch"
              : "success",
        },
        {
          href: "/admin/settings/workflows",
          title: "Workflow API",
          copy: "Public URL and budgets for workflows that other systems start through the API.",
          status: workflowOrgLimits.rows.length > 0 ? "Custom budget" : "Default budget",
          statusTone: "neutral",
        },
      ],
    },
  ];

  return (
    <div className="root">
      <AppHeader back={{ href: "/admin", label: "Admin" }}>
        <SectionNav current="admin" />
      </AppHeader>
      <PageHeading
        title="Workspace settings"
        description="Connections, the agent, sign-in, and spending for this workspace."
      />

      {groups.map((group) => (
        <section key={group.title} className="settings-index-group" aria-labelledby={`settings-${group.title}`}>
          <h2 id={`settings-${group.title}`} className="settings-index-group-title">
            {group.title}
          </h2>
          <div className="settings-index-grid">
            {group.cards.map((card) => (
              <Link
                key={card.href}
                href={card.href}
                className="settings-card settings-index-card no-underline"
              >
                <div className="settings-index-card-top">
                  <h3 className="settings-card-title">{card.title}</h3>
                  <ArrowUpRight className="settings-index-card-arrow" aria-hidden="true" />
                </div>
                <p className="settings-card-copy">{card.copy}</p>
                {card.status ? (
                  <div className="settings-index-card-foot">
                    <span
                      className="settings-index-status"
                      data-tone={card.statusTone ?? "neutral"}
                    >
                      {card.status}
                    </span>
                  </div>
                ) : null}
              </Link>
            ))}
          </div>
        </section>
      ))}
    </div>
  );
}
