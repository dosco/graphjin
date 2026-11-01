import { NextRequest, NextResponse } from "next/server";
import {
  and,
  asc,
  db,
  desc,
  eq,
  metric,
  metric_snapshot,
  or,
  processing_job,
  sql,
} from "@neko/db";
import { enqueue, QUEUE } from "@neko/db/jobs";
import {
  classifyQuestion,
  type QuestionClassificationDiagnostics,
} from "@neko/llm";
import { webProjection } from "@neko/channels";
import { WEB_PROFILE } from "@neko/interaction";
import { outputRowToInteractionEvent } from "@neko/llm/interaction";
import type { A2UIComponent, A2UIMessage } from "@/a2ui/types";
import { CATALOG_ID } from "@/a2ui/catalog";
import { getOrgId } from "@/lib/db";
import { heldItemIds, holdsItem } from "@/lib/entitlements";
import { isDemoMode, mockChatResponse } from "@/lib/demo-mode";

function genTimeSeriesData() {
  return Array.from({ length: 7 }, (_, i) => ({
    d: ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"][i],
    v: Math.floor(Math.random() * 60 + 40),
    t: Math.floor(Math.random() * 40 + 50),
  }));
}

function genDonutData() {
  const labels = ["Enterprise", "Mid-market", "SMB", "Partner", "Other"];
  return labels.map((label) => ({
    d: label,
    v: Math.floor(Math.random() * 40 + 10),
    t: 0,
  }));
}

function genKpiData(metric: string, currentRaw: number, prevRaw: number) {
  return [{ d: metric, v: currentRaw, t: prevRaw }];
}

function moodGreeting(
  insights: Array<{ mood: string; state?: string; metric: string }>,
) {
  const counts = { good: 0, watch: 0, bad: 0, pending: 0, failed: 0 };
  for (const ins of insights) {
    if (ins.state === "failed") counts.failed++;
    else if (ins.state === "pending" || ins.metric === "Fetching…") counts.pending++;
    else if (ins.mood === "good") counts.good++;
    else if (ins.mood === "bad") counts.bad++;
    else counts.watch++;
  }
  if (counts.pending > insights.length / 2) {
    return {
      greeting: "Just a moment.",
      subtitle: "Reading the latest numbers for you.",
    };
  }
  if (counts.bad > 0 || counts.failed > 0) {
    const flags = counts.bad + counts.failed;
    return {
      greeting: flags === 1 ? "One thing to flag." : `${flags} things to flag.`,
      subtitle: "Worth your attention right now.",
    };
  }
  if (counts.watch > 0) {
    return {
      greeting: "Mostly on track.",
      subtitle: counts.watch === 1 ? "One thing worth watching." : `${counts.watch} things worth watching.`,
    };
  }
  return {
    greeting: "Everything looks healthy today.",
    subtitle: "Nothing demanding your attention.",
  };
}

function genChartData(chartType: string, metric: string) {
  switch (chartType) {
    case "kpi": {
      const num = parseFloat(metric.replace(/[^\d.]/g, "")) || 100;
      const prev = num * (0.85 + Math.random() * 0.2);
      return genKpiData(metric, num, prev);
    }
    case "donut":
      return genDonutData();
    default:
      return genTimeSeriesData();
  }
}

/**
 * The metric cards the operator sees are produced by the built-in web channel:
 * each loop output (a metric snapshot) becomes an InteractionEvent, which the
 * web channel's projection turns into the BriefingCard the renderer consumes.
 * The dashboard chrome (metricId/source/state — web-specific affordances, not
 * modality-free intent) is stamped back on after projection.
 */
interface InsightLike {
  id: string;
  mood: string;
  text: string;
  metric: string;
  label?: string;
  detail?: string;
  chartType: string;
  metricId?: string;
  source?: string;
  state?: string;
  error?: string;
  chartDataOverride?: Array<{ d: string; v: number; t?: number }>;
  fromDb?: boolean;
}

const SERIES_KINDS = ["kpi", "line", "bar", "area", "donut"] as const;
type SeriesKind = (typeof SERIES_KINDS)[number];

const toSeriesKind = (c: string): SeriesKind =>
  (SERIES_KINDS as readonly string[]).includes(c) ? (c as SeriesKind) : "line";

const toWaistMood = (m: string): "good" | "watch" | "act" =>
  m === "bad" ? "act" : m === "good" ? "good" : "watch";

const chartDataForInsight = (ins: InsightLike): Array<{ d: string; v: number; t?: number }> => {
  if (ins.fromDb) return ins.chartDataOverride ?? [];
  if (ins.chartDataOverride && ins.chartDataOverride.length > 0) return ins.chartDataOverride;
  return genChartData(ins.chartType, ins.metric);
};

export function briefingCardsViaChannel(insights: InsightLike[]): A2UIComponent[] {
  return insights.map((ins) => {
    const inform = outputRowToInteractionEvent({
      id: ins.id,
      mood: toWaistMood(ins.mood),
      title: ins.text,
      body: ins.detail ?? "",
      metric: { label: ins.label ?? "", value: ins.metric },
      series: { kind: toSeriesKind(ins.chartType), points: chartDataForInsight(ins) },
    });
    const update = webProjection([inform], WEB_PROFILE).surfaces.find(
      (m) => "updateComponents" in m,
    ) as { updateComponents: { components: Array<Record<string, unknown>> } } | undefined;
    const card =
      update?.updateComponents.components.find(
        (c) => c.component === "MetricCard" || c.component === "BriefingCard",
      ) ?? {};
    // Restamp dashboard identity + render-state (web affordances, not waist concepts).
    return {
      ...card,
      id: ins.id,
      component: "BriefingCard",
      metricId: ins.metricId ?? ins.id,
      source: ins.source ?? "bootstrap",
      state: ins.state ?? "ok",
      error: ins.error,
      mood: ins.mood,
    } as A2UIComponent;
  });
}

const ROLE_DATA: Record<string, {
  greeting: string;
  subtitle: string;
  insights: Array<{
    id: string;
    mood: string;
    text: string;
    metric: string;
    label: string;
    detail: string;
    chartType: string;
  }>;
}> = {
  CEO: {
    greeting: "Everything looks healthy today.",
    subtitle: "One thing worth watching.",
    insights: [
      { id: "ceo-1", mood: "good", text: "Revenue is 3.2% above target this month", metric: "$4.7M", label: "Revenue MTD", detail: "$4.7M vs $4.55M target. Driven by strong renewals in enterprise.", chartType: "kpi" },
      { id: "ceo-2", mood: "good", text: "Team velocity is the highest it's been in 6 sprints", metric: "142 pts", label: "Sprint Velocity", detail: "142 story points shipped vs 128 average. No increase in bug rate.", chartType: "bar" },
      { id: "ceo-3", mood: "watch", text: "APAC churn ticked up to 4.1% — was 2.8% last month", metric: "4.1%", label: "APAC Churn", detail: "3 mid-market accounts in Singapore flagged. CS team is on it.", chartType: "line" },
      { id: "ceo-4", mood: "good", text: "NPS improved to 72 from 68 last quarter", metric: "72", label: "NPS Score", detail: "Key driver: onboarding redesign. Detractor comments down 40%.", chartType: "kpi" },
    ],
  },
  CFO: {
    greeting: "Finances are in good shape.",
    subtitle: "Runway is comfortable. OpEx trending slightly high.",
    insights: [
      { id: "cfo-1", mood: "good", text: "Runway is 18.4 months at current burn rate", metric: "18.4 mo", label: "Runway", detail: "$12.2M in bank. Monthly burn $660K. Improving from 16 months last quarter.", chartType: "kpi" },
      { id: "cfo-2", mood: "watch", text: "OpEx is 8% over budget — cloud costs spiked", metric: "+8%", label: "OpEx vs Budget", detail: "AWS bill up $42K due to GPU usage for model training. Consider reserved instances.", chartType: "bar" },
      { id: "cfo-3", mood: "good", text: "Collections are 94% on time this quarter", metric: "94%", label: "Collections", detail: "Only 2 invoices past 45 days. Both from government accounts (expected).", chartType: "kpi" },
      { id: "cfo-4", mood: "good", text: "Gross margin holding steady at 71%", metric: "71%", label: "Gross Margin", detail: "Up from 68% last year. SaaS revenue mix improving.", chartType: "area" },
    ],
  },
  CRO: {
    greeting: "Pipeline is strong this quarter.",
    subtitle: "You're 82% to target with 5 weeks left.",
    insights: [
      { id: "cro-1", mood: "good", text: "Pipeline value is $8.2M — 2.4x of remaining target", metric: "$8.2M", label: "Pipeline", detail: "28 qualified opportunities. Weighted forecast: $3.4M expected to close.", chartType: "area" },
      { id: "cro-2", mood: "good", text: "Demo-to-close rate improved to 34% from 28%", metric: "34%", label: "Conversion", detail: "New demo script performing well. Average deal cycle down to 22 days.", chartType: "kpi" },
      { id: "cro-3", mood: "watch", text: "Inbound leads down 12% this month vs last", metric: "-12%", label: "Inbound Leads", detail: "Blog traffic dropped after SEO algo change. Content team adjusting.", chartType: "line" },
      { id: "cro-4", mood: "good", text: "Average deal size up 18% to $68K", metric: "$68K", label: "Avg Deal", detail: "Enterprise tier adoption driving this. 4 deals above $100K in pipeline.", chartType: "kpi" },
    ],
  },
};

type SnapshotPayload = {
  headlineMetric?: string;
  headlineLabel?: string;
  insightText?: string;
  detailText?: string;
  chartType?: string;
  chartData?: Array<{ d: string; v: number; t?: number }>;
} | null;

function describeRefreshError(error: string | null | undefined): string {
  const text = error ?? "";
  if (/ECONNREFUSED|connection refused|dial tcp|ENOTFOUND|getaddrinfo/i.test(text)) {
    return "OpenNeko could not reach the data source. It tries again at the next refresh.";
  }
  if (/timeout|timed out|ETIMEDOUT/i.test(text)) {
    return "The data source did not answer in time. OpenNeko tries again at the next refresh.";
  }
  return "This metric did not refresh. OpenNeko tries again at the next refresh.";
}

export async function GET(request: NextRequest) {
  const requestedRole = request.nextUrl.searchParams.get("role") ?? "CEO";
  const isOverview = requestedRole === "__overview__";
  const role = isOverview ? "Overview" : requestedRole;
  const surfaceId = isOverview
    ? "briefing-overview"
    : `briefing-${role.toLowerCase()}`;
  const demo = isDemoMode();
  if (!isOverview && !(await holdsItem("dashboard", role))) {
    return NextResponse.json({ error: "Dashboard not found" }, { status: 404 });
  }
  const heldMetrics = await heldItemIds("metric");

  const unfilteredCards = demo
    ? []
    : await db()
        .query.metric.findMany({
          where: isOverview
            ? and(
                eq(metric.org_id, (await getOrgId())),
                eq(metric.active, true),
              )
            : and(
                eq(metric.org_id, (await getOrgId())),
                or(eq(metric.role, role), sql`${metric.source} like 'pack:%'`),
                eq(metric.active, true),
              ),
          orderBy: asc(metric.slug),
          with: {
            snapshots: {
              orderBy: desc(metric_snapshot.captured_at),
              limit: 1,
            },
          },
        })
        .catch(() => [] as Array<never>);
  const cards = heldMetrics === "*" ? unfilteredCards : unfilteredCards.filter((m) => heldMetrics.has(m.id));

  const dbInsights = cards.map((m) => {
    const snap = m.snapshots?.[0];
    const p = snap?.payload as SnapshotPayload;
    const hasData = !!p;
    const state =
      m.last_refresh_status === "failed"
        ? "failed"
        : m.last_refresh_status === "pending"
          ? "pending"
          : hasData
            ? "ok"
            : "pending";
    return {
      id: m.slug,
      metricId: m.id,
      source: m.source,
      state,
      error: state === "failed" ? (m.last_refresh_error ?? "Unknown error") : undefined,
      mood: state === "failed" ? "bad" : state === "pending" ? "watch" : ((snap?.status ?? "watch") as string),
      text: m.title,
      metric:
        state === "failed"
          ? "Couldn't load"
          : state === "ok"
            ? (p?.headlineMetric ?? "—")
            : "Fetching…",
      label: state === "ok" ? (p?.headlineLabel ?? "") : "",
      detail:
        state === "failed"
          ? describeRefreshError(m.last_refresh_error)
          : state === "ok"
            ? [p?.insightText, p?.detailText].filter(Boolean).join(" ")
            : (m.why ?? ""),
      chartType: p?.chartType ?? m.chart_hint ?? "line",
      chartDataOverride: state === "ok" ? p?.chartData : [],
      fromDb: true,
    };
  });

  const hasRealCards = dbInsights.length > 0;
  const roleData = hasRealCards
    ? {
        ...moodGreeting(dbInsights),
        insights: dbInsights,
      }
    : (ROLE_DATA[role] ?? ROLE_DATA.CEO);

  const messages: A2UIMessage[] = [];

  messages.push({
    version: "v1.0",
    createSurface: {
      surfaceId,
      catalogId: CATALOG_ID,
    },
  });

  const dataModel: Record<string, unknown> = {
    role,
    greeting: roleData.greeting,
    subtitle: roleData.subtitle,
    insights: Object.fromEntries(
      roleData.insights.map((ins) => {
        const override = (ins as { chartDataOverride?: Array<{ d: string; v: number; t?: number }> }).chartDataOverride;
        const fromDb = (ins as { fromDb?: boolean }).fromDb === true;
        const chartData = fromDb
          ? (override ?? [])
          : (override && override.length > 0 ? override : genChartData(ins.chartType, ins.metric));
        return [
          ins.id,
          { state: "ok", error: undefined, ...ins, chartData },
        ];
      })
    ),
  };

  messages.push({
    version: "v1.0",
    updateDataModel: {
      surfaceId,
      value: dataModel,
    },
  });

  const insightIds = roleData.insights.map((ins) => ins.id);

  const components = [
    {
      id: "root",
      component: "Briefing",
      greeting: { path: "/greeting" },
      subtitle: { path: "/subtitle" },
      role: { path: "/role" },
      isExample: !hasRealCards,
      children: insightIds,
    },
    ...briefingCardsViaChannel(roleData.insights as InsightLike[]),
  ];

  messages.push({
    version: "v1.0",
    updateComponents: {
      surfaceId,
      components,
    },
  });

  return NextResponse.json(messages, {
    headers: { "Content-Type": "application/a2ui+json" },
  });
}

export async function POST(request: NextRequest) {
  const body = await request.json();
  const { surfaceId, message } = body as { surfaceId: string; message: string };

  if (!message?.trim()) {
    return NextResponse.json({ error: "empty message" }, { status: 400 });
  }

  if (isDemoMode()) {
    const answer = mockChatResponse(message);
    const chatId = `chat-demo-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;
    return NextResponse.json({ mock: true, chatId, answer });
  }

  const role = (surfaceId?.replace("briefing-", "") ?? "ceo").toUpperCase();
  const orgId = await getOrgId();

  console.log(
    `[chat] org=${orgId} role=${role} surface=${surfaceId} q=${JSON.stringify(message).slice(0, 500)}`,
  );

  let card;
  let classification: QuestionClassificationDiagnostics | undefined;
  try {
    card = await classifyQuestion(message, role, orgId, {
      onDiagnostics(value) {
        classification = value;
      },
    });
    console.log(
      `[chat] org=${orgId} classify -> slug=${card.slug} chartHint=${card.chartHint} title=${JSON.stringify(card.title).slice(0, 200)}`,
    );
  } catch (e) {
    const err = e instanceof Error ? e.message : "classify failed";
    console.error(`[chat] org=${orgId} classify failed: ${err}`);
    if (e instanceof Error && e.stack) console.error(e.stack);
    return NextResponse.json({ error: err }, { status: 500 });
  }

  const inserted = await db()
    .insert(processing_job)
    .values({
      org_id: orgId,
      kind: "metric_refresh",
      status: "queued",
      trigger: "chat",
      trigger_payload: {
        question: message,
        slug: card.slug,
        title: card.title,
        why: card.why,
        chartHint: card.chartHint,
        role,
        ...(classification ? { classification } : {}),
      },
    })
    .returning({ id: processing_job.id });
  const jobId = inserted[0]?.id;
  if (!jobId) {
    console.error(`[chat] org=${orgId} insert processing_job returned no id`);
    return NextResponse.json({ error: "failed to enqueue job" }, { status: 500 });
  }

  await enqueue(QUEUE.METRIC_REFRESH, {
    processingJobId: jobId,
    orgId,
  });
  console.log(`[chat] org=${orgId} enqueued metric_refresh job=${jobId}`);

  return NextResponse.json({
    jobId,
    chatId: `chat-${jobId}`,
    skeleton: {
      text: card.title,
      metric: "Loading…",
      label: "",
      detail: card.why,
      chartType: card.chartHint,
      chartData: [],
      mood: "watch",
    },
  });
}
