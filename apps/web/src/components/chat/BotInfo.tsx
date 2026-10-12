"use client";

import { useEffect, useState } from "react";
import { MessageCircle, Plus } from "lucide-react";
import type { NekoPattern } from "@neko/avatars/react";
import type { BotRoutine } from "@/lib/bots";
import type { BotSummary } from "@/lib/bots";
import { Badge } from "@/components/ui/badge";
import { Button, ButtonLink } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty";
import { Field, Textarea } from "@/components/ui/field";
import { SkeletonList } from "@/components/ui/skeleton";
import { BotAvatar } from "./BotAvatar";
import { BotLookFields, type BotLook } from "./BotLookFields";

type Detail = { bot: BotSummary; routines: BotRoutine[] };

const ADD_ROUTINE_SEED = "Add a routine: ";

async function patchBot(botId: string, body: Record<string, unknown>): Promise<BotSummary> {
  const res = await fetch(`/api/bots/${botId}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok || !data.bot) throw new Error(data.error ?? "The change was not saved. Try again.");
  return data.bot as BotSummary;
}

function lookOf(bot: BotSummary): BotLook {
  return { name: bot.name, coat: bot.coat, pattern: bot.pattern as NekoPattern };
}

export default function BotInfo({ botId }: { botId: string }) {
  const [detail, setDetail] = useState<Detail | null>(null);
  const [failed, setFailed] = useState(false);
  const [instructions, setInstructions] = useState("");
  const [look, setLook] = useState<BotLook | null>(null);
  const [saving, setSaving] = useState<"instructions" | "look" | null>(null);
  const [notice, setNotice] = useState<{ part: "instructions" | "look"; text: string; error?: boolean } | null>(null);
  const [routineBusy, setRoutineBusy] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    fetch(`/api/bots/${botId}`)
      .then((res) => (res.ok ? res.json() : Promise.reject(res)))
      .then((data: Detail) => {
        if (cancelled) return;
        setDetail(data);
        setInstructions(data.bot.instructions);
        setLook(lookOf(data.bot));
      })
      .catch(() => {
        if (!cancelled) setFailed(true);
      });
    return () => {
      cancelled = true;
    };
  }, [botId]);

  if (failed) {
    return (
      <main className="bot-info">
        <EmptyState title="This bot did not load" body="Go back to your chats and try again." />
      </main>
    );
  }
  if (!detail || !look) {
    return (
      <main className="bot-info">
        <SkeletonList rows={3} variant="row" label="Loading the bot" />
      </main>
    );
  }

  const { bot, routines } = detail;
  const chatHref = bot.threadId ? `/chat/t/${bot.threadId}` : "/chat";
  const instructionsChanged = instructions.trim() !== bot.instructions.trim();
  const saved = lookOf(bot);
  const lookChanged =
    look.name.trim() !== saved.name || look.coat !== saved.coat || look.pattern !== saved.pattern;

  async function save(part: "instructions" | "look") {
    setSaving(part);
    setNotice(null);
    try {
      const updated = await patchBot(botId, part === "instructions" ? { instructions } : look!);
      setDetail((prev) => (prev ? { ...prev, bot: { ...updated, threadId: prev.bot.threadId, lastMessageAt: prev.bot.lastMessageAt } } : prev));
      if (part === "instructions") setInstructions(updated.instructions);
      else setLook(lookOf(updated));
      setNotice({ part, text: "Saved." });
    } catch (err) {
      setNotice({ part, text: err instanceof Error ? err.message : "The change was not saved.", error: true });
    } finally {
      setSaving(null);
    }
  }

  async function toggleRoutine(routine: BotRoutine) {
    setRoutineBusy(routine.id);
    try {
      const res = await fetch(`/api/bots/${botId}/routines/${routine.id}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ enabled: !routine.enabled }),
      });
      if (res.ok) {
        setDetail((prev) =>
          prev
            ? { ...prev, routines: prev.routines.map((r) => (r.id === routine.id ? { ...r, enabled: !r.enabled } : r)) }
            : prev,
        );
      }
    } finally {
      setRoutineBusy(null);
    }
  }

  return (
    <main className="bot-info">
      <header className="bot-info-head">
        <BotAvatar coat={look.coat} pattern={look.pattern} size={120} />
        <h1 className="chat-home-title">{bot.name}</h1>
        <ButtonLink href={chatHref} variant="secondary">
          <MessageCircle aria-hidden="true" strokeWidth={2} />
          Open chat
        </ButtonLink>
      </header>

      <section className="bot-info-section" aria-labelledby="bot-instructions-title">
        <h2 id="bot-instructions-title" className="bot-info-title">Instructions</h2>
        <Field
          label="What this bot does for you"
          htmlFor="bot-instructions"
          hint={
            notice?.part === "instructions" && !notice.error
              ? notice.text
              : "The bot keeps this up to date as you talk. You can edit it at any time."
          }
          error={notice?.part === "instructions" && notice.error ? notice.text : undefined}
        >
          <Textarea
            id="bot-instructions"
            rows={5}
            maxLength={2000}
            value={instructions}
            placeholder="Nothing yet. Tell the bot in chat what you want, or write it here."
            onChange={(event) => setInstructions(event.target.value)}
          />
        </Field>
        <div className="bot-info-actions">
          <Button
            variant="primary"
            disabled={!instructionsChanged || saving !== null}
            onClick={() => void save("instructions")}
          >
            {saving === "instructions" ? "Saving…" : "Save instructions"}
          </Button>
        </div>
      </section>

      <section className="bot-info-section" aria-labelledby="bot-routines-title">
        <div className="bot-info-row-head">
          <h2 id="bot-routines-title" className="bot-info-title">Routines</h2>
          <ButtonLink href={`${chatHref}?seed=${encodeURIComponent(ADD_ROUTINE_SEED)}`} variant="secondary">
            <Plus aria-hidden="true" strokeWidth={2} />
            Add routine
          </ButtonLink>
        </div>
        {routines.length === 0 ? (
          <p className="bot-info-copy">
            No routines yet. Ask the bot to do something on a schedule or when a number changes.
          </p>
        ) : (
          <ul className="chat-list">
            {routines.map((routine) => (
              <li key={routine.id} className="bot-routine">
                <span className="bot-routine-copy">
                  <span className="chat-row-title">{routine.name}</span>
                  <span className="chat-row-time">{routine.schedule}</span>
                </span>
                <Badge variant={routine.enabled ? "live" : "muted"}>{routine.enabled ? "On" : "Paused"}</Badge>
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={routineBusy === routine.id}
                  onClick={() => void toggleRoutine(routine)}
                >
                  {routine.enabled ? "Pause" : "Resume"}
                </Button>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="bot-info-section" aria-labelledby="bot-look-title">
        <h2 id="bot-look-title" className="bot-info-title">Look</h2>
        <form
          className="chat-create-form"
          onSubmit={(event) => {
            event.preventDefault();
            if (lookChanged && look.name.trim() && saving === null) void save("look");
          }}
        >
          <BotLookFields
            look={look}
            onChange={setLook}
            error={notice?.part === "look" && notice.error ? notice.text : null}
          />
          <div className="bot-info-actions">
            <Button type="submit" variant="primary" disabled={!lookChanged || !look.name.trim() || saving !== null}>
              {saving === "look" ? "Saving…" : "Save look"}
            </Button>
            {notice?.part === "look" && !notice.error ? <span className="bot-info-copy">{notice.text}</span> : null}
          </div>
        </form>
      </section>
    </main>
  );
}
