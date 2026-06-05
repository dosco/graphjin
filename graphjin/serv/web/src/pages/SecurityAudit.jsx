import React, { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ShieldAlert, ShieldCheck } from "lucide-react";
import { graphqlRequest, parseJSON } from "../services/graphql";
import { DataErrorState, EmptyState, LoadingState, Metric, PageHeader, Panel, StatusPill, cx } from "../components/ui";

const securityQuery = `query SecurityAudit {
  summary: gj_security(id: "summary") {
    id kind mode status severity title summary summary_json safety_json updated_at
  }
  findings: gj_security(where: { kind: { eq: "finding" } }, order_by: { severity_rank: desc }, limit: 80) {
    id scope config_id mode surface transport database_name source source_kind role capability action title summary severity severity_rank confidence status recommendation evidence_json details_json updated_at
  }
  policies: gj_security(where: { kind: { eq: "policy" } }, order_by: { source: asc }, limit: 100) {
    id scope mode surface source source_kind role capability action effective default_effective weakens_default read_only override_explicit override_key evidence_json
  }
}`;

const filters = ["all", "critical", "high", "medium", "low"];

const SecurityAudit = () => {
  const [severity, setSeverity] = useState("all");
  const { data, isLoading, error } = useQuery({
    queryKey: ["security-audit"],
    queryFn: () => graphqlRequest(securityQuery),
    staleTime: 30000,
  });
  const summary = data?.data?.summary;
  const findings = data?.data?.findings || [];
  const policies = data?.data?.policies || [];
  const visibleFindings = useMemo(
    () => findings.filter((finding) => severity === "all" || finding.severity === severity),
    [findings, severity]
  );
  const summaryJSON = parseJSON(summary?.summary_json, {});
  const highCount = findings.filter((finding) => ["critical", "high"].includes(finding.severity)).length;
  const weakenedCount = policies.filter((policy) => policy.weakens_default).length;

  return (
    <div className="page-stack">
      <PageHeader
        eyebrow="Audit"
        title="Security"
        description="Read-only posture, findings, and policy evidence from gj_security."
      />

      {isLoading ? (
        <LoadingState label="Reading gj_security" />
      ) : error ? (
        <DataErrorState
          error={error}
          permissionMessage="gj_security is an admin/operator surface. Grant security.read to the agentic operator role or inspect it from local development."
          unavailableMessage="The security root could not be reached from the current GraphJin service."
        />
      ) : (
        <>
          <div className="metric-grid">
            <Metric label="Mode" value={summary?.mode || "unknown"} detail={summary?.status || "status unknown"} />
            <Metric label="Findings" value={findings.length} detail={`${highCount} high or critical`} tone={highCount ? "warn" : "good"} />
            <Metric label="Policies" value={policies.length} detail={`${weakenedCount} weaken defaults`} tone={weakenedCount ? "warn" : "good"} />
            <Metric label="Updated" value={summary?.updated_at ? "Current" : "Pending"} detail={summary?.updated_at || "no timestamp"} />
          </div>

          <Panel title={summary?.title || "Security Summary"} description={summaryText(summary, summaryJSON)}>
            <div className="security-summary large">
              <ShieldCheck size={22} aria-hidden="true" />
              <div>
                <strong>{summaryJSON?.recommendation || summary?.summary || "Review visible findings before writes."}</strong>
                <p>{summaryJSON?.scope || "Use gj_catalog for ordinary discovery; detailed audit roots may be restricted by role."}</p>
              </div>
            </div>
          </Panel>

          <Panel
            title="Findings"
            description={`${visibleFindings.length} visible findings`}
            action={
              <div className="segment-control compact" role="tablist" aria-label="Severity filter">
                {filters.map((filter) => (
                  <button key={filter} className={cx(severity === filter && "active")} onClick={() => setSeverity(filter)} type="button">
                    {filter}
                  </button>
                ))}
              </div>
            }
          >
            {visibleFindings.length ? (
              <div className="finding-list">
                {visibleFindings.map((finding) => (
                  <article className="finding-card" key={finding.id}>
                    <div>
                      <div className="finding-title">
                        <StatusPill status={finding.severity} />
                        <h3>{finding.title}</h3>
                      </div>
                      <p>{finding.summary || finding.recommendation}</p>
                      <span className="muted-line">{[finding.mode, finding.surface, finding.source || finding.database_name, finding.capability].filter(Boolean).join(" · ")}</span>
                    </div>
                    <ShieldAlert size={18} aria-hidden="true" />
                  </article>
                ))}
              </div>
            ) : (
              <EmptyState title="No findings" message="No findings match this severity filter." />
            )}
          </Panel>

          <Panel title="Effective Policy" description="Policy rows that are visible to the current role.">
            {policies.length ? (
              <div className="data-table-wrap">
                <table className="data-table">
                  <thead>
                    <tr>
                      <th>Source</th>
                      <th>Role</th>
                      <th>Capability</th>
                      <th>Effective</th>
                      <th>Override</th>
                    </tr>
                  </thead>
                  <tbody>
                    {policies.slice(0, 40).map((policy) => (
                      <tr key={policy.id}>
                        <td>{policy.source || policy.surface || "-"}</td>
                        <td>{policy.role || "-"}</td>
                        <td>{policy.capability || policy.action || "-"}</td>
                        <td><StatusPill status={policy.effective || "unset"} /></td>
                        <td>{policy.override_explicit ? policy.override_key || "explicit" : "default"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <EmptyState title="No policy rows" message="No policy rows are visible for this role." />
            )}
          </Panel>
        </>
      )}
    </div>
  );
};

function summaryText(summary, data) {
  return data?.summary || summary?.summary || "Security posture is available through read-only gj_security rows.";
}

export default SecurityAudit;
