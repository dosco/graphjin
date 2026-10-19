import React, { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ShieldAlert, ShieldCheck } from "lucide-react";

import { graphqlRequest, parseJSON } from "../services/graphql";
import { DataErrorState, EmptyState, LoadingState, Metric, PageHeader, Panel, StatusPill } from "../components/ui";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

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
    <div className="mx-auto grid max-w-7xl gap-6">
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
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <Metric label="Mode" value={summary?.mode || "unknown"} detail={summary?.status || "status unknown"} />
            <Metric label="Findings" value={findings.length} detail={`${highCount} high or critical`} tone={highCount ? "warn" : "good"} />
            <Metric label="Policies" value={policies.length} detail={`${weakenedCount} weaken defaults`} tone={weakenedCount ? "warn" : "good"} />
            <Metric label="Updated" value={summary?.updated_at ? "Current" : "Pending"} detail={summary?.updated_at || "no timestamp"} />
          </div>

          <Panel title={summary?.title || "Security Summary"} description={summaryText(summary, summaryJSON)}>
            <div className="flex gap-3 rounded-md border bg-muted/30 p-4">
              <ShieldCheck className="mt-0.5 size-5 text-emerald-700" aria-hidden="true" />
              <div>
                <strong className="text-sm font-medium">{summaryJSON?.recommendation || summary?.summary || "Review visible findings before writes."}</strong>
                <p className="mt-1 text-sm leading-6 text-muted-foreground">
                  {summaryJSON?.scope || "Use gj_catalog for ordinary discovery; detailed audit roots may be restricted by role."}
                </p>
              </div>
            </div>
          </Panel>

          <Panel
            title="Findings"
            description={`${visibleFindings.length} visible findings`}
            action={
              <Tabs value={severity} onValueChange={setSeverity}>
                <TabsList>
                  {filters.map((filter) => (
                    <TabsTrigger key={filter} value={filter} className="capitalize">
                      {filter}
                    </TabsTrigger>
                  ))}
                </TabsList>
              </Tabs>
            }
          >
            {visibleFindings.length ? (
              <div className="grid gap-3">
                {visibleFindings.map((finding) => (
                  <article className="grid gap-3 rounded-md border bg-background p-4 md:grid-cols-[minmax(0,1fr)_auto]" key={finding.id}>
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <StatusPill status={finding.severity} />
                        <h3 className="min-w-0 truncate text-sm font-semibold">{finding.title}</h3>
                      </div>
                      <p className="mt-2 text-sm leading-6 text-muted-foreground">{finding.summary || finding.recommendation}</p>
                      <span className="mt-2 block text-xs text-muted-foreground">
                        {[finding.mode, finding.surface, finding.source || finding.database_name, finding.capability].filter(Boolean).join(" · ")}
                      </span>
                    </div>
                    <ShieldAlert className="size-5 text-muted-foreground" aria-hidden="true" />
                  </article>
                ))}
              </div>
            ) : (
              <EmptyState title="No findings" message="No findings match this severity filter." />
            )}
          </Panel>

          <Panel title="Effective Policy" description="Policy rows that are visible to the current role.">
            {policies.length ? (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Source</TableHead>
                    <TableHead>Role</TableHead>
                    <TableHead>Capability</TableHead>
                    <TableHead>Effective</TableHead>
                    <TableHead>Override</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {policies.slice(0, 40).map((policy) => (
                    <TableRow key={policy.id}>
                      <TableCell>{policy.source || policy.surface || "-"}</TableCell>
                      <TableCell>{policy.role || "-"}</TableCell>
                      <TableCell>{policy.capability || policy.action || "-"}</TableCell>
                      <TableCell><StatusPill status={policy.effective || "unset"} /></TableCell>
                      <TableCell>{policy.override_explicit ? policy.override_key || "explicit" : "default"}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
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
