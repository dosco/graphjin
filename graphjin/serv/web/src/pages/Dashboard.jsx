import React from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, Database, RefreshCw, ShieldCheck } from "lucide-react";

import { compactNumber, graphqlRequest, parseJSON, relativeTime } from "../services/graphql";
import { DataErrorState, EmptyState, LoadingState, Metric, PageHeader, Panel, StatusPill } from "../components/ui";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";

const runtimeQuery = `query RuntimeDashboard {
  status: gj_runtime(where: { kind: { eq: "status" } }, limit: 1) {
    id kind created_at node_id mode store phase status severity summary next_action active_database schema_ready table_count catalog_revision details_json suggested_next_json
  }
  sources: gj_runtime(where: { kind: { eq: "source" } }, order_by: { source: asc }, limit: 80) {
    id kind created_at source source_kind database_name status severity schema_ready table_count duration_ms summary next_action details_json
  }
  events: gj_runtime(where: { kind: { eq: "event" } }, order_by: { created_at: desc }, limit: 8) {
    id kind created_at phase status severity source database_name summary next_action details_json
  }
}`;

const catalogQuery = `query DashboardCatalog {
  capabilities: gj_catalog(where: { kind: { eq: "system_capability" } }, order_by: { name: asc }, limit: 80) {
    id name kind summary enabled source source_kind details_json safety_json
  }
  tables: gj_catalog(where: { kind: { eq: "table" } }, limit: 1) {
    id
  }
  relationships: gj_catalog(where: { kind: { eq: "relationship" } }, limit: 1) {
    id
  }
}`;

const securityQuery = `query DashboardSecurity {
  summary: gj_security(id: "summary") {
    id kind mode severity status title summary summary_json safety_json
  }
  findings: gj_security(where: { kind: { eq: "finding" } }, order_by: { severity_rank: desc }, limit: 6) {
    id severity severity_rank status title recommendation surface source source_kind updated_at
  }
}`;

const Dashboard = () => {
  const runtime = useQuery({
    queryKey: ["runtime-dashboard"],
    queryFn: () => graphqlRequest(runtimeQuery),
    refetchInterval: 10000,
  });
  const catalog = useQuery({
    queryKey: ["dashboard-catalog"],
    queryFn: () => graphqlRequest(catalogQuery),
    staleTime: 60000,
  });
  const security = useQuery({
    queryKey: ["dashboard-security"],
    queryFn: () => graphqlRequest(securityQuery),
    staleTime: 30000,
  });

  const status = runtime.data?.data?.status?.[0];
  const sources = runtime.data?.data?.sources || [];
  const events = runtime.data?.data?.events || [];
  const capabilities = catalog.data?.data?.capabilities || [];
  const findings = security.data?.data?.findings || [];
  const securitySummary = security.data?.data?.summary;
  const readySources = sources.filter((source) => source.status === "ready").length;
  const degradedSources = sources.filter((source) => source.status !== "ready").length;
  const runtimeDetails = parseJSON(status?.details_json, {});
  const runtimeRead = runtimeDetails?.runtime_read === true;

  return (
    <div className="mx-auto grid max-w-7xl gap-6">
      <PageHeader
        eyebrow="Runtime"
        title="System Overview"
        description="Live GraphJin health, source readiness, discovery coverage, and security posture from GraphQL system roots."
        actions={
          <Badge variant="outline" className="gap-2 px-3 py-1.5">
            <RefreshCw className="size-3.5" aria-hidden="true" />
            refreshes every 10s
          </Badge>
        }
      />

      {runtime.isLoading ? (
        <LoadingState label="Reading gj_runtime" />
      ) : runtime.error ? (
        <DataErrorState
          error={runtime.error}
          permissionMessage="gj_runtime is an operator-only system root. Open the console in local development or grant runtime.read/admin access to the agentic operator role."
          unavailableMessage="The runtime root could not be reached from the current GraphJin service."
        />
      ) : (
        <>
          <Card className="overflow-hidden">
            <CardContent className="grid gap-6 p-5 xl:grid-cols-[1.25fr_0.75fr]">
              <div className="flex min-h-48 flex-col justify-center gap-4">
                <div className="flex flex-wrap items-center gap-2">
                  <StatusPill status={status?.status} severity={status?.severity} />
                  <Badge variant="outline">{status?.mode || "mode unknown"}</Badge>
                  {status?.store && <Badge variant="secondary">{status.store}</Badge>}
                </div>
                <div className="grid gap-3">
                  <h2 className="max-w-3xl text-2xl font-semibold leading-tight tracking-normal md:text-4xl">
                    {status?.summary || "Runtime status is not available."}
                  </h2>
                  <p className="max-w-3xl text-sm leading-6 text-muted-foreground md:text-base">
                    {status?.next_action || "Query gj_runtime before guarded workflow, config, or schema actions."}
                  </p>
                </div>
              </div>
              <div className="grid gap-3 sm:grid-cols-2">
                <Metric label="Sources" value={compactNumber(sources.length)} detail={`${readySources} ready`} tone={degradedSources ? "warn" : "good"} />
                <Metric label="Tables" value={compactNumber(status?.table_count)} detail={status?.schema_ready ? "schema ready" : "schema pending"} />
                <Metric label="Catalog" value={status?.catalog_revision ? "Ready" : "Pending"} detail={status?.active_database || "no active database"} />
                <Metric label="Runtime Read" value={runtimeRead ? "On" : "Limited"} detail={status?.node_id || "current node"} tone={runtimeRead ? "good" : "warn"} />
              </div>
            </CardContent>
          </Card>

          <div className="grid gap-4 xl:grid-cols-2">
            <Panel title="Source Health" description="Ephemeral gj_runtime source rows from the current node.">
              {sources.length ? (
                <div className="grid gap-2">
                  {sources.map((source) => (
                    <div className="grid gap-3 rounded-md border bg-muted/30 p-3 sm:grid-cols-[2.25rem_minmax(0,1fr)_auto] sm:items-center" key={source.id || source.source}>
                      <div className="flex size-9 items-center justify-center rounded-md border bg-background text-muted-foreground">
                        <Database size={17} aria-hidden="true" />
                      </div>
                      <div className="min-w-0">
                        <strong className="block truncate text-sm font-medium">{source.source}</strong>
                        <p className="mt-1 truncate text-sm text-muted-foreground">{source.summary}</p>
                        <span className="mt-1 block text-xs text-muted-foreground">
                          {source.source_kind || "source"} · {source.database_name || "no database"} · {compactNumber(source.table_count)} tables
                        </span>
                      </div>
                      <div className="flex items-center gap-2 sm:flex-col sm:items-end">
                        <StatusPill status={source.status} severity={source.severity} />
                        {source.duration_ms > 0 && <small className="text-xs text-muted-foreground">{source.duration_ms} ms</small>}
                      </div>
                    </div>
                  ))}
                </div>
              ) : (
                <EmptyState title="No source rows" message="gj_runtime did not return current source health rows." />
              )}
            </Panel>

            <Panel title="Security Posture" description="Read-only summary from gj_security.">
              {security.isLoading ? (
                <LoadingState label="Reading gj_security" />
              ) : security.error ? (
                <DataErrorState
                  error={security.error}
                  permissionMessage="gj_security is intended for admins and governed agentic operators. Grant that role security.read access when this console is exposed."
                  unavailableMessage="The security root could not be reached from the current GraphJin service."
                />
              ) : (
                <div className="grid gap-4">
                  <div className="flex gap-3 rounded-md border bg-muted/30 p-4">
                    <ShieldCheck className="mt-0.5 size-5 text-emerald-700" aria-hidden="true" />
                    <div>
                      <strong className="text-sm font-medium">{securitySummary?.title || securitySummary?.summary || "Security summary"}</strong>
                      <p className="mt-1 text-sm leading-6 text-muted-foreground">{summaryText(securitySummary)}</p>
                    </div>
                  </div>
                  <div className="grid gap-2">
                    {findings.length ? findings.map((finding) => (
                      <div className="flex items-center gap-3 rounded-md border bg-background p-3" key={finding.id}>
                        <StatusPill status={finding.severity} />
                        <span className="min-w-0 truncate text-sm">{finding.title}</span>
                      </div>
                    )) : <EmptyState title="No findings returned" message="No security findings are visible for this role." />}
                  </div>
                </div>
              )}
            </Panel>
          </div>

          <div className="grid gap-4 xl:grid-cols-2">
            <Panel title="Discovery Coverage" description="Catalog system capabilities and schema coverage.">
              {catalog.isLoading ? (
                <LoadingState label="Reading gj_catalog" />
              ) : catalog.error ? (
                <DataErrorState
                  error={catalog.error}
                  permissionMessage="gj_catalog is available to operator roles. Grant catalog.read access or use the console from local development."
                  unavailableMessage="The catalog root could not be reached from the current GraphJin service."
                />
              ) : (
                <div className="grid gap-4">
                  <div className="grid gap-3 sm:grid-cols-3">
                    <Metric label="Capabilities" value={compactNumber(capabilities.length)} />
                    <Metric label="Tables" value={catalog.data?.data?.tables?.length ? "Visible" : "Hidden"} />
                    <Metric label="Relationships" value={catalog.data?.data?.relationships?.length ? "Visible" : "Hidden"} />
                  </div>
                  <div className="flex flex-wrap gap-2">
                    {capabilities.slice(0, 8).map((capability) => (
                      <Badge variant="outline" key={capability.id || capability.name}>{capability.name}</Badge>
                    ))}
                  </div>
                </div>
              )}
            </Panel>

            <Panel title="Recent Runtime Events" description="Bounded event rows from gj_runtime.">
              {events.length ? (
                <div className="grid gap-2">
                  {events.map((event) => (
                    <div className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 rounded-md border bg-muted/30 p-3" key={event.id}>
                      <StatusPill status={event.status} severity={event.severity} />
                      <div className="min-w-0">
                        <strong className="block truncate text-sm font-medium">{event.summary}</strong>
                        <span className="text-xs text-muted-foreground">{event.phase || "runtime"} · {relativeTime(event.created_at)}</span>
                      </div>
                      <ArrowRight className="size-4 text-muted-foreground" aria-hidden="true" />
                    </div>
                  ))}
                </div>
              ) : (
                <EmptyState title="No recent events" message="Runtime event storage has not recorded recent rows." />
              )}
            </Panel>
          </div>
        </>
      )}
    </div>
  );
};

function summaryText(summary) {
  const data = parseJSON(summary?.summary_json, {});
  if (typeof data?.summary === "string") {
    return data.summary;
  }
  return summary?.summary || "Security rows are available through gj_security.";
}

export default Dashboard;
