import { describe, expect, it } from "vitest";
import {
  EMAIL_DIGEST_PROFILE,
  SLACK_PROFILE,
  VOICE_PROFILE,
  WEB_PROFILE,
  type InteractionEvent,
} from "@neko/interaction";
import {
  slackProjection,
  voiceProjection,
  webProjection,
  type SlackBlock,
} from "../src/index.js";

const inform: InteractionEvent = {
  kind: "inform",
  id: "o1",
  mood: "good",
  title: "Q3 revenue landed",
  body: "Revenue hit $4.7M, up 12% MoM. Strong enterprise pull this quarter.",
  metric: { label: "Revenue MTD", value: "$4.7M" },
  series: { kind: "line", points: [{ d: "Mon", v: 1 }, { d: "Tue", v: 2 }] },
};

const ask: InteractionEvent = {
  kind: "ask",
  id: "a1",
  ask: "approval",
  prompt: "Approve $5k refund to ACME?",
  decisionRef: "ar-123",
  risk: "medium",
};

const highlight: InteractionEvent = {
  kind: "highlight",
  id: "h1",
  metrics: [
    { label: "Top-10 share", value: "48%", sub: "down from 53%" },
    { label: "YoY revenue", value: "+14%" },
  ],
};

const blockTypes = (blocks: SlackBlock[]): string[] => blocks.map((b) => String(b.type));

describe("one inform, every substrate (degradation by profile)", () => {
  it("web keeps the full card with chart data", () => {
    const { surfaces, pendingAsks } = webProjection([inform], WEB_PROFILE);
    const update = surfaces[1] as { updateComponents: { components: Array<Record<string, unknown>> } };
    const card = update.updateComponents.components[0]!;
    expect(card.component).toBe("MetricCard");
    expect(card.metric).toBe("$4.7M");
    expect((card.chartData as unknown[]).length).toBe(2);
    expect(pendingAsks).toHaveLength(0);
  });

  it("slack keeps the card but drops the chart to a note", () => {
    const { blocks } = slackProjection([inform], SLACK_PROFILE);
    const section = blocks[0] as { text: { text: string } };
    expect(section.text.text).toContain("Q3 revenue landed");
    expect(blockTypes(blocks)).toContain("context");
    expect(JSON.stringify(blocks)).toContain("Chart available");
    expect(JSON.stringify(blocks)).not.toContain("Tue");
  });

  it("voice speaks the headline and metric, no chart", () => {
    const { ssml } = voiceProjection([inform], VOICE_PROFILE);
    expect(ssml.startsWith("<speak>")).toBe(true);
    expect(ssml).toContain("Q3 revenue landed");
    expect(ssml).toContain("Revenue MTD is $4.7M");
    expect(ssml).not.toContain("Tue");
  });
});

describe("one highlight, every substrate renders the figures its own way", () => {
  it("web renders the figures as a markdown component", () => {
    const { surfaces } = webProjection([highlight], WEB_PROFILE);
    const update = surfaces[1] as { updateComponents: { components: Array<Record<string, unknown>> } };
    const comp = update.updateComponents.components[0]!;
    expect(comp.component).toBe("Markdown");
    expect(String(comp.text)).toContain("**48%** Top-10 share — down from 53%");
    expect(String(comp.text)).toContain("**+14%** YoY revenue");
  });

  it("slack renders the figures as section fields", () => {
    const { blocks } = slackProjection([highlight], SLACK_PROFILE);
    const json = JSON.stringify(blocks);
    expect(json).toContain("Key figures");
    expect(json).toContain("Top-10 share");
    expect(json).toContain("down from 53%");
  });

  it("voice reads the figures aloud, no sub when absent", () => {
    const { ssml } = voiceProjection([highlight], VOICE_PROFILE);
    expect(ssml).toContain("Top-10 share is 48%, down from 53%.");
    expect(ssml).toContain("YoY revenue is +14%.");
  });
});

describe("ask negotiation branches on profile, never on channel identity", () => {
  it("same slack projection yields buttons under an inline-capable profile", () => {
    const { blocks } = slackProjection([ask], SLACK_PROFILE);
    expect(blockTypes(blocks)).toContain("actions");
    expect(JSON.stringify(blocks)).toContain('"action_id":"approve"');
  });

  it("and yields a link-back under a read-only profile — same function", () => {
    const { blocks } = slackProjection([ask], EMAIL_DIGEST_PROFILE);
    expect(blockTypes(blocks)).not.toContain("actions");
    expect(JSON.stringify(blocks)).toContain("Open the web dashboard");
  });

  it("web surfaces the ask to its native approval UI", () => {
    const { pendingAsks } = webProjection([ask], WEB_PROFILE);
    expect(pendingAsks).toHaveLength(1);
    expect(pendingAsks[0]!.decisionRef).toBe("ar-123");
  });

  it("voice speaks the ask as a yes/no", () => {
    const { ssml } = voiceProjection([ask], VOICE_PROFILE);
    expect(ssml).toContain("Say approve or reject");
  });
});
