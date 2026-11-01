/**
 * Demo mode — flipped on with env var NEXT_PUBLIC_DEMO=true (preferred) or
 * the legacy DEMO=true server-side flag.
 *
 * NEXT_PUBLIC_DEMO is inlined into the client bundle by Next, so client
 * components (AppHeader's creator credit, etc.) can read the same flag
 * without a separate server roundtrip.
 *
 * When on, the app ignores the DB for user-facing data and serves canned
 * mock content:
 *   - /api/onboarding/status returns ready + default seats (no wizard bounce)
 *   - /api/briefing GET returns the ROLE_DATA mock with isExample=true
 *   - /api/briefing POST (chat Q&A) returns a random mock answer instead of
 *     enqueueing a metric_refresh job
 *
 * Useful for showcase/screenshots/video recordings without needing a
 * real postgres + LLM provider chain.
 */

import type { ChartDataPoint } from "@/a2ui/catalog";

export function isDemoMode(): boolean {
  return (
    process.env.NEXT_PUBLIC_DEMO === "true" || process.env.DEMO === "true"
  );
}

type MockCard = {
  metric: string;
  label: string;
  mood: "good" | "watch" | "bad";
  chartType: "kpi" | "line" | "bar" | "donut" | "area";
  // Keywords that should route a question to this card. Lowercase, matched
  // as substrings against the question text.
  keywords: string[];
};

const MOCK_CARDS: MockCard[] = [
  { metric: "$1.2M", label: "Weekly Revenue",  mood: "good",  chartType: "line",  keywords: ["revenue", "sales", "income", "topline", "top line"] },
  { metric: "42%",   label: "Conversion Rate", mood: "good",  chartType: "kpi",   keywords: ["conversion", "convert", "funnel", "signup"] },
  { metric: "-3.1%", label: "MoM Change",      mood: "watch", chartType: "line",  keywords: ["mom", "month over month", "trend", "change"] },
  { metric: "128",   label: "Active Users",    mood: "good",  chartType: "area",  keywords: ["users", "user", "active", "dau", "mau"] },
  { metric: "$340K", label: "Pipeline Added",  mood: "good",  chartType: "bar",   keywords: ["pipeline", "deals", "opportunities", "opps"] },
  { metric: "1.8x",  label: "LTV:CAC",         mood: "good",  chartType: "kpi",   keywords: ["ltv", "cac", "payback", "unit economics"] },
  { metric: "94%",   label: "Retention",       mood: "good",  chartType: "donut", keywords: ["retention", "churn", "retain"] },
  { metric: "+18%",  label: "Growth YoY",      mood: "good",  chartType: "area",  keywords: ["growth", "yoy", "year over year", "annual"] },
];

function pickCard(question: string): MockCard {
  const q = question.toLowerCase();
  const hit = MOCK_CARDS.find((c) => c.keywords.some((k) => q.includes(k)));
  return hit ?? MOCK_CARDS[Math.floor(Math.random() * MOCK_CARDS.length)];
}

function answerSentence(card: MockCard): string {
  const direction = card.mood === "bad" ? "down" : card.mood === "watch" ? "softening" : "up";
  return `${card.label} is ${card.metric} and trending ${direction} this period.`;
}

const DAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
const DONUT_LABELS = ["Enterprise", "Mid-market", "SMB", "Partner", "Other"];

function genChartData(chartType: string, metric: string): ChartDataPoint[] {
  switch (chartType) {
    case "kpi": {
      const num = parseFloat(metric.replace(/[^\d.]/g, "")) || 100;
      const prev = num * (0.85 + Math.random() * 0.2);
      return [{ d: metric, v: num, t: prev }];
    }
    case "donut":
      return DONUT_LABELS.map((d) => ({
        d,
        v: Math.floor(Math.random() * 40 + 10),
        t: 0,
      }));
    default:
      return DAYS.map((d) => ({
        d,
        v: Math.floor(Math.random() * 60 + 40),
        t: Math.floor(Math.random() * 40 + 50),
      }));
  }
}

export type MockChatAnswer = {
  text: string;
  metric: string;
  label: string;
  mood: string;
  detail: string;
  chartType: string;
  chartData: ChartDataPoint[];
};

export function mockChatResponse(question: string): MockChatAnswer {
  const card = pickCard(question);
  return {
    text: answerSentence(card),
    metric: card.metric,
    label: card.label,
    mood: card.mood,
    detail: "Sample answer shown in demo mode. Connect a data source to see real numbers.",
    chartType: card.chartType,
    chartData: genChartData(card.chartType, card.metric),
  };
}

// Seats the demo dashboard shows — must match keys in ROLE_DATA.
export const DEMO_SEATS = ["CEO", "CFO", "CRO"] as const;
