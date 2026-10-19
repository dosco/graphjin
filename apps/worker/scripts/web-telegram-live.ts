/**
 * Proof: web + Telegram are used *simultaneously* by the same agent loop, with
 * no code above the waist knowing either exists.
 *
 * ONE modality-free InteractionEvent[] (an `inform` + an approval `ask`) is
 * fanned out to two membranes at once:
 *   - WEB     — the built-in, in-process projection the live /api/briefing uses.
 *   - TELEGRAM — the built-in Telegram channel the worker delivers through,
 *                projecting with the Telegram profile.
 * Then a real Telegram button tap is normalized back to the same `decision`
 * intent the web Approve button produces — terminating at the same agent entry
 * point. This is the worker's delivery fan-out, minus the (Phase-2) binding row.
 *
 * Run:  pnpm --filter @neko/worker demo:telegram
 * Live: TELEGRAM_BOT_TOKEN=… TELEGRAM_CHAT_ID=… pnpm --filter @neko/worker demo:telegram
 */
import { TELEGRAM_CHANNEL, webProjection } from "@neko/channels";
import {
  WEB_PROFILE,
  type InteractionEvent,
} from "@neko/interaction";

const CHAT_RAW = process.env.TELEGRAM_CHAT_ID ?? "123456789";
const chatId = /^-?\d+$/.test(CHAT_RAW) ? Number(CHAT_RAW) : CHAT_RAW;

/** The waist. In production these come from toInteractionEvents(agentStream)
 *  (see multi-channel-demo.ts) or outputRowToInteractionEvent(row) (the
 *  /api/briefing path). Constructed here so the contrast is deterministic. */
const events: InteractionEvent[] = [
  {
    kind: "inform",
    id: "active-customers-latest-year",
    mood: "good",
    title: "Active customers up 366%",
    body:
      "18,069 distinct customers placed an order in the most recent year, up from 3,873 the prior year.\n\nGrowth was concentrated in the enterprise segment — full breakdown in the report.",
    metric: { label: "Active customers", value: "18,069" },
    series: { kind: "line", points: [ { d: "FY22", v: 3873 }, { d: "FY23", v: 18069 } ] },
  },
  {
    kind: "ask",
    id: "ar-501",
    ask: "approval",
    prompt: "Approve posting the customer-growth summary to the exec channel?",
    decisionRef: "ar-501",
    risk: "medium",
  },
];

const rule = (label: string): string =>
  `\n${"─".repeat(4)} ${label} ${"─".repeat(Math.max(0, 66 - label.length))}`;

const main = async (): Promise<void> => {
  console.log(rule("THE WAIST — one modality-free InteractionEvent[]"));
  console.log(JSON.stringify(events, null, 2));

  // ── WEB: in-process, the exact projection /api/briefing renders ──
  const web = webProjection(events, WEB_PROFILE);
  const cards = (web.surfaces[1] as { updateComponents?: { components?: unknown[] } } | undefined)
    ?.updateComponents?.components;
  console.log(rule("WEB  (built-in, in-process)  profile: visual+cards+charts"));
  console.log("A2UI components:");
  console.log(JSON.stringify(cards, null, 2));
  console.log(`approval affordances: ${JSON.stringify(web.pendingAsks)}`);

  // ── TELEGRAM: the built-in channel, exactly as the worker delivers ──
  console.log(rule("TELEGRAM  (built-in channel)  profile: text+markdown+buttons"));
  console.log(`providerLabel="${TELEGRAM_CHANNEL.providerLabel}"  directions=${TELEGRAM_CHANNEL.directions.join(",")}`);
  const live = Boolean(process.env.TELEGRAM_BOT_TOKEN);
  // Without a token the channel runs dry and logs the projected payload.
  const delivered = await TELEGRAM_CHANNEL.deliver({
    recipient: { kind: "telegram", chatId },
    events,
    env: { TELEGRAM_BOT_TOKEN: process.env.TELEGRAM_BOT_TOKEN },
  });
  console.log(`deliver → ${JSON.stringify(delivered)}  ${live ? "(LIVE send)" : "(dry-run — set TELEGRAM_BOT_TOKEN to send)"}`);

  // ── INBOUND: a real Telegram button tap → the same intent the web button makes ──
  const tap = { update_id: 1, callback_query: { id: "c1", data: "approve:ar-501", message: { chat: { id: chatId } } } };
  const { intents } = TELEGRAM_CHANNEL.parseInbound(tap, {});
  console.log(rule("INBOUND — Telegram tap → IntentEvent → same agent entry point"));
  for (const intent of intents) {
    const route =
      intent.kind === "decision"
        ? `→ ${intent.choice}ActionRequest(orgId, "${intent.decisionRef}")  ← identical to the web Approve button`
        : intent.kind === "utterance"
          ? `→ runChatTurn({ message: "${intent.text}" })`
          : `→ (${intent.kind})`;
    console.log(`${JSON.stringify(intent)}\n  ${route}`);
  }

  console.log(rule("RESULT"));
  console.log(
    "One InteractionEvent[] → WEB (A2UI BriefingCard + inline approval) AND TELEGRAM\n" +
      "(HTML message + Approve/Reject buttons), projected per profile, simultaneously.\n" +
      "Neither the agent nor the waist named a channel. Adding Telegram was one projection\n" +
      "+ one adapter; the web path was untouched.\n",
  );
};

await main();
