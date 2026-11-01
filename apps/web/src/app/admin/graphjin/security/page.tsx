import Link from "next/link";
import { connection } from "next/server";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { graphjinAdminQuery, parseGraphjinJson } from "@/lib/graphjin-admin";
import { AdminDenied } from "../../AdminShell";
import { buttonClassName } from "@/components/ui/button";
import { StatCard, StatGrid } from "@/components/ui/stat-card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { GraphjinShell, GraphjinUnavailable, SettingsCard, StatusBadge, firstParam } from "../GraphjinAdmin";

const SECURITY_QUERY = `query AdminGraphjinSecurity {
  summary: gj_security(id: "summary") {
    id mode status severity title summary summary_json updated_at
  }
  findings: gj_security(where: { kind: { eq: "finding" } }, order_by: { severity_rank: desc }, limit: 80) {
    id mode surface database_name source capability title summary severity recommendation
  }
  policies: gj_security(where: { kind: { eq: "policy" } }, order_by: { source: asc }, limit: 100) {
    id surface source role capability action effective weakens_default override_explicit override_key
  }
}`;

const SEVERITIES = ["all", "critical", "high", "medium", "low"];

type SecurityData = {
  summary: {
    mode: string | null;
    status: string | null;
    title: string | null;
    summary: string | null;
    summary_json: unknown;
    updated_at: string | null;
  } | null;
  findings: Array<{
    id: string;
    mode: string | null;
    surface: string | null;
    database_name: string | null;
    source: string | null;
    capability: string | null;
    title: string | null;
    summary: string | null;
    severity: string | null;
    recommendation: string | null;
  }>;
  policies: Array<{
    id: string;
    surface: string | null;
    source: string | null;
    role: string | null;
    capability: string | null;
    action: string | null;
    effective: string | null;
    weakens_default: boolean | null;
    override_explicit: boolean | null;
    override_key: string | null;
  }>;
};

export default async function AdminGraphjinSecurityPage({
  searchParams,
}: {
  searchParams: Promise<{ severity?: string | string[] }>;
}) {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;

  const requested = firstParam((await searchParams).severity);
  const severity = SEVERITIES.includes(requested) ? requested : "all";
  const result = await graphjinAdminQuery<SecurityData>(
    { orgId: await getOrgId(), userId: actor.userId },
    SECURITY_QUERY,
  );

  return (
    <GraphjinShell
      current="/admin/graphjin/security"
      title="Security"
      subtitle="Read-only posture, findings, and policy evidence from GraphJin."
    >
      {!result.ok ? (
        <GraphjinUnavailable title="Security" error={result.error} />
      ) : (
        <SecurityReport data={result.data} severity={severity} />
      )}
    </GraphjinShell>
  );
}

function SecurityReport({ data, severity }: { data: SecurityData; severity: string }) {
  const findings = data.findings ?? [];
  const policies = data.policies ?? [];
  const summary = data.summary;
  const summaryJson = parseGraphjinJson<{ summary?: string; recommendation?: string; scope?: string }>(summary?.summary_json, {});
  const visible = findings.filter((finding) => severity === "all" || finding.severity === severity);
  const highCount = findings.filter((finding) => finding.severity === "critical" || finding.severity === "high").length;
  const weakenedCount = policies.filter((policy) => policy.weakens_default).length;

  return (
    <>
      <StatGrid label="Security summary">
        <StatCard label="Mode" value={summary?.mode ?? "Unknown"} detail={summary?.status ?? undefined} />
        <StatCard label="Findings" value={findings.length} detail={`${highCount} high or critical`} tone={highCount ? "warn" : "ok"} />
        <StatCard label="Policies" value={policies.length} detail={`${weakenedCount} weaken defaults`} tone={weakenedCount ? "warn" : "ok"} />
        <StatCard label="Updated" value={summary?.updated_at ? "Current" : "Pending"} detail={summary?.updated_at ?? undefined} />
      </StatGrid>

      <SettingsCard
        title={summary?.title ?? "Security summary"}
        copy={summaryJson.summary ?? summary?.summary ?? "Security posture from GraphJin's read-only security report."}
      >
        <p className="text-ui-body-sm font-semibold text-text">
          {summaryJson.recommendation ?? "Review visible findings before enabling writes."}
        </p>
        {summaryJson.scope ? <p className="mt-1 text-ui-body-sm text-text2">{summaryJson.scope}</p> : null}
      </SettingsCard>

      <SettingsCard title="Findings" copy={`${visible.length} of ${findings.length} findings`}>
        <nav aria-label="Filter by severity" className="mb-4 flex flex-wrap gap-2">
          {SEVERITIES.map((value) => (
            <Link
              key={value}
              href={value === "all" ? "/admin/graphjin/security" : `/admin/graphjin/security?severity=${value}`}
              aria-current={value === severity ? "page" : undefined}
              className={buttonClassName({ size: "sm", variant: value === severity ? "primary" : "secondary" })}
            >
              {value.charAt(0).toUpperCase() + value.slice(1)}
            </Link>
          ))}
        </nav>
        {visible.length === 0 ? (
          <p className="text-ui-body-sm text-text2">No findings match this severity.</p>
        ) : (
          <ul className="divide-y divide-border">
            {visible.map((finding) => (
              <li key={finding.id} className="grid gap-1.5 py-3 first:pt-0 last:pb-0">
                <div className="flex flex-wrap items-center gap-2">
                  <StatusBadge status={finding.severity} />
                  <span className="min-w-0 font-semibold text-text">{finding.title}</span>
                </div>
                {finding.summary || finding.recommendation ? (
                  <p className="text-ui-body-sm text-text2">{finding.summary || finding.recommendation}</p>
                ) : null}
                <div className="text-ui-caption text-text3">
                  {[finding.mode, finding.surface, finding.source || finding.database_name, finding.capability].filter(Boolean).join(" · ")}
                </div>
              </li>
            ))}
          </ul>
        )}
      </SettingsCard>

      <SettingsCard title="Effective policy" copy="Policy rows visible to the admin role.">
        {policies.length === 0 ? (
          <p className="text-ui-body-sm text-text2">No policy rows.</p>
        ) : (
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
              {policies.map((policy) => (
                <TableRow key={policy.id}>
                  <TableCell>{policy.source || policy.surface || "—"}</TableCell>
                  <TableCell>{policy.role || "—"}</TableCell>
                  <TableCell>{policy.capability || policy.action || "—"}</TableCell>
                  <TableCell>
                    <StatusBadge status={policy.effective || "unset"} />
                  </TableCell>
                  <TableCell>{policy.override_explicit ? policy.override_key || "explicit" : "default"}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </SettingsCard>
    </>
  );
}
