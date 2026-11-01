"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { BotAvatar } from "./BotAvatar";
import { BotLookFields, type BotLook } from "./BotLookFields";

export default function CreateBot() {
  const router = useRouter();
  const [look, setLook] = useState<BotLook>({ name: "", coat: "sky", pattern: "patch" });
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function create() {
    setSaving(true);
    setError(null);
    try {
      const res = await fetch("/api/bots", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(look),
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
        <BotAvatar coat={look.coat} pattern={look.pattern} size={160} />
      </div>
      <form
        className="chat-create-form"
        onSubmit={(event) => {
          event.preventDefault();
          if (look.name.trim() && !saving) void create();
        }}
      >
        <BotLookFields look={look} onChange={setLook} error={error} autoFocus />
        <Button type="submit" variant="primary" size="lg" disabled={!look.name.trim() || saving}>
          {saving ? "Creating…" : "Create"}
        </Button>
      </form>
    </main>
  );
}
