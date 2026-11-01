"use client";

import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Field, Input } from "@/components/ui/field";
import { Textarea } from "@/components/ui/textarea";

type Profile = {
  displayName: string | null;
  roleTemplate: string;
  focusAreas: string[];
};

/**
 * A person's own account: the persona their agent runs read, plus sign out.
 * Members edit only their own row; the persona API enforces that.
 */
export function ProfileClient({ email, signInEnabled }: { email: string; signInEnabled: boolean }) {
  const [displayName, setDisplayName] = useState("");
  const [roleTemplate, setRoleTemplate] = useState("");
  const [focusAreas, setFocusAreas] = useState("");
  const [state, setState] = useState<"loading" | "ready" | "saving">("loading");
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const res = await fetch("/api/settings/persona", { cache: "no-store" });
        if (!res.ok) throw new Error(`could not load your profile (${res.status})`);
        const body = (await res.json()) as { profile: Profile | null };
        if (cancelled) return;
        // A person who has never saved a persona has no row yet.
        setDisplayName(body.profile?.displayName ?? "");
        setRoleTemplate(body.profile?.roleTemplate ?? "");
        setFocusAreas((body.profile?.focusAreas ?? []).join("\n"));
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      } finally {
        if (!cancelled) setState("ready");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  async function save() {
    setState("saving");
    setError(null);
    setSaved(false);
    try {
      const res = await fetch("/api/settings/persona", {
        method: "PUT",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          displayName: displayName.trim() || null,
          roleTemplate: roleTemplate.trim(),
          focusAreas: focusAreas.split("\n").map((area) => area.trim()).filter(Boolean),
        }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as { error?: string } | null;
        throw new Error(body?.error ?? `save failed (${res.status})`);
      }
      setSaved(true);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setState("ready");
    }
  }

  async function signOut() {
    try {
      await fetch("/api/auth/logout", { method: "POST" });
    } finally {
      window.location.href = "/signin";
    }
  }

  return (
    <section className="settings-card max-w-(--form-max)">
      <div className="settings-card-head">
        <div>
          <h2 className="settings-card-title">Your profile</h2>
          <p className="settings-card-copy">
            Your agent runs read this. It belongs to your account, so it changes nobody else&apos;s workspace.
          </p>
        </div>
        {email ? (
          <div className="settings-source">
            <strong className="is-ok">{email}</strong>
          </div>
        ) : null}
      </div>

      {error ? (
        <div className="mb-3 rounded-control bg-danger-soft px-3 py-2 text-sm text-danger" role="alert">
          {error}
        </div>
      ) : null}

      <div className="flex flex-col gap-4">
        <Field label="Display name" htmlFor="profile-display-name">
          <Input
            id="profile-display-name"
            value={displayName}
            maxLength={120}
            disabled={state === "loading"}
            onChange={(event) => setDisplayName(event.target.value)}
            placeholder="How your name appears in runs"
          />
        </Field>
        <Field label="Your role" htmlFor="profile-role">
          <Input
            id="profile-role"
            value={roleTemplate}
            maxLength={120}
            disabled={state === "loading"}
            onChange={(event) => setRoleTemplate(event.target.value)}
            placeholder="Head of EU wholesale operations"
          />
        </Field>
        <Field label="What needs your attention?" htmlFor="profile-focus" hint="One priority per line.">
          <Textarea
            id="profile-focus"
            rows={5}
            value={focusAreas}
            disabled={state === "loading"}
            onChange={(event) => setFocusAreas(event.target.value)}
            placeholder={"Stock-outs on top SKUs\nReorder lead times\nWholesale margin"}
          />
        </Field>
        <div className="flex items-center justify-end gap-3">
          {saved ? <span className="text-sm text-text2">Saved.</span> : null}
          <Button variant="primary" disabled={state !== "ready"} onClick={() => void save()}>
            {state === "saving" ? "Saving…" : "Save profile"}
          </Button>
        </div>
      </div>

      {signInEnabled ? (
        <div className="mt-6 border-t border-border pt-4">
          <Button variant="danger" onClick={() => void signOut()}>
            Sign out
          </Button>
        </div>
      ) : null}
    </section>
  );
}
