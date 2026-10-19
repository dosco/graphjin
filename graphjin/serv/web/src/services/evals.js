import { fetchJSON } from "./graphql";

export function fetchEvalReports() {
  return fetchJSON("/api/v1/eval/reports");
}

export function fetchEvalReport(runID) {
  return fetchJSON(`/api/v1/eval/reports/${encodeURIComponent(runID)}`);
}
