import React from "react";
import { useQuery } from "@tanstack/react-query";
import { FileText, FlaskConical } from "lucide-react";
import { useSearchParams } from "react-router-dom";

import { DataErrorState, EmptyState, LoadingState, Metric, PageHeader, Panel, StatusPill } from "../components/ui";
import { MarkdownContent } from "@/components/ui/markdown-content";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { fetchEvalReport, fetchEvalReports } from "../services/evals";
import { relativeTime } from "../services/graphql";

const EvalReports = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const listQuery = useQuery({
    queryKey: ["eval-reports"],
    queryFn: fetchEvalReports,
    staleTime: 15000,
  });
  const reports = listQuery.data?.reports || [];
  const requestedRun = searchParams.get("run");
  const selectedRun = reports.some((report) => report.run_id === requestedRun)
    ? requestedRun
    : reports[0]?.run_id || "";
  const detailQuery = useQuery({
    queryKey: ["eval-report", selectedRun],
    queryFn: () => fetchEvalReport(selectedRun),
    enabled: Boolean(selectedRun && listQuery.data?.available),
    staleTime: 30000,
  });

  const selectRun = (runID) => {
    const next = new URLSearchParams(searchParams);
    next.set("run", runID);
    setSearchParams(next);
  };

  return (
    <div className="mx-auto grid max-w-[92rem] gap-6">
      <PageHeader
        eyebrow="Trainer"
        title="Evaluation reports"
        description="Shareable benchmark outcomes, method checks, safety gates, and provider usage from the local GraphJin eval store."
      />

      {listQuery.isLoading ? (
        <LoadingState label="Reading evaluation reports" />
      ) : listQuery.error ? (
        <DataErrorState
          error={listQuery.error}
          permissionMessage="Trainer reports are restricted to the current GraphJin operator/admin role."
          unavailableMessage="The evaluation report API is unavailable on this GraphJin service."
        />
      ) : !listQuery.data?.available || reports.length === 0 ? (
        <EvalReportsEmpty stateDir={listQuery.data?.state_dir} available={listQuery.data?.available} />
      ) : (
        <div className="grid min-w-0 gap-5 lg:grid-cols-[20rem_minmax(0,1fr)]">
          <Panel
            title="Runs"
            description={`${reports.length} stored report${reports.length === 1 ? "" : "s"}`}
            contentClassName="p-0 sm:p-0"
          >
            <div className="divide-y">
              {reports.map((report) => (
                <button
                  key={report.run_id}
                  type="button"
                  onClick={() => selectRun(report.run_id)}
                  aria-pressed={selectedRun === report.run_id}
                  className={`grid w-full gap-2 px-4 py-4 text-left transition hover:bg-muted/45 ${selectedRun === report.run_id ? "bg-muted/60" : "bg-background"}`}
                >
                  <div className="flex min-w-0 items-center justify-between gap-3">
                    <strong className="truncate text-sm font-medium">{report.provenance?.model || "Unknown model"}</strong>
                    <StatusPill status={reportStatus(report)} />
                  </div>
                  <span className="truncate font-mono text-xs text-muted-foreground">{report.run_id}</span>
                  <span className="flex items-center justify-between gap-3 text-xs text-muted-foreground">
                    <span>{reportListResult(report)}</span>
                    <span>{relativeTime(report.generated_at)}</span>
                  </span>
                </button>
              ))}
            </div>
          </Panel>

          <div className="min-w-0">
            {detailQuery.isLoading ? (
              <LoadingState label="Loading report" />
            ) : detailQuery.error ? (
              <DataErrorState
                error={detailQuery.error}
                permissionMessage="This report is restricted to the current GraphJin operator/admin role."
                unavailableMessage="The selected evaluation report could not be loaded."
              />
            ) : detailQuery.data ? (
              <EvalReportDetail report={detailQuery.data} />
            ) : null}
          </div>
        </div>
      )}
    </div>
  );
};

function EvalReportDetail({ report }) {
  const summary = report.friendly_summary || {};
  const complete = summary.complete === true;
  return (
    <div className="grid min-w-0 gap-5">
      {complete ? (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
          <Metric label="Questions passed reliably" value={countOf(summary.questions_passed_reliably, summary.question_count)} detail={formatPercent(report.recall)} tone={report.accepted ? "good" : "warn"} />
          <Metric label="Solved at least once" value={countOf(summary.questions_solved_at_least_once, summary.question_count)} detail={formatPercent(report.pass_at_k)} />
          <Metric label="Solved every time" value={countOf(summary.questions_solved_every_time, summary.question_count)} detail={formatPercent(report.pass_power_k)} />
          <Metric label="Complete DB method" value={formatPercent(report.method_recall)} detail="database did the full calculation" />
          <Metric label="Rules followed" value={formatPercent(report.safety_precision)} detail={report.safety_precision === 1 ? "all safety checks passed" : "a safety check failed"} tone={report.safety_precision === 1 ? "good" : "warn"} />
        </div>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2">
          <Metric label="Test attempts" value={countOf(summary.completed_test_attempts, summary.planned_test_attempts)} detail="completed work is saved" tone="warn" />
          <Metric label="Questions" value={summary.question_count || "n/a"} detail="no overall score is available" />
        </div>
      )}

      <Tabs defaultValue="summary" className="grid min-w-0 gap-4">
        <TabsList className="w-fit">
          <TabsTrigger value="summary">Plain-language summary</TabsTrigger>
          <TabsTrigger value="technical">Technical benchmark</TabsTrigger>
        </TabsList>
        <TabsContent value="summary" className="mt-0">
          <Panel
            title={summary.title || report.model || "Evaluation summary"}
            description={`${report.model || "Unknown model"} · ${report.run_id} · ${relativeTime(report.generated_at)}`}
            action={<StatusPill status={reportStatus(report)} />}
          >
            <MarkdownContent value={report.markdown} className="gap-5 text-sm" />
          </Panel>
        </TabsContent>
        <TabsContent value="technical" className="mt-0">
          <Panel
            title="Technical benchmark report"
            description="Industry-standard metrics, provenance, fingerprints, task verdicts, and provider accounting."
            action={<StatusPill status={reportStatus(report)} />}
          >
            <MarkdownContent value={report.technical_markdown} className="gap-5 text-sm" />
          </Panel>
        </TabsContent>
      </Tabs>
    </div>
  );
}

function EvalReportsEmpty({ stateDir, available }) {
  const location = stateDir || ".graphjin-evals";
  return (
    <Panel title="No reports yet" description={available ? "The eval store is ready." : "GraphJin did not find an eval store at the configured path."}>
      <EmptyState
        title="Run the public benchmark"
        message={`GraphJin looked in ${location}. Run “graphjin eval bench --public --yes” to create the first report, then refresh this page.`}
      />
      <div className="mt-4 flex items-start gap-3 rounded-lg bg-muted/45 p-4 text-sm text-muted-foreground">
        {available ? <FileText className="mt-0.5 size-4" aria-hidden="true" /> : <FlaskConical className="mt-0.5 size-4" aria-hidden="true" />}
        <code className="break-all font-mono text-xs">{location}</code>
      </div>
    </Panel>
  );
}

function formatPercent(value) {
  const number = Number(value);
  return Number.isFinite(number) ? `${(number * 100).toFixed(1)}%` : "n/a";
}

function countOf(value, total) {
  const count = Number(value);
  const maximum = Number(total);
  return Number.isFinite(count) && Number.isFinite(maximum) ? `${count} of ${maximum}` : "n/a";
}

function reportListResult(report) {
  const summary = report.friendly_summary || {};
  if (summary.complete) {
    return `${countOf(summary.questions_passed_reliably, summary.question_count)} reliable`;
  }
  return `${countOf(summary.completed_test_attempts, summary.planned_test_attempts)} attempts`;
}

function reportStatus(report) {
  if (report.run_status === "environment_failed") return "stopped";
  if (report.run_status === "interrupted") return "paused";
  if (report.accepted) return "passed";
  return report.run_status === "complete" ? "needs improvement" : report.run_status || "pending";
}

export default EvalReports;
export { EvalReportDetail, reportListResult, reportStatus };
