import { readdir, stat } from "node:fs/promises";
import { extname, join } from "node:path";
import { pool } from "@neko/db";
import { getOrgAgentRoot } from "../../work/workspace";
import { reckonRunStatus, type ReckonExecutionMode, type ReckonRunStatus } from "./contract";

export type ReckonRunRow = {
  runId: string;
  orgId: string;
  workRunId: string;
  mode: ReckonExecutionMode;
  status: ReckonRunStatus;
  summary: string | null;
  error: string | null;
  progress: Record<string, unknown>;
  expired: boolean;
};

const MISSING_BATCH_ARTIFACT = "Batch run ended without publishing the required result.csv artifact.";

export async function getReckonRun(
  reckonWorkflowId: string,
  runId: string,
  now = new Date(),
): Promise<ReckonRunRow | null> {
  const { rows } = await pool().query<{
    org_id: string;
    work_run_id: string;
    execution_mode: ReckonExecutionMode;
    status: string;
    summary: string | null;
    error: string | null;
    progress: Record<string, unknown> | null;
    result_expires_at: Date | null;
  }>(
    `select compat.org_id, compat.work_run_id, compat.execution_mode,
            run.status, run.summary, run.error, run.progress, run.result_expires_at
       from compat_webhook_run compat
       join workflow_run run on run.id = compat.workflow_run_id
      where compat.reckon_workflow_id = $1 and compat.id = $2`,
    [reckonWorkflowId, runId],
  );
  const row = rows[0];
  if (!row) return null;
  return {
    runId,
    orgId: row.org_id,
    workRunId: row.work_run_id,
    mode: row.execution_mode,
    status: reckonRunStatus(row.status),
    summary: row.summary,
    error: row.error,
    progress: row.progress ?? {},
    expired: Boolean(row.result_expires_at && row.result_expires_at.getTime() <= now.getTime()),
  };
}

export type ReckonArtifact =
  | { ok: true; name: string; absolutePath: string; contentType: string; bytes: number }
  | { ok: false; reason: "none" | "ambiguous" | "missing_batch_result"; names: string[] };

export function reckonArtifactContentType(name: string): string {
  switch (extname(name).toLowerCase()) {
    case ".json":
      return "application/json; charset=utf-8";
    case ".md":
      return "text/markdown; charset=utf-8";
    case ".csv":
      return "text/csv; charset=utf-8";
    case ".txt":
      return "text/plain; charset=utf-8";
    default:
      return "application/octet-stream";
  }
}

/** Reckon's rule: result.json, else the only .json file, else the only file. */
export async function pickReckonArtifact(run: ReckonRunRow): Promise<ReckonArtifact> {
  const root = join(getOrgAgentRoot(run.orgId), "runs", run.workRunId, "artifacts");
  const entries = await readdir(root, { withFileTypes: true }).catch(() => []);
  const names = entries
    .filter((entry) => entry.isFile())
    .map((entry) => entry.name)
    .sort();
  if (run.mode === "batch" && !names.includes("result.csv")) {
    return { ok: false, reason: "missing_batch_result", names };
  }
  if (names.length === 0) return { ok: false, reason: "none", names };
  const reserved = names.find((name) => name.toLowerCase() === "result.json");
  const jsons = names.filter((name) => name.toLowerCase().endsWith(".json"));
  const chosen =
    run.mode === "batch"
      ? "result.csv"
      : (reserved ?? (jsons.length === 1 ? jsons[0] : names.length === 1 ? names[0] : undefined));
  if (!chosen) return { ok: false, reason: "ambiguous", names };
  const absolutePath = join(root, chosen);
  const info = await stat(absolutePath).catch(() => null);
  if (!info?.isFile()) return { ok: false, reason: "none", names };
  return {
    ok: true,
    name: chosen,
    absolutePath,
    contentType: reckonArtifactContentType(chosen),
    bytes: info.size,
  };
}

export function reckonFailureBody(run: ReckonRunRow): { status: number; body: Record<string, unknown> } {
  const message = run.error ?? (run.mode === "batch" ? MISSING_BATCH_ARTIFACT : "Run did not produce an artifact.");
  const phase = run.status === "error" ? "failed" : run.status === "needs_input" ? "needs_input" : "aborted";
  return {
    status: run.status === "error" ? 502 : 409,
    body: {
      error: "run_failed",
      status: run.status,
      runId: run.runId,
      message,
      progress: { ...run.progress, phase, message },
    },
  };
}

export function reckonMissingBatchArtifactBody(run: ReckonRunRow): {
  status: number;
  body: Record<string, unknown>;
} {
  return {
    status: 502,
    body: {
      error: "run_failed",
      status: "error",
      runId: run.runId,
      message: MISSING_BATCH_ARTIFACT,
      progress: { ...run.progress, phase: "failed", message: MISSING_BATCH_ARTIFACT },
    },
  };
}
