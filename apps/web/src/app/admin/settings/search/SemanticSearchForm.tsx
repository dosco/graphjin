"use client";

import { useState } from "react";
import { toast } from "sonner";
import AppHeader from "@/components/AppHeader";
import PageHeading from "@/components/PageHeading";
import SectionNav from "@/components/SectionNav";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";

type Setting = {
  enabled: boolean;
  waiting?: number;
  graphjin?: { status: "applied" } | { status: "skipped" | "failed"; reason: string };
};

export default function SemanticSearchForm({ initial }: { initial: Setting }) {
  const [enabled, setEnabled] = useState(initial.enabled);
  const [saved, setSaved] = useState(initial.enabled);
  const [waiting, setWaiting] = useState(initial.waiting ?? 0);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function save() {
    setSaving(true);
    setError(null);
    try {
      const response = await fetch("/api/admin/semantic-search", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ enabled }),
      });
      const body = (await response.json().catch(() => null)) as (Setting & { error?: string }) | null;
      if (!response.ok || !body) throw new Error(body?.error ?? "Could not save semantic search.");
      setSaved(body.enabled);
      toast.success(body.enabled ? "Semantic search is on." : "Semantic search is off.");
      if (body.graphjin?.status === "failed") {
        toast.warning("Data lookups and Library files will follow when the data service next updates.", {
          description: body.graphjin.reason,
        });
      }
      const refreshed = await fetch("/api/admin/semantic-search").then((r) => (r.ok ? r.json() : null)).catch(() => null);
      if (refreshed && typeof refreshed.waiting === "number") setWaiting(refreshed.waiting);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not save semantic search.");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="root is-narrow">
      <AppHeader back={{ href: "/admin/settings", label: "Settings" }}>
        <SectionNav current="admin" />
      </AppHeader>
      <PageHeading
        title="Semantic search"
        description="Find memories, skills, Library files and data by meaning, not only by the words they contain."
      />

      <section className="settings-card flex flex-col gap-6">
        <div className="grid gap-1.5">
          <Checkbox
            label="Use semantic search"
            checked={enabled}
            onCheckedChange={(checked) => setEnabled(checked === true)}
          />
          <div className="pl-6 grid gap-2 text-ui-caption leading-[var(--leading-compact)] text-text3">
            <p>
              When on, memories, skills, Library files and data lookups use meaning-based search, so a question
              finds what it means even when the words differ.
            </p>
            <p>
              It runs a local embedding model that loads on first use and needs about 2 GB of memory. Existing
              content is indexed in the background after you turn it on.
            </p>
            <p>When off, OpenNeko searches by keyword and never loads the model.</p>
          </div>
        </div>

        {saved && waiting > 0 ? (
          <p className="text-text2">
            Indexing in the background: {waiting.toLocaleString()} {waiting === 1 ? "item" : "items"} left. Until then they
            are found by keyword.
          </p>
        ) : null}
        {error ? <p role="alert" className="text-danger">{error}</p> : null}

        <div className="flex justify-end">
          <Button type="button" variant="primary" onClick={save} disabled={saving || enabled === saved}>
            {saving ? "Saving…" : "Save settings"}
          </Button>
        </div>
      </section>
    </div>
  );
}
