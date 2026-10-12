"use client";

import { useEffect, useState } from "react";
import { MessageCircle, Pencil, Plus } from "lucide-react";
import type { NekoPattern } from "@neko/avatars/react";
import type { BotRoutine, BotRoutineRun } from "@/lib/bots";
import type { BotSummary } from "@/lib/bots";
import { Badge } from "@/components/ui/badge";
import { Button, ButtonLink, IconButton } from "@/components/ui/button";
import { Disclosure } from "@/components/ui/disclosure";
import { EmptyState } from "@/components/ui/empty";
import { LocalDateTime } from "@/components/ui/local-date-time";
import { Field, FieldInput, Textarea } from "@/components/ui/field";
import { SkeletonList } from "@/components/ui/skeleton";
import { BotAvatar } from "./BotAvatar";
import { BotLookFields, type BotLook } from "./BotLookFields";
import { useBots } from "./useBots";

type Detail = { bot: BotSummary; routines: BotRoutine[] };

const ADD_ROUTINE_SEED = "Add a routine: ";

const RUN_STATUS: Record<string, string> = {
  completed: "Finished",
  failed: "Failed",
  cancelled: "Stopped",
  running: "Running",
  queued: "Waiting to run",
};

type Part = "name" | "instructions" | "look";

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
  const [saving, setSaving] = useState<Part | null>(null);
  const [notice, setNotice] = useState<{ part: Part; text: string; error?: boolean } | null>(null);
  const [renaming, setRenaming] = useState(false);
  const [name, setName] = useState("");
  const { reload: reloadBots } = useBots();
  const [routineBusy, setRoutineBusy] = useState<string | null>(null);
  const [runsBusy, setRunsBusy] = useState<string | null>(null);

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
  const lookChanged = look.coat !== saved.coat || look.pattern !== saved.pattern;
  const nameChanged = name.trim() !== "" && name.trim() !== bot.name;

  async function save(part: Part) {
    setSaving(part);
    setNotice(null);
    try {
      const body =
        part === "instructions"
          ? { instructions }
          : part === "name"
            ? { name: name.trim() }
            : { coat: look!.coat, pattern: look!.pattern };
      const updated = await patchBot(botId, body);
      setDetail((prev) => (prev ? { ...prev, bot: { ...updated, threadId: prev.bot.threadId, lastMessageAt: prev.bot.lastMessageAt } } : prev));
      if (part === "instructions") setInstructions(updated.instructions);
      else setLook(lookOf(updated));
      if (part === "name") setRenaming(false);
      if (part !== "instructions") reloadBots();
      setNotice({ part, text: "Saved." });
    } catch (err) {
      setNotice({ part, text: err instanceof Error ? err.message : "The change was not saved.", error: true });
    } finally {
      setSaving(null);
    }
  }

  async function loadMoreRuns(routine: BotRoutine) {
    const last = routine.runs[routine.runs.length - 1];
    setRunsBusy(routine.id);
    try {
      const res = await fetch(
        `/api/bots/${botId}/routines/${routine.id}${last ? `?before=${encodeURIComponent(last.id)}` : ""}`,
      );
      if (!res.ok) return;
      const page = (await res.json()) as { runs: BotRoutineRun[]; more: boolean };
      setDetail((prev) =>
        prev
          ? {
              ...prev,
              routines: prev.routines.map((r) =>
                r.id === routine.id ? { ...r, runs: [...r.runs, ...page.runs], moreRuns: page.more } : r,
              ),
            }
          : prev,
      );
    } finally {
      setRunsBusy(null);
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
        {renaming ? (
          <form
            className="bot-info-rename"
            onSubmit={(event) => {
              event.preventDefault();
              if (nameChanged && saving === null) void save("name");
            }}
          >
            <FieldInput
              label="Bot name"
              value={name}
              maxLength={40}
              autoFocus
              onChange={(event) => setName(event.target.value)}
              error={notice?.part === "name" && notice.error ? notice.text : undefined}
            />
            <div className="bot-info-actions">
              <Button type="submit" variant="primary" disabled={!nameChanged || saving !== null}>
                {saving === "name" ? "Saving…" : "Save name"}
              </Button>
              <Button type="button" variant="ghost" onClick={() => setRenaming(false)}>
                Cancel
              </Button>
            </div>
          </form>
        ) : (
          <div className="bot-info-name">
            <h1 className="chat-home-title">{bot.name}</h1>
            <IconButton
              label="Rename bot"
              variant="ghost"
              onClick={() => {
                setName(bot.name);
                setNotice(null);
                setRenaming(true);
              }}
            >
              <Pencil aria-hidden="true" strokeWidth={2} />
            </IconButton>
          </div>
        )}
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
          <ul className="bot-routines">
            {routines.map((routine) => (
              <li key={routine.id}>
                <Disclosure
                  title={
                    <span className="bot-routine-copy">
                      <span className="chat-row-title">{routine.name}</span>
                      <span className="chat-row-time">{routine.schedule}</span>
                    </span>
                  }
                  meta={<Badge variant={routine.enabled ? "live" : "muted"}>{routine.enabled ? "On" : "Paused"}</Badge>}
                >
                  <div className="bot-routine-body">
                    <div className="bot-routine-top">
                      {routine.description ? <p className="bot-info-copy">{routine.description}</p> : null}
                      <Button
                        variant="secondary"
                        size="sm"
                        disabled={routineBusy === routine.id}
                        onClick={() => void toggleRoutine(routine)}
                      >
                        {routine.enabled ? "Pause" : "Resume"}
                      </Button>
                    </div>
                    <div>
                      <h3 className="bot-routine-label">Recent runs</h3>
                      {routine.runs.length ? (
                        <ul className="bot-routine-runs">
                          {routine.runs.map((run) => (
                            <li key={run.id} className="bot-info-copy">
                              {RUN_STATUS[run.status] ?? "Ran"} · <LocalDateTime value={run.at} relativeDay />
                              {run.summary ? <span className="bot-routine-summary">{run.summary}</span> : null}
                            </li>
                          ))}
                        </ul>
                      ) : (
                        <p className="bot-info-copy">It has not run yet.</p>
                      )}
                      {routine.moreRuns ? (
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={runsBusy === routine.id}
                          onClick={() => void loadMoreRuns(routine)}
                        >
                          {runsBusy === routine.id ? "Loading…" : "Show more runs"}
                        </Button>
                      ) : null}
                    </div>
                    {routine.steps.length ? (
                      <div>
                        <h3 className="bot-routine-label">Steps</h3>
                        <ol className="bot-routine-steps">
                          {routine.steps.map((step, index) => (
                            <li key={index}>{step}</li>
                          ))}
                        </ol>
                      </div>
                    ) : null}
                  </div>
                </Disclosure>
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
            if (lookChanged && saving === null) void save("look");
          }}
        >
          <BotLookFields
            look={look}
            onChange={setLook}
            error={notice?.part === "look" && notice.error ? notice.text : null}
            showName={false}
          />
          <div className="bot-info-actions">
            <Button type="submit" variant="primary" disabled={!lookChanged || saving !== null}>
              {saving === "look" ? "Saving…" : "Save look"}
            </Button>
            {notice?.part === "look" && !notice.error ? <span className="bot-info-copy">{notice.text}</span> : null}
          </div>
        </form>
      </section>
    </main>
  );
}
