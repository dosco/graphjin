import { basename, extname } from "node:path";
import { and, db, eq, metric, processing_job } from "@neko/db";
import { canonicalHash, type SolutionPackBundle } from "@neko/packs";
import { graphjinQuery, mintGraphjinToken } from "@neko/llm/graphjin";
import { extractValueAtPath, resolveWatcherVariables } from "@neko/llm/workflows";
import { mapSavedQueryMetric } from "../jobs/deterministic-metric.js";
import { packVariables } from "./declarative.js";
import { PackAuthorError, type PackAuthorDiagnostic } from "./errors.js";

function artifactRecord(artifact: SolutionPackBundle["artifacts"][number]): Record<string, unknown> {
  return artifact.content as Record<string, unknown>;
}

function savedQuery(bundle: SolutionPackBundle, name: string): string {
  const artifact = savedQueryArtifact(bundle, name);
  return String(artifact.content);
}

function savedQueryArtifact(bundle: SolutionPackBundle, name: string): SolutionPackBundle["artifacts"][number] {
  const artifact = bundle.artifacts.find((value) => value.kind === "saved_query" && basename(value.path, extname(value.path)) === name);
  if (!artifact || typeof artifact.content !== "string") throw new Error(`pack saved query ${name} is missing`);
  return artifact;
}

export type PackPreflightDiagnostic = PackAuthorDiagnostic & {
  code: "pack_query_preflight_failed";
  phase: "query_preflight";
  artifact: { kind: "saved_query"; name: string; path: string };
};

export class PackPreflightError extends PackAuthorError {
  constructor(name: string, path: string, causes: string[]) {
    const detail = causes.join("; ") || "GraphJin returned no data";
    const nextStep = "Check the saved query, source operation exposure, and credentials, then upload and review the corrected pack.";
    const diagnostic: PackPreflightDiagnostic = {
      code: "pack_query_preflight_failed",
      phase: "query_preflight",
      artifact: { kind: "saved_query", name, path },
      causes,
      nextStep,
    };
    super(`Pack query preflight failed for saved query "${name}" at ${path}: ${detail}. ${nextStep}`, diagnostic);
    this.name = "PackPreflightError";
  }
}

function graphjinEndpoint(url: string): string {
  const clean = url.replace(/\/+$/, "");
  return clean.endsWith("/api/v1/graphql") ? clean : `${clean}/api/v1/graphql`;
}

export async function runMagentoAnalyticsSmoke(endpoint: string, orgId: string): Promise<void> {
  const headers = { authorization: `Bearer ${mintGraphjinToken({ orgId, userId: "pack-health", role: "service", ttlSeconds: 60 })}` };
  const result = await graphjinQuery<{ sales_order?: Array<{ entity_id?: string | number }> }>({
    baseUrl: endpoint, headers,
    query: 'query MagentoPackAnalyticsSmoke { sales_order(where: { created_at: { gte: "1970-01-01 00:00:00" } }, limit: 1) { entity_id } }',
    signal: AbortSignal.timeout(30_000),
  });
  if (result.errors?.length || !Array.isArray(result.data?.sales_order)) throw new Error(`Magento analytics smoke query failed: ${result.errors?.map((error) => error.message).join("; ") ?? "sales_order result unavailable"}`);
  const operational = await graphjinQuery<{ sales_order?: Array<{ customer_id?: string | number; customer_email?: string; customer_firstname?: string }> }>({
    baseUrl: endpoint, headers,
    query: 'query MagentoPackOperationalDataCanary { sales_order(where: { created_at: { gte: "1970-01-01 00:00:00" } }, limit: 1) { customer_id customer_email customer_firstname } }',
    signal: AbortSignal.timeout(30_000),
  });
  if (operational.errors?.length || !Array.isArray(operational.data?.sales_order)) throw new Error(`Magento operational data check failed: ${operational.errors?.map((error) => error.message).join("; ") ?? "sales_order result unavailable"}`);
  const secret = await graphjinQuery<{ sales_order?: Array<{ protect_code?: string }> }>({
    baseUrl: endpoint, headers,
    query: 'query MagentoPackSecretColumnCanary { sales_order(where: { created_at: { gte: "1970-01-01 00:00:00" } }, limit: 1) { protect_code } }',
    signal: AbortSignal.timeout(30_000),
  });
  if (!secret.errors?.length) throw new Error("Magento secret-data check failed: GraphJin did not enforce the sales_order protect_code blocklist");
}

export async function runPackReadPreflight(bundle: SolutionPackBundle, endpoint: string, orgId: string, inputs: Record<string, unknown>): Promise<void> {
  const results = new Map<string, unknown>();
  const exercised = new Set<string>();
  const now = new Date();
  const run = async (name: string, variables?: unknown): Promise<unknown> => {
    const resolved = resolveWatcherVariables(packVariables(variables, inputs), now);
    const key = canonicalHash({ name, resolved });
    if (results.has(key)) return results.get(key);
    const artifact = savedQueryArtifact(bundle, name);
    let result: Awaited<ReturnType<typeof graphjinQuery>>;
    try {
      result = await graphjinQuery({
        baseUrl: graphjinEndpoint(endpoint), query: String(artifact.content), variables: resolved,
        headers: { authorization: `Bearer ${mintGraphjinToken({ orgId, userId: "pack-preflight", role: "service", ttlSeconds: 60 })}` },
        role: "service", signal: AbortSignal.timeout(30_000),
      });
    } catch (error) {
      throw new PackPreflightError(name, artifact.path, [error instanceof Error ? error.message : String(error)]);
    }
    if (result.errors?.length || !result.data) {
      const causes = result.errors?.map((error) => `${error.message}${error.path?.length ? ` (path: ${error.path.join(".")})` : ""}`) ?? [];
      throw new PackPreflightError(name, artifact.path, causes);
    }
    exercised.add(name); results.set(key, result.data); return result.data;
  };
  for (const artifact of bundle.artifacts) {
    if (artifact.kind !== "metric" && artifact.kind !== "watcher") continue;
    const value = artifactRecord(artifact);
    if (artifact.kind === "metric") {
      const execution = value.execution as Record<string, unknown>;
      const data = await run(String(execution.query), execution.variables);
      const mapping = execution.result as Record<string, unknown>;
      if (mapping.kind === "scalar" && typeof mapping.path === "string" && extractValueAtPath(data, mapping.path) == null) throw new Error(`pack metric ${artifact.key} result path is missing or null`);
      mapSavedQueryMetric({ definition: { ...value, execution: { ...execution, document: savedQuery(bundle, String(execution.query)) } }, data, baseline: null });
    } else {
      const data = await run(String(value.query), value.variables);
      if (extractValueAtPath(data, String(value.valuePath)) === undefined) throw new Error(`pack watcher ${artifact.key} result path is missing`);
    }
  }
  for (const artifact of bundle.artifacts.filter((value) => value.kind === "saved_query")) {
    const name = basename(artifact.path, extname(artifact.path));
    if (!exercised.has(name)) await run(name);
  }
}

export async function enqueuePackMetricRefreshes(orgId: string, bundle: SolutionPackBundle): Promise<number> {
  const { enqueue, QUEUE } = await import("@neko/db/jobs");
  let enqueued = 0;
  for (const artifact of bundle.artifacts.filter((value) => value.kind === "metric")) {
    const definition = artifactRecord(artifact);
    const [card] = await db().select({ id: metric.id }).from(metric).where(and(eq(metric.org_id, orgId), eq(metric.role, String(definition.role)), eq(metric.slug, artifact.targetRef))).limit(1);
    if (!card) continue;
    const [job] = await db().insert(processing_job).values({ org_id: orgId, kind: "metric_refresh", status: "queued", trigger: "pack_install", trigger_payload: { metricId: card.id } }).returning({ id: processing_job.id });
    if (!job) continue;
    await db().update(metric).set({ last_refresh_status: "pending", last_refresh_error: null, last_refresh_job_id: job.id, updated_at: new Date() }).where(eq(metric.id, card.id));
    try {
      await enqueue(QUEUE.METRIC_REFRESH, { processingJobId: job.id, orgId });
      enqueued++;
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      await db().update(processing_job).set({ status: "failed", error: message.slice(0, 1_000), finished_at: new Date(), updated_at: new Date() }).where(eq(processing_job.id, job.id));
      await db().update(metric).set({ last_refresh_status: "failed", last_refresh_error: message.slice(0, 500), updated_at: new Date() }).where(eq(metric.id, card.id));
      console.warn(`[packs] could not schedule initial refresh for ${artifact.targetRef}: ${message}`);
    }
  }
  return enqueued;
}
