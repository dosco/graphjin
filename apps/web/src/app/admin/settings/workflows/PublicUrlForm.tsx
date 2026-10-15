"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Field, Input } from "@/components/ui/field";

type Setting = { saved: string | null; environment: string | null };

export default function PublicUrlForm({ initial }: { initial: Setting }) {
  const [url, setUrl] = useState(initial.saved ?? "");
  const [environment, setEnvironment] = useState(initial.environment);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function save(event: React.FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError(null);
    try {
      const response = await fetch("/api/admin/public-url", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ url }),
      });
      const body = await response.json();
      if (!response.ok) throw new Error(body.error ?? "Could not save the public URL.");
      setUrl(body.saved ?? "");
      setEnvironment(body.environment);
      toast.success(body.saved ? "Public URL saved." : "Public URL cleared.");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not save the public URL.");
    } finally {
      setSaving(false);
    }
  }

  const fallback = environment ?? "the address in the browser";

  return (
    <form onSubmit={save} className="settings-card mb-6 grid gap-5">
      <div>
        <h2 className="settings-card-title">Public URL</h2>
        <p className="settings-card-copy">The address that API callers use to reach OpenNeko, such as a proxy address. Workflow API examples start with it.</p>
      </div>
      <Field label="Public URL" htmlFor="workflow-public-url" hint={`Leave empty to use ${fallback}.`}>
        <Input
          id="workflow-public-url"
          type="url"
          inputMode="url"
          placeholder={environment ?? "https://openneko.example.com"}
          value={url}
          onChange={(event) => setUrl(event.target.value)}
        />
      </Field>
      {error ? <p role="alert" className="text-danger">{error}</p> : null}
      <Button type="submit" variant="primary" disabled={saving} className="justify-self-end">{saving ? "Saving…" : "Save public URL"}</Button>
    </form>
  );
}
