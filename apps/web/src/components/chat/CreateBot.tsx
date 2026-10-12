"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { nekoCoats, nekoPatterns, type NekoPattern } from "@neko/avatars/react";
import { Button } from "@/components/ui/button";
import { FieldInput } from "@/components/ui/field";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { BotAvatar } from "./BotAvatar";

const PATTERN_LABELS: Record<NekoPattern, string> = {
  solid: "Solid",
  patch: "Patch",
  tabby: "Tabby",
};

export default function CreateBot() {
  const router = useRouter();
  const [name, setName] = useState("");
  const [coat, setCoat] = useState("sky");
  const [pattern, setPattern] = useState<NekoPattern>("patch");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function create() {
    setSaving(true);
    setError(null);
    try {
      const res = await fetch("/api/bots", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name, coat, pattern }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok || !data.bot?.threadId) throw new Error(data.error ?? "The bot was not created. Try again.");
      router.replace(`/chat/t/${data.bot.threadId}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "The bot was not created. Try again.");
      setSaving(false);
    }
  }

  return (
    <main className="chat-create">
      <h1 className="chat-home-title">New bot</h1>
      <div className="chat-create-preview">
        <BotAvatar coat={coat} pattern={pattern} size={160} />
      </div>
      <form
        className="chat-create-form"
        onSubmit={(event) => {
          event.preventDefault();
          if (name.trim() && !saving) void create();
        }}
      >
        <FieldInput
          label="Name"
          placeholder="Name your bot"
          value={name}
          maxLength={40}
          autoFocus
          onChange={(event) => setName(event.target.value)}
          error={error ?? undefined}
        />

        <div className="chat-create-group">
          <span className="chat-create-label" id="coat-label">Coat</span>
          <ToggleGroup
            type="single"
            value={coat}
            onValueChange={(value) => value && setCoat(value)}
            aria-labelledby="coat-label"
            className="chat-create-swatches"
          >
            {nekoCoats.map((c) => (
              <ToggleGroupItem key={c.name} value={c.name} aria-label={c.name} className="chat-create-swatch">
                <BotAvatar coat={c.name} pattern={pattern} size={40} animated={false} aria-hidden />
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        </div>

        <div className="chat-create-group">
          <span className="chat-create-label" id="pattern-label">Pattern</span>
          <ToggleGroup
            type="single"
            value={pattern}
            onValueChange={(value) => value && setPattern(value as NekoPattern)}
            aria-labelledby="pattern-label"
            className="chat-create-swatches"
          >
            {nekoPatterns.map((p) => (
              <ToggleGroupItem key={p} value={p} className="chat-create-swatch is-labelled">
                <BotAvatar coat={coat} pattern={p} size={40} animated={false} aria-hidden />
                <span>{PATTERN_LABELS[p]}</span>
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        </div>

        <Button type="submit" variant="primary" size="lg" disabled={!name.trim() || saving}>
          {saving ? "Creating…" : "Create"}
        </Button>
      </form>
    </main>
  );
}
