import React from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";

import { compactNumber, graphqlRequest, relativeTime } from "../services/graphql";
import { DataErrorState, EmptyState, LoadingState, Metric, PageHeader, Panel, StatusPill } from "../components/ui";

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
  const readySources = sources.filter((source) => source.status === "ready").length;
  const degradedSources = sources.filter((source) => source.status !== "ready").length;

  return (
    <div className="grid max-w-6xl gap-8">
      <PageHeader
        title="Runtime"
        description={runtime.isLoading || runtime.error ? undefined : status?.summary || "Status unavailable"}
      />

      {runtime.isLoading ? (
        <LoadingState />
      ) : runtime.error ? (
        <DataErrorState
          error={runtime.error}
          permissionMessage="Your role needs runtime.read to see this."
          unavailableMessage="Couldn't load runtime status."
        />
      ) : (
        <>
          <section className="grid grid-cols-2 gap-x-8 gap-y-5 lg:grid-cols-4" aria-label="Runtime summary">
            <Metric label="Status" value={capitalize(status?.status || "unknown")} detail={[status?.mode, status?.store].filter(Boolean).join(" · ")} tone={status?.status === "ready" ? "good" : "warn"} />
            <Metric label="Sources" value={compactNumber(sources.length)} detail={`${readySources} ready`} tone={degradedSources ? "warn" : "good"} />
            <Metric label="Tables" value={compactNumber(status?.table_count)} detail={status?.schema_ready ? "schema ready" : "schema pending"} />
            <Metric label="Catalog" value={status?.catalog_revision ? "Ready" : "Pending"} detail={status?.active_database || "no active database"} />
          </section>

          <div className="grid gap-x-12 gap-y-8 xl:grid-cols-2">
            <Panel title="Sources">
              {sources.length ? (
                <ul className="grid gap-3">
                  {sources.map((source) => (
                    <li className="flex items-baseline justify-between gap-4" key={source.id || source.source}>
                      <div className="min-w-0">
                        <strong className="block truncate text-sm font-semibold">{source.source}</strong>
                        <span className="block truncate font-mono text-[0.6875rem] text-muted-foreground">
                          {source.source_kind || "source"} · {compactNumber(source.table_count)} tables{source.duration_ms > 0 ? ` · ${source.duration_ms} ms` : ""}
                        </span>
                        {source.status !== "ready" && source.summary && <p className="mt-1 text-sm text-muted-foreground">{source.summary}</p>}
                      </div>
                      <StatusPill status={source.status} severity={source.severity} />
                    </li>
                  ))}
                </ul>
              ) : (
                <EmptyState title="No sources" />
              )}
            </Panel>

            <Panel
              title="Security"
              action={<Link to="/admin/security" className="text-xs font-medium text-muted-foreground hover:text-foreground">All findings</Link>}
            >
              {security.isLoading ? (
                <LoadingState />
              ) : security.error ? (
                <DataErrorState
                  error={security.error}
                  permissionMessage="Your role needs security.read to see this."
                  unavailableMessage="Couldn't load security."
                />
              ) : findings.length ? (
                <ul className="grid gap-2.5">
                  {findings.map((finding) => (
                    <li className="flex items-center gap-3" key={finding.id}>
                      <StatusPill status={finding.severity} />
                      <span className="min-w-0 truncate text-sm">{finding.title}</span>
                    </li>
                  ))}
                </ul>
              ) : (
                <EmptyState title="No findings" />
              )}
            </Panel>

            <Panel title={capabilities.length ? `Capabilities · ${capabilities.length}` : "Capabilities"}>
              {catalog.isLoading ? (
                <LoadingState />
              ) : catalog.error ? (
                <DataErrorState
                  error={catalog.error}
                  permissionMessage="Your role needs catalog.read to see this."
                  unavailableMessage="Couldn't load the catalog."
                />
              ) : capabilities.length ? (
                <ul className="grid gap-x-6 gap-y-1.5 font-mono text-xs text-muted-foreground sm:grid-cols-2">
                  {capabilities.map((capability) => (
                    <li className="truncate" key={capability.id || capability.name}>{capability.name}</li>
                  ))}
                </ul>
              ) : (
                <EmptyState title="No capabilities" />
              )}
            </Panel>

            <Panel title="Recent events">
              {events.length ? (
                <ul className="grid gap-3">
                  {events.map((event) => (
                    <li className="flex items-baseline justify-between gap-4" key={event.id}>
                      <div className="min-w-0">
                        <strong className="block truncate text-sm font-medium">{event.summary}</strong>
                        <span className="font-mono text-[0.6875rem] text-muted-foreground">{event.phase || "runtime"} · {relativeTime(event.created_at)}</span>
                      </div>
                      <StatusPill status={event.status} severity={event.severity} />
                    </li>
                  ))}
                </ul>
              ) : (
                <EmptyState title="No recent events" />
              )}
            </Panel>
          </div>
        </>
      )}
    </div>
  );
};

function capitalize(value) {
  return value.charAt(0).toUpperCase() + value.slice(1);
}

export default Dashboard;
