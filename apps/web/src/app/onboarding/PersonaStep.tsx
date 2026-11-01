"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import EntryShell from "@/components/EntryShell";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";

/**
 * Per-user onboarding (CV3): every SSO identity describes its own role and
 * priorities. The agent reads that exact operator_profile on their runs.
 */
export default function PersonaStep({
  initialRoleTemplate,
  initialFocusAreas,
}: {
  initialRoleTemplate: string;
  initialFocusAreas: string[];
}) {
  const router = useRouter();
  const [roleTemplate, setRoleTemplate] = useState(initialRoleTemplate);
  const [focusAreas, setFocusAreas] = useState(initialFocusAreas.join("\n"));
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async () => {
    setSubmitting(true);
    setError(null);
    try {
      const res = await fetch("/api/settings/persona", {
        method: "PUT",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          roleTemplate: roleTemplate.trim(),
          focusAreas: focusAreas
            .split("\n")
            .map((area) => area.trim())
            .filter(Boolean),
        }),
      });
      if (!res.ok) throw new Error(`save failed (${res.status})`);
      router.push("/");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setSubmitting(false);
    }
  };

  return (
    <EntryShell
      title="Define your role."
      description="This context belongs to your account. It shapes your agent runs without changing another person’s workspace."
    >
      <div className="entry-section-head">
        <p className="entry-section-kicker">Your persona</p>
        <h2>Define your working context</h2>
        <p>
          Use your real role—not a generic executive seat. You can revise this
          from the account menu.
        </p>
      </div>
      <div className="entry-fields">
        <label className="entry-field">
          <span>Your role</span>
          <Input
            className="entry-control"
            value={roleTemplate}
            maxLength={120}
            onChange={(event) => setRoleTemplate(event.target.value)}
            placeholder="Head of EU wholesale operations"
            autoComplete="organization-title"
          />
        </label>
        <label className="entry-field">
          <span>
            What needs your attention?{" "}
            <span className="entry-optional">Optional</span>
          </span>
          <Textarea
            className="entry-control"
            rows={5}
            value={focusAreas}
            onChange={(event) => setFocusAreas(event.target.value)}
            placeholder={
              "Stock-outs on top SKUs\nReorder lead times\nWholesale margin"
            }
          />
          <span className="entry-field-help">Add one priority per line.</span>
        </label>
        {error ? (
          <div className="entry-error" role="alert">
            {error}
          </div>
        ) : null}
      </div>
      <div className="entry-actions is-end">
        <Button
          variant="primary"
          size="lg"
          onClick={() => void submit()}
          disabled={submitting || !roleTemplate.trim()}
        >
          {submitting ? "Saving…" : "Open my workspace"}
        </Button>
      </div>
    </EntryShell>
  );
}
