import React, { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { graphqlRequest, relativeTime } from "../services/graphql";
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
  const highCount = findings.filter((finding) => ["critical", "high"].includes(finding.severity)).length;
  const weakenedCount = policies.filter((policy) => policy.weakens_default).length;

  return (
    <div className="grid max-w-6xl gap-6">
      <PageHeader
        title="Security"
      />

      {isLoading ? (
        <LoadingState />
      ) : error ? (
        <DataErrorState
          error={error}
          permissionMessage="Your role needs security.read to see this."
          unavailableMessage="Couldn't load security."
        />
      ) : (
        <>
          <div className="grid grid-cols-2 gap-x-8 gap-y-2 xl:grid-cols-4">
            <Metric label="Mode" value={summary?.mode || "unknown"} />
            <Metric label="Findings" value={findings.length} detail={`${highCount} high or critical`} tone={highCount ? "warn" : "good"} />
            <Metric label="Policies" value={policies.length} detail={`${weakenedCount} weaken defaults`} tone={weakenedCount ? "warn" : "good"} />
            <Metric label="Updated" value={summary?.updated_at ? relativeTime(summary.updated_at) : "Pending"} />
          </div>

          <Panel
            title="Findings"
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
              <div className="grid gap-5">
                {visibleFindings.map((finding) => (
                  <article className="min-w-0" key={finding.id}>
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <StatusPill status={finding.severity} />
                        <h3 className="min-w-0 truncate text-sm font-semibold">{finding.title}</h3>
                      </div>
                      <p className="mt-1.5 text-sm leading-6 text-muted-foreground">{finding.summary || finding.recommendation}</p>
                      <span className="mt-1 block font-mono text-[0.6875rem] text-muted-foreground">
                        {[finding.mode, finding.surface, finding.source || finding.database_name, finding.capability].filter(Boolean).join(" · ")}
                      </span>
                    </div>
                  </article>
                ))}
              </div>
            ) : (
              <EmptyState title="No findings" />
            )}
          </Panel>

          <Panel title="Policy">
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
              <EmptyState title="No policy rows" />
            )}
          </Panel>
        </>
      )}
    </div>
  );
};

export default SecurityAudit;
