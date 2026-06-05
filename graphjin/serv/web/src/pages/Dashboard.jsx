import React from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, Database, RefreshCw, ShieldCheck } from "lucide-react";
import { compactNumber, graphqlRequest, parseJSON, relativeTime } from "../services/graphql";
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
  const securitySummary = security.data?.data?.summary;
  const readySources = sources.filter((source) => source.status === "ready").length;
  const degradedSources = sources.filter((source) => source.status !== "ready").length;
  const runtimeDetails = parseJSON(status?.details_json, {});
  const runtimeRead = runtimeDetails?.runtime_read === true;

  return (
    <div className="page-stack">
      <PageHeader
        eyebrow="Runtime"
        title="System Overview"
        description="Live GraphJin health, source readiness, discovery coverage, and security posture from GraphQL system roots."
        actions={<span className="refresh-note"><RefreshCw size={14} aria-hidden="true" /> refreshes every 10s</span>}
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
          <div className="dashboard-hero">
            <div className="runtime-summary">
              <div className="runtime-status-line">
                <StatusPill status={status?.status} severity={status?.severity} />
                <span>{status?.mode || "mode unknown"}</span>
                {status?.store && <span>{status.store}</span>}
              </div>
              <h2>{status?.summary || "Runtime status is not available."}</h2>
              <p>{status?.next_action || "Query gj_runtime before guarded workflow, config, or schema actions."}</p>
            </div>
            <div className="metric-grid hero-metrics">
              <Metric label="Sources" value={compactNumber(sources.length)} detail={`${readySources} ready`} tone={degradedSources ? "warn" : "good"} />
              <Metric label="Tables" value={compactNumber(status?.table_count)} detail={status?.schema_ready ? "schema ready" : "schema pending"} />
              <Metric label="Catalog" value={status?.catalog_revision ? "Ready" : "Pending"} detail={status?.active_database || "no active database"} />
              <Metric label="Runtime Read" value={runtimeRead ? "On" : "Limited"} detail={status?.node_id || "current node"} tone={runtimeRead ? "good" : "warn"} />
            </div>
          </div>

          <div className="dashboard-grid">
            <Panel title="Source Health" description="Ephemeral gj_runtime source rows from the current node.">
              {sources.length ? (
                <div className="source-list">
                  {sources.map((source) => (
                    <div className="source-row" key={source.id || source.source}>
                      <div className="source-icon"><Database size={17} aria-hidden="true" /></div>
                      <div>
                        <strong>{source.source}</strong>
                        <p>{source.summary}</p>
                        <span>{source.source_kind || "source"} · {source.database_name || "no database"} · {compactNumber(source.table_count)} tables</span>
                      </div>
                      <div className="source-meta">
                        <StatusPill status={source.status} severity={source.severity} />
                        {source.duration_ms > 0 && <small>{source.duration_ms} ms</small>}
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
                <div className="security-brief">
                  <div className="security-summary">
                    <ShieldCheck size={18} aria-hidden="true" />
                    <div>
                      <strong>{securitySummary?.title || securitySummary?.summary || "Security summary"}</strong>
                      <p>{summaryText(securitySummary)}</p>
                    </div>
                  </div>
                  <div className="finding-list compact">
                    {findings.length ? findings.map((finding) => (
                      <div className="finding-row" key={finding.id}>
                        <StatusPill status={finding.severity} />
                        <span>{finding.title}</span>
                      </div>
                    )) : <EmptyState title="No findings returned" message="No security findings are visible for this role." />}
                  </div>
                </div>
              )}
            </Panel>
          </div>

          <div className="dashboard-grid">
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
                <>
                  <div className="metric-grid">
                    <Metric label="Capabilities" value={compactNumber(capabilities.length)} />
                    <Metric label="Tables" value={catalog.data?.data?.tables?.length ? "Visible" : "Hidden"} />
                    <Metric label="Relationships" value={catalog.data?.data?.relationships?.length ? "Visible" : "Hidden"} />
                  </div>
                  <div className="capability-strip">
                    {capabilities.slice(0, 5).map((capability) => (
                      <span key={capability.id || capability.name}>{capability.name}</span>
                    ))}
                  </div>
                </>
              )}
            </Panel>

            <Panel title="Recent Runtime Events" description="Bounded event rows from gj_runtime.">
              {events.length ? (
                <div className="event-list">
                  {events.map((event) => (
                    <div className="event-row" key={event.id}>
                      <StatusPill status={event.status} severity={event.severity} />
                      <div>
                        <strong>{event.summary}</strong>
                        <span>{event.phase || "runtime"} · {relativeTime(event.created_at)}</span>
                      </div>
                      <ArrowRight size={15} aria-hidden="true" />
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
