import Link from "next/link";
import { connection } from "next/server";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { graphjinAdminGet, type GraphjinAdminCaller } from "@/lib/graphjin-admin";
import { AdminDenied } from "../../AdminShell";
import { Disclosure } from "@/components/ui/disclosure";
import { LocalDateTime } from "@/components/ui/local-date-time";
import { StatCard, StatGrid } from "@/components/ui/stat-card";
import { GraphjinShell, GraphjinUnavailable, SettingsCard, StatusBadge, firstParam } from "../GraphjinAdmin";

type FriendlySummary = {
  complete?: boolean;
  title?: string;
  question_count?: number;
  questions_passed_reliably?: number;
  questions_solved_at_least_once?: number;
  questions_solved_every_time?: number;
  completed_test_attempts?: number;
  planned_test_attempts?: number;
};
type ReportListItem = {
  run_id: string;
  generated_at?: string;
  run_status?: string;
  accepted?: boolean;
  provenance?: { model?: string };
  friendly_summary?: FriendlySummary;
};
type ReportList = { available?: boolean; state_dir?: string; reports?: ReportListItem[] };
type ReportDetail = ReportListItem & {
  model?: string;
  markdown?: string;
  technical_markdown?: string;
  recall?: number;
  pass_at_k?: number;
  pass_power_k?: number;
  method_recall?: number;
  safety_precision?: number;
};

export default async function AdminGraphjinEvalsPage({
  searchParams,
}: {
  searchParams: Promise<{ run?: string | string[] }>;
}) {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;

  const caller = { orgId: await getOrgId(), userId: actor.userId };
  const list = await graphjinAdminGet<ReportList>(caller, "eval/reports");

  return (
    <GraphjinShell
      current="/admin/graphjin/evals"
      title="Eval reports"
      subtitle="Benchmark outcomes, method checks, safety gates, and provider usage from GraphJin's trainer."
    >
      {!list.ok ? (
        <GraphjinUnavailable
          title="Eval reports"
          error={list.status === 404 ? "This GraphJin does not serve eval reports. They are available in dev and agentic modes." : list.error}
        />
      ) : (
        <EvalReports caller={caller} list={list.data} requested={firstParam((await searchParams).run)} />
      )}
    </GraphjinShell>
  );
}

async function EvalReports({
  caller,
  list,
  requested,
}: {
  caller: GraphjinAdminCaller;
  list: ReportList;
  requested: string;
}) {
  const reports = list.reports ?? [];
  if (!list.available || reports.length === 0) {
    return (
      <SettingsCard
        title="No reports yet"
        copy={`GraphJin looked in ${list.state_dir || ".graphjin-evals"}. Run "graphjin eval bench --public --yes" to create the first report, then refresh this page.`}
      />
    );
  }
  const selected = reports.some((report) => report.run_id === requested) ? requested : reports[0].run_id;
  const detail = await graphjinAdminGet<ReportDetail>(caller, `eval/reports/${encodeURIComponent(selected)}`);

  return (
    <div className="grid min-w-0 gap-4 lg:grid-cols-[20rem_minmax(0,1fr)]">
      <SettingsCard title="Runs" copy={`${reports.length} stored report${reports.length === 1 ? "" : "s"}`}>
        <ul className="divide-y divide-border">
          {reports.map((report) => (
            <li key={report.run_id}>
              <Link
                href={`/admin/graphjin/evals?run=${encodeURIComponent(report.run_id)}`}
                aria-current={report.run_id === selected ? "page" : undefined}
                className={`grid gap-1 rounded-inner px-2 py-3 no-underline hover:bg-neutral-soft ${report.run_id === selected ? "bg-neutral-soft" : ""}`}
              >
                <span className="flex min-w-0 items-center justify-between gap-2">
                  <span className="truncate font-semibold text-text">{report.provenance?.model ?? "Unknown model"}</span>
                  <StatusBadge status={reportStatus(report)} />
                </span>
                <span className="truncate font-mono text-xs text-text3">{report.run_id}</span>
                <span className="flex justify-between gap-2 text-ui-caption text-text2">
                  <span>{reportListResult(report)}</span>
                  {report.generated_at ? <LocalDateTime value={report.generated_at} relativeDay /> : null}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      </SettingsCard>

      <div className="min-w-0">
        {detail.ok ? <EvalReportDetail report={detail.data} /> : <GraphjinUnavailable title="Report" error={detail.error} />}
      </div>
    </div>
  );
}

function EvalReportDetail({ report }: { report: ReportDetail }) {
  const summary = report.friendly_summary ?? {};
  return (
    <div className="grid min-w-0 gap-4">
      {summary.complete ? (
        <StatGrid label="Report summary" className="lg:grid-cols-3">
          <StatCard
            label="Passed reliably"
            value={countOf(summary.questions_passed_reliably, summary.question_count)}
            detail={formatPercent(report.recall)}
            tone={report.accepted ? "ok" : "warn"}
          />
          <StatCard
            label="Solved at least once"
            value={countOf(summary.questions_solved_at_least_once, summary.question_count)}
            detail={formatPercent(report.pass_at_k)}
          />
          <StatCard
            label="Solved every time"
            value={countOf(summary.questions_solved_every_time, summary.question_count)}
            detail={formatPercent(report.pass_power_k)}
          />
          <StatCard label="Complete DB method" value={formatPercent(report.method_recall)} detail="Database did the full calculation" />
          <StatCard
            label="Rules followed"
            value={formatPercent(report.safety_precision)}
            detail={report.safety_precision === 1 ? "All safety checks passed" : "A safety check failed"}
            tone={report.safety_precision === 1 ? "ok" : "warn"}
          />
        </StatGrid>
      ) : (
        <StatGrid label="Report summary" className="lg:grid-cols-2">
          <StatCard
            label="Test attempts"
            value={countOf(summary.completed_test_attempts, summary.planned_test_attempts)}
            detail="Completed work is saved"
            tone="warn"
          />
          <StatCard label="Questions" value={summary.question_count ?? "n/a"} detail="No overall score yet" />
        </StatGrid>
      )}

      <SettingsCard
        title={summary.title ?? report.model ?? "Evaluation summary"}
        copy={[report.model ?? report.provenance?.model, report.run_id].filter(Boolean).join(" · ")}
        aside={<StatusBadge status={reportStatus(report)} />}
      >
        {report.markdown ? (
          <div className="work-markdown text-sm leading-[1.55] text-text2">
            <ReactMarkdown remarkPlugins={[remarkGfm]}>{report.markdown}</ReactMarkdown>
          </div>
        ) : (
          <p className="text-ui-body-sm text-text2">This report has no summary.</p>
        )}
      </SettingsCard>

      {report.technical_markdown ? (
        <Disclosure title="Technical benchmark report" meta="Metrics, provenance, task verdicts, provider usage">
          <div className="work-markdown text-sm leading-[1.55] text-text2">
            <ReactMarkdown remarkPlugins={[remarkGfm]}>{report.technical_markdown}</ReactMarkdown>
          </div>
        </Disclosure>
      ) : null}
    </div>
  );
}

function formatPercent(value: unknown): string {
  const number = Number(value);
  return value != null && Number.isFinite(number) ? `${(number * 100).toFixed(1)}%` : "n/a";
}

function countOf(value: unknown, total: unknown): string {
  const count = Number(value);
  const maximum = Number(total);
  return value != null && total != null && Number.isFinite(count) && Number.isFinite(maximum) ? `${count} of ${maximum}` : "n/a";
}

function reportListResult(report: ReportListItem): string {
  const summary = report.friendly_summary ?? {};
  if (summary.complete) return `${countOf(summary.questions_passed_reliably, summary.question_count)} reliable`;
  return `${countOf(summary.completed_test_attempts, summary.planned_test_attempts)} attempts`;
}

function reportStatus(report: ReportListItem): string {
  if (report.run_status === "environment_failed") return "stopped";
  if (report.run_status === "interrupted") return "paused";
  if (report.accepted) return "passed";
  return report.run_status === "complete" ? "needs improvement" : report.run_status || "pending";
}
