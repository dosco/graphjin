import Link from "next/link";
import { connection } from "next/server";
import {
  action_policy,
  action_request,
  activeAdministratorIds,
  and,
  app_user,
  data_source,
  db,
  eq,
  sql,
  workflow_definition,
} from "@neko/db";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { getSetupCompleteAt } from "@/lib/org-state";
import { getAuthGateStatus } from "@/lib/auth";
import { AdminDenied, AdminShell } from "./AdminShell";
import { Card } from "@/components/ui/card";
import { StatCard, StatGrid } from "@/components/ui/stat-card";

export default async function AdminPage() {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;

  const orgId = await getOrgId();
  const [
    setupCompleteAt,
    authGate,
    users,
    administrators,
    sources,
    policyCounts,
    workflowCounts,
    pendingActionCounts,
  ] = await Promise.all([
    getSetupCompleteAt(orgId),
    getAuthGateStatus(),
    db()
      .select({ id: app_user.id, disabledAt: app_user.disabled_at })
      .from(app_user)
      .where(eq(app_user.org_id, orgId)),
    activeAdministratorIds(orgId),
    db()
      .select({
        id: data_source.id,
        authMode: data_source.auth_mode,
        enabled: data_source.enabled,
      })
      .from(data_source)
      .where(eq(data_source.org_id, orgId)),
    db()
      .select({
        total: sql<number>`count(*)::int`,
        enabled: sql<number>`count(*) filter (where ${action_policy.enabled} = true)::int`,
      })
      .from(action_policy)
      .where(eq(action_policy.org_id, orgId)),
    db()
      .select({
        total: sql<number>`count(*)::int`,
        active: sql<number>`count(*) filter (where ${workflow_definition.enabled} = true)::int`,
      })
      .from(workflow_definition)
      .where(eq(workflow_definition.org_id, orgId)),
    db()
      .select({ total: sql<number>`count(*)::int` })
      .from(action_request)
      .where(
        and(
          eq(action_request.org_id, orgId),
          eq(action_request.status, "pending_approval"),
        ),
      ),
  ]);

  const adminCount = administrators.length;
  const activeUserCount = users.filter((user) => !user.disabledAt).length;
  const enabledSources = sources.filter((source) => source.enabled);
  const jwtSources = enabledSources.filter(
    (source) => source.authMode === "jwt",
  ).length;
  const workflowTotal = workflowCounts[0]?.total ?? 0;
  const workflowActive = workflowCounts[0]?.active ?? 0;
  const policyTotal = policyCounts[0]?.total ?? 0;
  const policyEnabled = policyCounts[0]?.enabled ?? 0;
  const pendingActions = pendingActionCounts[0]?.total ?? 0;
  const authProviderInstalled = Boolean(authGate.provider);

  const coreCards = [
    {
      href: "/admin/users",
      title: "Users",
      copy: authProviderInstalled
        ? "Admin and member accounts from the sign-in provider."
        : "No sign-in provider is set up; this deployment runs with solo admin access.",
      status: authProviderInstalled
        ? `${activeUserCount} active - ${adminCount} admin`
        : "Solo admin",
      ok: authProviderInstalled ? adminCount > 0 : true,
    },
    {
      href: "/admin/settings",
      title: "Settings",
      copy: "Setup wizard, provider, data source, research, and security configuration.",
      status: setupCompleteAt ? "Setup complete" : "Setup incomplete",
      ok: Boolean(setupCompleteAt),
    },
    {
      href: "/admin/graphjin",
      title: "GraphJin",
      copy: "Data engine status, workbench, catalog, security, API reference, and eval reports.",
      status:
        enabledSources.length === 0
          ? "No sources"
          : `${enabledSources.length} source${enabledSources.length === 1 ? "" : "s"} enabled`,
      ok: enabledSources.length > 0,
    },
    {
      href: "/admin/rules",
      title: "Rules",
      copy: "Skill learning, plus what OpenNeko can act on its own, queue for review, or never run.",
      status:
        policyTotal === 0
          ? "Defaults on demand"
          : `${policyEnabled}/${policyTotal} enabled`,
      ok: true,
    },
  ];

  const operateCards = [
    {
      href: "/workflows",
      title: "Workflows",
      copy: "Pause, resume, run now, inspect schedules, and manage workflow runs.",
      status:
        workflowTotal === 0
          ? "None yet"
          : `${workflowActive}/${workflowTotal} active`,
      ok: workflowTotal === 0 || workflowActive > 0,
    },
    {
      href: "/actions?filter=awaiting",
      title: "Actions",
      copy: "Review pending approvals and inspect action receipts across workflows and packs.",
      status:
        pendingActions === 0 ? "None pending" : `${pendingActions} pending`,
      ok: true,
    },
  ];

  return (
    <AdminShell
      title="Administration"
      subtitle="OpenNeko configuration, users, packs, and data access."
    >
      <StatGrid label="Workspace status">
        <StatCard
          label="Setup"
          value={setupCompleteAt ? "Complete" : "Incomplete"}
          tone={setupCompleteAt ? "ok" : "warn"}
        />
        <StatCard
          label="Auth"
          value={authGate.provider ? "Sign-in" : "Solo"}
          detail={authGate.provider?.providerLabel ?? "userless admin"}
        />
        <StatCard
          label="GraphJin"
          value={
            enabledSources.length > 0
              ? `${jwtSources}/${enabledSources.length}`
              : "0"
          }
          detail="JWT sources"
          tone={
            enabledSources.length > 0 && jwtSources === enabledSources.length
              ? "ok"
              : "warn"
          }
        />
      </StatGrid>

      <AdminCardGroup title="Core" cards={coreCards} />
      <AdminCardGroup title="Operate" cards={operateCards} />
    </AdminShell>
  );
}

type AdminCard = {
  href: string;
  title: string;
  copy: string;
  status: string;
  ok: boolean;
};

function AdminCardGroup({
  title,
  cards,
}: {
  title: string;
  cards: AdminCard[];
}) {
  return (
    <section className="mt-7">
      <h2 className="mb-4 font-display text-ui-caption font-semibold text-text3">
        {title}
      </h2>
      <div className="grid gap-4 md:grid-cols-2">
        {cards.map((card) => (
          <Card
            as={Link}
            key={card.href}
            href={card.href}
            className="flex flex-col gap-4 no-underline transition-[border-color] hover:border-accent"
          >
              <div className="min-w-0 flex-1">
                <h3 className="settings-card-title">{card.title}</h3>
                <p className="settings-card-copy">{card.copy}</p>
              </div>
              <StatusText value={card.status} ok={card.ok} />
          </Card>
        ))}
      </div>
    </section>
  );
}

function StatusText({ value, ok }: { value: string; ok: boolean }) {
  return (
    <div
      className={`text-ui-caption font-semibold leading-snug ${
        ok ? "text-success-mid" : "text-danger"
      }`}
    >
      {value}
    </div>
  );
}
