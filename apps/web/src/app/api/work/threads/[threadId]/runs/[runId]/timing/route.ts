import { NextRequest } from "next/server";
import { startupEvent, withStartupTrace } from "@neko/telemetry/startup";
import { getOrgId } from "@/lib/db";
import { getAuthorizedWorkThread } from "@/lib/work-thread-auth";
import { getWorkRun } from "@/lib/work-store";

export const runtime = "nodejs";
const allowed = new Set(["acknowledgementMs", "streamOpenMs", "firstEventMs", "firstOutputMs", "paintOpportunityMs", "doneMs", "documentVisible"]);
export async function POST(request: NextRequest, context: { params: Promise<{ threadId: string; runId: string }> }) {
  const { threadId, runId } = await context.params;
  const orgId = await getOrgId();
  if (!await getAuthorizedWorkThread(orgId, threadId)) return new Response(null, { status: 404 });
  const run = await getWorkRun(orgId, runId);
  if (!run || run.thread_id !== threadId) return new Response(null, { status: 404 });
  const body: unknown = await request.json().catch(() => null);
  if (!body || typeof body !== "object" || Array.isArray(body)) return new Response(null, { status: 400 });
  const values = Object.entries(body);
  if (!values.length || values.some(([key, value]) => !allowed.has(key) || typeof value !== "number" || !Number.isFinite(value) || value < 0 || value > (key === "documentVisible" ? 1 : 3_600_000))) return new Response(null, { status: 400 });
  withStartupTrace({ threadId, runId }, () => startupEvent("browser.startup", { origin: "client_reported", ...Object.fromEntries(values) }));
  return new Response(null, { status: 204 });
}
