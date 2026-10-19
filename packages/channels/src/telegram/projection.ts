import type { CapabilityProfile, InteractionEvent } from "@neko/interaction";
import { escapeHtml, mdToTelegramHtml } from "./markdown";
import { renderSurface } from "./surface";

/**
 * Telegram projection: modality-free InteractionEvents → Telegram sendMessage
 * payloads. Pure; branches only on the CapabilityProfile (and the event kind),
 * never on channel identity. HTML parse mode (forgiving escaping vs MarkdownV2);
 * an `ask` becomes an inline keyboard whose callback_data carries the decision
 * ref in the shared `verb:rest` convention the inbound parser decodes.
 *
 * When an `inform` carries an A2UI surface (`enrichment.surfaces`), the full
 * rich answer (tables, callouts, metric cards, sections) is rendered via
 * `renderSurface`; otherwise we render the modality-free fields. Agent prose is
 * Markdown — `mdToTelegramHtml` converts the subset Telegram renders.
 */

export interface TelegramInlineButton {
  text: string;
  callback_data: string;
}

export interface TelegramReplyButton {
  text: string;
}

export interface TelegramMessage {
  text: string;
  parse_mode: "HTML";
  reply_markup?: {
    inline_keyboard?: TelegramInlineButton[][];
    keyboard?: TelegramReplyButton[][];
    one_time_keyboard?: boolean;
    resize_keyboard?: boolean;
    input_field_placeholder?: string;
  };
}

const MOOD_ICON: Record<string, string> = { good: "✅", watch: "👀", act: "🚨" };

const summarizeBody = (body: string, fidelity: CapabilityProfile["fidelity"]): string => {
  if (fidelity === "full") return body;
  const firstPara = body.split(/\n\n+/)[0] ?? body;
  if (fidelity === "summary") return firstPara;
  return firstPara.split(/(?<=[.!?])\s/)[0] ?? firstPara; // headline: first sentence
};

// Telegram parses parse_mode=HTML, so the length clamp must never slice through
// a tag: an orphaned `<b>` makes Telegram reject the ENTIRE message ("can't
// parse entities: can't find end tag"). Truncate with headroom, drop a
// half-written trailing tag, then close whatever tags the cut left open so the
// result is always well-formed.
// Tags still open at an arbitrary cut point (a <pre> table, a <blockquote>
// callout, a <b> run) — so a split can close them on one message and reopen
// them on the next.
const openTags = (html: string): string[] => {
  const open: string[] = [];
  const re = /<(\/?)([a-z0-9-]+)(?:\s[^>]*)?>/gi;
  for (let m = re.exec(html); m; m = re.exec(html)) {
    const name = (m[2] ?? "").toLowerCase();
    if (m[1]) {
      const at = open.lastIndexOf(name);
      if (at !== -1) open.splice(at, 1);
    } else {
      open.push(name);
    }
  }
  return open;
};

// Telegram caps ONE message at 4096 chars but accepts many, so a long answer
// (deep research, a big table) is split ACROSS messages, never truncated — the
// agent's full output is always delivered. Each message is valid HTML on its
// own: break at a paragraph/line/word boundary, never inside a tag, and if a
// chunk leaves tags open, close them here and reopen them on the next message.
const splitHtml = (html: string, max?: number): string[] => {
  if (max == null || html.length <= max) return [html];
  const out: string[] = [];
  let prefix = ""; // tags carried (reopened) from the previous chunk
  let rest = html;
  while (prefix.length + rest.length > max) {
    const room = Math.max(1, max - prefix.length - 24); // headroom for closers
    let at = Math.min(room, rest.length);
    const window = rest.slice(0, at);
    for (const sep of ["\n\n", "\n", " "]) {
      const idx = window.lastIndexOf(sep);
      if (idx > room * 0.4) {
        at = idx + sep.length;
        break;
      }
    }
    const head = rest.slice(0, at);
    if (head.lastIndexOf("<") > head.lastIndexOf(">")) {
      const safe = head.lastIndexOf("<"); // never cut inside a tag
      if (safe > 0) at = safe;
    }
    const chunk = prefix + rest.slice(0, at);
    const open = openTags(chunk);
    out.push((chunk + open.slice().reverse().map((t) => `</${t}>`).join("")).trimEnd());
    prefix = open.map((t) => `<${t}>`).join("");
    rest = rest.slice(at).replace(/^\n+/, "");
  }
  const tail = (prefix + rest).trimEnd();
  if (tail) out.push(tail);
  return out;
};

const askButtons = (
  event: Extract<InteractionEvent, { kind: "ask" }>,
): TelegramInlineButton[][] => {
  if (event.ask === "choice" && event.options?.length) {
    return event.options
      .slice(0, 5)
      .map((o) => [{ text: o.label.slice(0, 64), callback_data: `select:${event.decisionRef}:${o.id}` }]);
  }
  if (event.ask === "approval") {
    return [
      [
        { text: "✅ Approve", callback_data: `approve:${event.decisionRef}` },
        { text: "🚫 Reject", callback_data: `reject:${event.decisionRef}` },
      ],
    ];
  }
  return [];
};

export const projectTelegram = (
  events: InteractionEvent[],
  profile: CapabilityProfile,
): TelegramMessage[] => {
  const parts: string[] = [];
  const followups: string[] = [];
  let keyboard: TelegramInlineButton[][] | undefined;

  for (const event of events) {
    switch (event.kind) {
      case "converse": {
        const surfaces = event.enrichment?.surfaces;
        if (surfaces && surfaces.length) {
          const rendered = renderSurface(surfaces, profile);
          if (rendered.html) parts.push(rendered.html);
          followups.push(...rendered.followups);
        } else {
          parts.push(mdToTelegramHtml(event.text));
        }
        break;
      }
      case "inform": {
        const surfaces = event.enrichment?.surfaces;
        if (surfaces && surfaces.length) {
          const rendered = renderSurface(surfaces, profile);
          if (rendered.html) parts.push(rendered.html);
          followups.push(...rendered.followups);
          break;
        }
        const icon = MOOD_ICON[event.mood] ?? "";
        const head = `${icon ? `${icon} ` : ""}<b>${escapeHtml(event.title)}</b>`;
        const body = summarizeBody(event.body, profile.fidelity);
        const metric = event.metric
          ? `\n<b>${escapeHtml(event.metric.label)}:</b> ${escapeHtml(event.metric.value)}`
          : "";
        parts.push(`${head}${body ? `\n${mdToTelegramHtml(body)}` : ""}${metric}`);
        break;
      }
      case "ask":
        parts.push(mdToTelegramHtml(event.prompt));
        if (!keyboard && profile.interaction.quickReplies) {
          const buttons = askButtons(event);
          if (buttons.length) keyboard = buttons;
        }
        break;
      case "resolve": {
        const mark =
          event.status === "succeeded" ? "✅" : event.status === "rejected" ? "🚫" : "⚠️";
        parts.push(`${mark} ${escapeHtml(event.summary)}`);
        break;
      }
      case "highlight":
        parts.push(
          [
            "<b>Key figures</b>",
            ...event.metrics.map(
              (m) => `• <b>${escapeHtml(m.value)}</b> — ${escapeHtml(m.label)}${m.sub ? ` <i>(${escapeHtml(m.sub)})</i>` : ""}`,
            ),
          ].join("\n"),
        );
        break;
      case "offer":
        parts.push(
          /^https?:\/\//.test(event.artifactRef)
            ? `📎 <a href="${escapeHtml(event.artifactRef)}">${escapeHtml(event.label)}</a>`
            : `📎 ${escapeHtml(event.label)}`,
        );
        break;
      // `progress` (tool start/end) isn't delivered as a message on an async push channel
    }
  }

  // A2UI Choice options become tappable follow-ups via a reply keyboard — the
  // button text is sent verbatim as the next message (→ an `utterance` the agent
  // runs), sidestepping the 64-byte callback_data cap. Telegram allows one
  // reply_markup per message, so a pending approval (inline keyboard) wins; with
  // no quick replies or no body to attach to, fall back to a plain list.
  let replyKeyboard: TelegramReplyButton[][] | undefined;
  if (followups.length) {
    const labels = followups.slice(0, 6);
    if (!keyboard && profile.interaction.quickReplies && parts.some(Boolean)) {
      replyKeyboard = labels.map((f) => [{ text: f }]);
    } else {
      parts.push(`💡 <b>Ask next</b>\n${labels.map((f) => `• ${escapeHtml(f)}`).join("\n")}`);
    }
  }

  const full = parts.filter(Boolean).join("\n\n");
  if (!full) return [];
  const messages: TelegramMessage[] = splitHtml(
    full,
    profile.constraints.maxOutboundChars,
  ).map((text) => ({ text, parse_mode: "HTML" }));
  // reply_markup rides the LAST message — Telegram shows it under the final bubble.
  const last = messages[messages.length - 1]!;
  if (keyboard) last.reply_markup = { inline_keyboard: keyboard };
  else if (replyKeyboard)
    last.reply_markup = {
      keyboard: replyKeyboard,
      one_time_keyboard: true,
      resize_keyboard: true,
      input_field_placeholder: "Ask a follow-up…",
    };
  return messages;
};
