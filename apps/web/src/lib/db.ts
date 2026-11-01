/**
 * Web-app facade over @neko/db. Re-exports the shared org resolver so
 * routes and pages can `import { getOrgId } from "@/lib/db"`.
 *
 * Single-tenant for now — getOrgId() returns the id of the (currently
 * one) organization row, creating it on first call if none exists.
 */

export { getOrgId } from "@neko/db";

export type StageKind =
  | "business_profile_build"
  | "industry_insights_build"
  | "bootstrap_metrics_build"
  | "metric_refresh";

export type CurrentStage = {
  kind: StageKind;
  message: string | null;
};

export type MetricsProgress = {
  total: number;
  completed: number;
  failed: number;
};

export type OnboardingStatus =
  | { state: "needs_wizard" }
  | { state: "needs_persona" }
  | {
      state: "processing";
      jobId: string;
      currentStage?: CurrentStage;
      metricsProgress?: MetricsProgress;
    }
  | {
      state: "ready";
      profileVersion: number;
      seats: string[];
      mode: "shared" | "personal";
      metricsProgress?: MetricsProgress;
    }
  | { state: "failed"; jobId: string; message: string }
  | { state: "db_error"; message: string };
