import { describe, it, expect } from "vitest";
import { TELEGRAM_PROFILE, type InteractionEvent } from "@neko/interaction";
import { projectTelegram } from "../../src/telegram/projection";

describe("projectTelegram", () => {
  it("renders an inform as HTML with mood icon, bold title, and metric", () => {
    const events: InteractionEvent[] = [
      {
        kind: "inform",
        id: "i1",
        mood: "act",
        title: "Revenue dropped",
        body: "Q3 is down 12%.\n\nDetails follow in the report.",
        metric: { label: "Q3 Revenue", value: "$1.2M" },
      },
    ];
    const msgs = projectTelegram(events, TELEGRAM_PROFILE);
    expect(msgs).toHaveLength(1);
    const msg = msgs[0]!;
    expect(msg.parse_mode).toBe("HTML");
    expect(msg.text).toContain("🚨");
    expect(msg.text).toContain("<b>Revenue dropped</b>");
    expect(msg.text).toContain("<b>Q3 Revenue:</b> $1.2M");
    // summary fidelity keeps only the first paragraph
    expect(msg.text).toContain("Q3 is down 12%.");
    expect(msg.text).not.toContain("Details follow");
    expect(msg.reply_markup).toBeUndefined();
  });

  it("turns an approval ask into an inline keyboard with verb:ref callback_data", () => {
    const events: InteractionEvent[] = [
      { kind: "ask", id: "a1", ask: "approval", prompt: "Approve the refund?", decisionRef: "dr-123" },
    ];
    const msg = projectTelegram(events, TELEGRAM_PROFILE)[0]!;
    expect(msg.text).toContain("Approve the refund?");
    const row = msg.reply_markup?.inline_keyboard?.[0];
    expect(row?.map((b) => b.callback_data)).toEqual(["approve:dr-123", "reject:dr-123"]);
  });

  it("turns a choice ask into one select button per option", () => {
    const events: InteractionEvent[] = [
      {
        kind: "ask",
        id: "a2",
        ask: "choice",
        prompt: "Pick a tier",
        decisionRef: "dr-9",
        options: [
          { id: "a", label: "Gold" },
          { id: "b", label: "Silver" },
        ],
      },
    ];
    const kb = projectTelegram(events, TELEGRAM_PROFILE)[0]!.reply_markup?.inline_keyboard;
    expect(kb).toHaveLength(2);
    expect(kb?.[0]?.[0]?.callback_data).toBe("select:dr-9:a");
    expect(kb?.[1]?.[0]?.callback_data).toBe("select:dr-9:b");
  });

  it("escapes HTML metacharacters", () => {
    const events: InteractionEvent[] = [
      { kind: "converse", id: "c1", role: "assistant", text: "5 < 10 & rising > expected" },
    ];
    expect(projectTelegram(events, TELEGRAM_PROFILE)[0]!.text).toBe(
      "5 &lt; 10 &amp; rising &gt; expected",
    );
  });

  it("splits a long reply into multiple full messages, never truncating", () => {
    const events: InteractionEvent[] = [
      { kind: "converse", id: "c2", role: "assistant", text: `${"x".repeat(5000)} ENDMARK` },
    ];
    const msgs = projectTelegram(events, TELEGRAM_PROFILE);
    expect(msgs.length).toBeGreaterThan(1); // split across messages, not one clamp
    for (const m of msgs) expect(m.text.length).toBeLessThanOrEqual(4096);
    expect(msgs.some((m) => m.text.includes("…"))).toBe(false); // nothing dropped
    expect(msgs[msgs.length - 1]!.text).toContain("ENDMARK"); // the tail is delivered
  });

  it("keeps every split message valid HTML (balanced tags)", () => {
    // One <b>…</b> spanning past 4096 — a split lands inside the tag.
    const events: InteractionEvent[] = [
      { kind: "converse", id: "c3", role: "assistant", text: `**${"word ".repeat(1200)}**` },
    ];
    const msgs = projectTelegram(events, TELEGRAM_PROFILE);
    expect(msgs.length).toBeGreaterThan(1);
    for (const m of msgs) {
      expect(m.text.length).toBeLessThanOrEqual(4096);
      // Every <b> matched by a </b> — else Telegram rejects the whole send.
      expect((m.text.match(/<b>/g) ?? []).length).toBe((m.text.match(/<\/b>/g) ?? []).length);
    }
  });

  it("attaches the reply keyboard to the last split message only", () => {
    const long = `${"x".repeat(5000)}`;
    const events: InteractionEvent[] = [
      { kind: "converse", id: "c4", role: "assistant", text: long },
      { kind: "ask", id: "a1", ask: "choice", prompt: "Next?", decisionRef: "d1",
        options: [{ id: "o1", label: "Drill in" }] },
    ];
    const msgs = projectTelegram(events, TELEGRAM_PROFILE);
    expect(msgs.length).toBeGreaterThan(1);
    expect(msgs.slice(0, -1).every((m) => m.reply_markup === undefined)).toBe(true);
    expect(msgs[msgs.length - 1]!.reply_markup).toBeDefined();
  });

  it("drops progress events (not delivered on an async push channel)", () => {
    const events: InteractionEvent[] = [
      { kind: "progress", id: "p1", label: "thinking", phase: "start" },
    ];
    expect(projectTelegram(events, TELEGRAM_PROFILE)).toEqual([]);
  });

  it("combines an inform and its sibling ask into one message with a keyboard", () => {
    const events: InteractionEvent[] = [
      { kind: "inform", id: "i", mood: "watch", title: "Stock low", body: "SKU-1 below reorder." },
      { kind: "ask", id: "a", ask: "approval", prompt: "Reorder now?", decisionRef: "dr-1" },
    ];
    const msgs = projectTelegram(events, TELEGRAM_PROFILE);
    expect(msgs).toHaveLength(1);
    expect(msgs[0]!.text).toContain("<b>Stock low</b>");
    expect(msgs[0]!.text).toContain("Reorder now?");
    expect(msgs[0]!.reply_markup?.inline_keyboard?.[0]?.[0]?.callback_data).toBe("approve:dr-1");
  });

  it("converts Markdown in a converse message to Telegram HTML", () => {
    const events: InteractionEvent[] = [
      { kind: "converse", id: "c", role: "assistant", text: "Run `npm i` then **deploy**." },
    ];
    const text = projectTelegram(events, TELEGRAM_PROFILE)[0]!.text;
    expect(text).toContain("<code>npm i</code>");
    expect(text).toContain("<b>deploy</b>");
  });

  it("renders the A2UI surface from inform.enrichment and makes Choice follow-ups a tappable reply keyboard", () => {
    const events: InteractionEvent[] = [
      {
        kind: "inform",
        id: "i",
        mood: "good",
        title: "flat title ignored when a surface is present",
        body: "flat body ignored",
        enrichment: {
          surfaces: [
            { version: "v0.9", createSurface: { surfaceId: "s1", catalogId: "x" } },
            {
              version: "v0.9",
              updateComponents: {
                surfaceId: "s1",
                components: [
                  { id: "root", component: "Answer", title: "Revenue", children: ["c1", "ch"] },
                  { id: "c1", component: "MetricCard", mood: "good", metric: "$4.7M", label: "MTD" },
                  {
                    id: "ch",
                    component: "Choice",
                    options: [{ label: "Break it down", prompt: "..." }, { label: "Show the trend" }],
                  },
                ],
              },
            },
          ],
        },
      },
    ];
    const msg = projectTelegram(events, TELEGRAM_PROFILE)[0]!;
    expect(msg.text).toContain("<b>Revenue</b>");
    expect(msg.text).toContain("<b>$4.7M</b> — MTD");
    // follow-ups are tappable buttons (sent verbatim as the next message), not a text list
    expect(msg.reply_markup?.keyboard).toEqual([[{ text: "Break it down" }], [{ text: "Show the trend" }]]);
    expect(msg.reply_markup?.one_time_keyboard).toBe(true);
    expect(msg.text).not.toContain("💡");
    expect(msg.text).not.toContain("flat title");
  });

  it("renders Choice follow-ups on a converse surface as a reply keyboard", () => {
    const events: InteractionEvent[] = [
      {
        kind: "converse",
        id: "c",
        role: "assistant",
        text: "",
        enrichment: {
          surfaces: [
            { version: "v0.9", createSurface: { surfaceId: "s1", catalogId: "x" } },
            {
              version: "v0.9",
              updateComponents: {
                surfaceId: "s1",
                components: [
                  { id: "root", component: "Answer", title: "Top territories", children: ["m", "ch"] },
                  { id: "m", component: "Markdown", text: "Southwest leads." },
                  { id: "ch", component: "Choice", options: [{ label: "Why is Central down?" }] },
                ],
              },
            },
          ],
        },
      },
    ];
    const msg = projectTelegram(events, TELEGRAM_PROFILE)[0]!;
    expect(msg.text).toContain("Southwest leads.");
    expect(msg.reply_markup?.keyboard).toEqual([[{ text: "Why is Central down?" }]]);
  });

  it("lets a pending approval (inline keyboard) win, listing follow-ups as text instead", () => {
    const events: InteractionEvent[] = [
      {
        kind: "inform",
        id: "i",
        mood: "good",
        title: "Revenue",
        body: "",
        enrichment: {
          surfaces: [
            { version: "v0.9", createSurface: { surfaceId: "s1", catalogId: "x" } },
            {
              version: "v0.9",
              updateComponents: {
                surfaceId: "s1",
                components: [
                  { id: "root", component: "Answer", title: "Revenue", children: ["m", "ch"] },
                  { id: "m", component: "Markdown", text: "Up 12%." },
                  { id: "ch", component: "Choice", options: [{ label: "Break it down" }] },
                ],
              },
            },
          ],
        },
      },
      { kind: "ask", id: "a", ask: "approval", prompt: "Post to exec channel?", decisionRef: "dr-1" },
    ];
    const msg = projectTelegram(events, TELEGRAM_PROFILE)[0]!;
    // inline keyboard claims reply_markup; the follow-up survives as a text list
    expect(msg.reply_markup?.inline_keyboard?.[0]?.[0]?.callback_data).toBe("approve:dr-1");
    expect(msg.reply_markup?.keyboard).toBeUndefined();
    expect(msg.text).toContain("💡 <b>Ask next</b>");
    expect(msg.text).toContain("• Break it down");
  });

  it("renders a surface carried on a converse reply (the chat-answer path)", () => {
    const events: InteractionEvent[] = [
      {
        kind: "converse",
        id: "c",
        role: "assistant",
        text: "",
        enrichment: {
          surfaces: [
            { version: "v0.9", createSurface: { surfaceId: "s1", catalogId: "x" } },
            {
              version: "v0.9",
              updateComponents: {
                surfaceId: "s1",
                components: [
                  { id: "root", component: "Answer", title: "Revenue by region", children: ["t"] },
                  {
                    id: "t",
                    component: "Table",
                    columns: [
                      { key: "r", label: "Region" },
                      { key: "v", label: "Rev", align: "right" },
                    ],
                    rows: [{ r: "SW", v: "$4.7M" }],
                  },
                ],
              },
            },
          ],
        },
      },
    ];
    const text = projectTelegram(events, TELEGRAM_PROFILE)[0]!.text;
    expect(text).toContain("<b>Revenue by region</b>");
    expect(text).toContain("<pre>");
    expect(text).toContain("SW");
  });
});
