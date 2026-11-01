"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Field, Input } from "@/components/ui/field";

type Limits = { rollingTokenBudget: number; rollingCostMicrosBudget: number };

export default function WorkflowOrgLimitsForm({ initial }: { initial: Limits }) {
  const [tokens, setTokens] = useState(String(initial.rollingTokenBudget));
  const [dollars, setDollars] = useState(String(initial.rollingCostMicrosBudget / 1_000_000));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function save(event: React.FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError(null);
    try {
      const response = await fetch("/api/admin/workflow-api-limits", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          rollingTokenBudget: Number(tokens),
          rollingCostMicrosBudget: Math.round(Number(dollars) * 1_000_000),
        }),
      });
      const body = await response.json();
      if (!response.ok) throw new Error(body.error ?? "Could not save workflow API limits.");
      setTokens(String(body.limits.rollingTokenBudget));
      setDollars(String(body.limits.rollingCostMicrosBudget / 1_000_000));
      toast.success("Workflow API limits saved.");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not save workflow API limits.");
    } finally {
      setSaving(false);
    }
  }

  return (
    <form onSubmit={save} className="settings-card grid gap-5">
      <div>
        <h2 className="settings-card-title">Organization rolling budget</h2>
        <p className="settings-card-copy">Reservations for API runs in the current budget window. Per-workflow limits are set below.</p>
      </div>
      <div className="grid grid-cols-2 gap-4 max-[720px]:grid-cols-1">
        <Field label="Token budget" htmlFor="workflow-org-tokens" hint="1,000 to 100,000,000 tokens.">
          <Input id="workflow-org-tokens" type="number" min={1000} max={100000000} step={1} required value={tokens} onChange={(event) => setTokens(event.target.value)} />
        </Field>
        <Field label="Provider spend budget (USD)" htmlFor="workflow-org-spend" hint="$0.001 to $10,000. Model spending limits still apply.">
          <Input id="workflow-org-spend" type="number" min={0.001} max={10000} step={0.001} required value={dollars} onChange={(event) => setDollars(event.target.value)} />
        </Field>
      </div>
      {error ? <p role="alert" className="text-danger">{error}</p> : null}
      <Button type="submit" variant="primary" disabled={saving} className="justify-self-end">{saving ? "Saving…" : "Save organization budget"}</Button>
    </form>
  );
}
