import Link from "next/link";
import { connection } from "next/server";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import {
  graphjinAdminGet,
  graphjinAdminQuery,
  graphjinAdminSource,
  parseGraphjinJson,
} from "@/lib/graphjin-admin";
import { AdminDenied } from "../AdminShell";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { LocalDateTime } from "@/components/ui/local-date-time";
import { StatCard, StatGrid } from "@/components/ui/stat-card";
import { GraphjinShell, GraphjinUnavailable, SettingsCard, StatusBadge } from "./GraphjinAdmin";

const RUNTIME_QUERY = `query AdminGraphjinRuntime {
  status: gj_runtime(where: { kind: { eq: "status" } }, limit: 1) {
    id mode store status severity summary next_action active_database schema_ready table_count catalog_revision details_json
  }
  sources: gj_runtime(where: { kind: { eq: "source" } }, order_by: { source: asc }, limit: 80) {
    id source source_kind database_name status severity table_count duration_ms summary
  }
  events: gj_runtime(where: { kind: { eq: "event" } }, order_by: { created_at: desc }, limit: 8) {
    id created_at phase status severity summary
  }
}`;

const SECURITY_QUERY = `query AdminGraphjinSecuritySummary {
  summary: gj_security(id: "summary") { id mode status severity title summary summary_json }
  findings: gj_security(where: { kind: { eq: "finding" } }, order_by: { severity_rank: desc }, limit: 6) { id severity title }
}`;

const CATALOG_QUERY = `query AdminGraphjinCoverage {
  capabilities: gj_catalog(where: { kind: { eq: "system_capability" } }, order_by: { name: asc }, limit: 80) { id name }
  tables: gj_catalog(where: { kind: { eq: "table" } }, limit: 1) { id }
  relationships: gj_catalog(where: { kind: { eq: "relationship" } }, limit: 1) { id }
}`;

type RuntimeStatus = {
  id: string;
  mode: string | null;
  store: string | null;
  status: string | null;
  severity: string | null;
  summary: string | null;
  next_action: string | null;
  active_database: string | null;
  schema_ready: boolean | null;
  table_count: number | null;
  catalog_revision: string | null;
  details_json: unknown;
};
type RuntimeSource = {
  id: string;
  source: string;
  source_kind: string | null;
  database_name: string | null;
  status: string | null;
  severity: string | null;
  table_count: number | null;
  duration_ms: number | null;
  summary: string | null;
};
type RuntimeEvent = {
  id: string;
  created_at: string | null;
  phase: string | null;
  status: string | null;
  severity: string | null;
  summary: string | null;
};
type SecuritySummary = {
  summary: { title: string | null; summary: string | null; summary_json: unknown } | null;
  findings: Array<{ id: string; severity: string | null; title: string | null }>;
};
type Coverage = {
  capabilities: Array<{ id: string; name: string }>;
  tables: Array<{ id: string }>;
  relationships: Array<{ id: string }>;
};
type AgentStatus = { ready: boolean; enabled: boolean; model?: string; read_only: boolean; message?: string };

const SECTION_CARDS = [
  { href: "/admin/graphjin/workbench", title: "Workbench", copy: "Run GraphQL against GraphJin and ask its built-in agent." },
  { href: "/admin/graphjin/catalog", title: "Catalog", copy: "Databases, tables, columns, relationships, and capabilities GraphJin discovered." },
  { href: "/admin/graphjin/security", title: "Security", copy: "Posture, findings, and the effective policy for each role." },
  { href: "/admin/graphjin/code", title: "Code", copy: "Files, symbols, and database references from configured code sources." },
  { href: "/admin/graphjin/api", title: "API reference", copy: "REST operations from GraphJin's OpenAPI document." },
  { href: "/admin/graphjin/evals", title: "Eval reports", copy: "Benchmark outcomes, method checks, and safety gates from GraphJin's trainer." },
  { href: "/admin/graphjin/activity", title: "Activity", copy: "Tasks, the watch inbox, and annotations waiting for review." },
  { href: "/admin/graphjin/config", title: "Config", copy: "GraphJin's effective configuration with secrets redacted." },
  { href: "/admin/settings/graphjin", title: "Settings", copy: "Sources, secrets, RBAC claims, and Ask-based config tools." },
];

export default async function AdminGraphjinPage() {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;

  const orgId = await getOrgId();
  const caller = { orgId, userId: actor.userId };
  const [source, runtime, security, coverage, agent] = await Promise.all([
    graphjinAdminSource(orgId),
    graphjinAdminQuery<{ status: RuntimeStatus[]; sources: RuntimeSource[]; events: RuntimeEvent[] }>(caller, RUNTIME_QUERY),
    graphjinAdminQuery<SecuritySummary>(caller, SECURITY_QUERY),
    graphjinAdminQuery<Coverage>(caller, CATALOG_QUERY),
    graphjinAdminGet<AgentStatus>(caller, "agent/status"),
  ]);

  const status = runtime.ok ? runtime.data.status?.[0] : undefined;
  const sources = runtime.ok ? runtime.data.sources ?? [] : [];
  const events = runtime.ok ? runtime.data.events ?? [] : [];
  const readySources = sources.filter((row) => row.status === "ready").length;
  const runtimeRead = parseGraphjinJson<{ runtime_read?: boolean }>(status?.details_json, {}).runtime_read === true;

  return (
    <GraphjinShell
      current="/admin/graphjin"
      title="GraphJin"
      subtitle="Health, discovery, and security for the data engine behind Ask and workflows."
    >
      {!runtime.ok ? (
        <GraphjinUnavailable title="Runtime status" error={runtime.error} />
      ) : (
        <>
          <StatGrid label="GraphJin runtime">
            <StatCard
              label="Status"
              value={status?.status ?? "Unknown"}
              detail={status?.mode ?? undefined}
              tone={status?.status === "ready" ? "ok" : "warn"}
            />
            <StatCard
              label="Sources"
              value={sources.length}
              detail={`${readySources} ready`}
              tone={readySources === sources.length ? "ok" : "warn"}
            />
            <StatCard
              label="Tables"
              value={status?.table_count ?? 0}
              detail={status?.schema_ready ? "Schema ready" : "Schema pending"}
            />
            <StatCard
              label="Agent"
              value={agent.ok ? (agent.data.ready ? "Ready" : "Not ready") : "Unavailable"}
              detail={agent.ok ? agent.data.model ?? agent.data.message : undefined}
              tone={agent.ok && agent.data.ready ? "ok" : "warn"}
            />
          </StatGrid>

          <SettingsCard
            title={status?.summary ?? "Runtime status is not available."}
            copy={status?.next_action ?? undefined}
            aside={<StatusBadge status={status?.status} />}
          >
            <div className="flex flex-wrap gap-2">
              {source ? <Badge variant="outline">{source.name}</Badge> : null}
              {status?.store ? <Badge variant="secondary">{status.store}</Badge> : null}
              {status?.active_database ? <Badge variant="outline">{status.active_database}</Badge> : null}
              <Badge variant={status?.catalog_revision ? "success" : "watch"}>
                {status?.catalog_revision ? "Catalog ready" : "Catalog pending"}
              </Badge>
              <Badge variant={runtimeRead ? "success" : "watch"}>
                {runtimeRead ? "Runtime read on" : "Runtime read limited"}
              </Badge>
            </div>
          </SettingsCard>

          <div className="grid gap-4 xl:grid-cols-2">
            <SettingsCard title="Source health" copy="Current source rows from this GraphJin node.">
              {sources.length === 0 ? (
                <p className="text-ui-body-sm text-text2">GraphJin did not return source health rows.</p>
              ) : (
                <ul className="divide-y divide-border">
                  {sources.map((row) => (
                    <li key={row.id || row.source} className="flex items-start justify-between gap-3 py-3 first:pt-0 last:pb-0">
                      <div className="min-w-0">
                        <div className="truncate font-semibold text-text">{row.source}</div>
                        {row.summary ? <p className="mt-1 text-ui-body-sm text-text2">{row.summary}</p> : null}
                        <div className="mt-1 text-ui-caption text-text3">
                          {[row.source_kind ?? "source", row.database_name ?? "no database", `${row.table_count ?? 0} tables`].join(" · ")}
                          {row.duration_ms ? ` · ${row.duration_ms} ms` : ""}
                        </div>
                      </div>
                      <StatusBadge status={row.status} />
                    </li>
                  ))}
                </ul>
              )}
            </SettingsCard>

            <SettingsCard title="Recent runtime events" copy="The latest events GraphJin recorded.">
              {events.length === 0 ? (
                <p className="text-ui-body-sm text-text2">No recent events.</p>
              ) : (
                <ul className="divide-y divide-border">
                  {events.map((event) => (
                    <li key={event.id} className="flex items-start justify-between gap-3 py-3 first:pt-0 last:pb-0">
                      <div className="min-w-0">
                        <div className="text-ui-body-sm font-semibold text-text">{event.summary}</div>
                        <div className="mt-1 text-ui-caption text-text3">
                          {event.phase ?? "runtime"}
                          {event.created_at ? (
                            <>
                              {" · "}
                              <LocalDateTime value={event.created_at} relativeDay />
                            </>
                          ) : null}
                        </div>
                      </div>
                      <StatusBadge status={event.status} />
                    </li>
                  ))}
                </ul>
              )}
            </SettingsCard>
          </div>
        </>
      )}

      <div className="grid gap-4 xl:grid-cols-2">
        {security.ok ? (
          <SettingsCard
            title={security.data.summary?.title ?? "Security posture"}
            copy={securitySummaryText(security.data.summary)}
            aside={
              <Link href="/admin/graphjin/security" className="text-ui-body-sm font-semibold text-accent">
                Review
              </Link>
            }
          >
            {security.data.findings?.length ? (
              <ul className="grid gap-2">
                {security.data.findings.map((finding) => (
                  <li key={finding.id} className="flex items-center gap-3">
                    <StatusBadge status={finding.severity} />
                    <span className="min-w-0 truncate text-ui-body-sm text-text">{finding.title}</span>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-ui-body-sm text-text2">No findings.</p>
            )}
          </SettingsCard>
        ) : (
          <GraphjinUnavailable title="Security posture" error={security.error} />
        )}

        {coverage.ok ? (
          <SettingsCard
            title="Discovery coverage"
            copy={`${coverage.data.capabilities?.length ?? 0} system capabilities enabled.`}
            aside={
              <Link href="/admin/graphjin/catalog" className="text-ui-body-sm font-semibold text-accent">
                Browse
              </Link>
            }
          >
            <div className="flex flex-wrap gap-2">
              <Badge variant={coverage.data.tables?.length ? "success" : "watch"}>
                {coverage.data.tables?.length ? "Tables visible" : "No tables"}
              </Badge>
              <Badge variant={coverage.data.relationships?.length ? "success" : "watch"}>
                {coverage.data.relationships?.length ? "Relationships visible" : "No relationships"}
              </Badge>
              {(coverage.data.capabilities ?? []).slice(0, 8).map((capability) => (
                <Badge key={capability.id || capability.name} variant="outline">
                  {capability.name}
                </Badge>
              ))}
            </div>
          </SettingsCard>
        ) : (
          <GraphjinUnavailable title="Discovery coverage" error={coverage.error} />
        )}
      </div>

      <section className="mt-7">
        <h2 className="mb-4 font-display text-ui-caption font-semibold text-text3">Console</h2>
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {SECTION_CARDS.map((card) => (
            <Card
              as={Link}
              key={card.href}
              href={card.href}
              className="flex flex-col gap-2 no-underline transition-[border-color] hover:border-accent"
            >
              <h3 className="settings-card-title">{card.title}</h3>
              <p className="settings-card-copy">{card.copy}</p>
            </Card>
          ))}
        </div>
      </section>
    </GraphjinShell>
  );
}

function securitySummaryText(summary: SecuritySummary["summary"]): string {
  const data = parseGraphjinJson<{ summary?: unknown }>(summary?.summary_json, {});
  if (typeof data.summary === "string") return data.summary;
  return summary?.summary ?? "Read-only posture from GraphJin's security report.";
}
