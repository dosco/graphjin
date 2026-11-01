"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import AppHeader from "@/components/AppHeader";
import CreatorCredit from "@/components/CreatorCredit";
import PageHeading from "@/components/PageHeading";
import SectionNav from "@/components/SectionNav";
import { Button } from "@/components/ui/button";
import { NativeSelect } from "@/components/ui/field";
import { adminApi } from "@/components/admin/admin-api";
import { TargetListEditor } from "@/components/admin/TargetListEditor";
import { targetPatterns } from "../RulesClient";

type PolicyDetail = {
  policy: {
    id: string;
    name: string;
    description: string;
    appliesToKinds: string[];
    appliesToScopes: string[];
    mode: string;
    riskThresholdAutoApprove: string | null;
    allowedTargets: Record<string, unknown> | null;
    deniedTargets: Record<string, unknown> | null;
    limits: Record<string, unknown>;
    approverRole: string | null;
    approverGroupId: string | null;
    priority: number;
    enabled: boolean;
    createdByThreadId?: string | null;
    createdAt: string;
    updatedAt: string;
  };
};

function ApproverGroupPicker({
  policyId,
  value,
  onSaved,
}: {
  policyId: string;
  value: string | null;
  onSaved: (approverGroupId: string | null) => void;
}) {
  const [groups, setGroups] = useState<Array<{ id: string; name: string; slug: string }>>([]);
  const [selected, setSelected] = useState(value ?? "");
  const [status, setStatus] = useState<string | null>(null);

  useEffect(() => {
    void adminApi<{ groups: Array<{ id: string; name: string; slug: string }> }>("/api/admin/groups").then((result) => {
      if (result.ok) setGroups(result.body.groups.filter((g) => g.slug !== "everyone"));
    });
  }, []);

  return (
    <div className="flex flex-wrap items-center gap-2">
      <NativeSelect aria-label="Approver group" value={selected} onChange={(e) => setSelected(e.target.value)}>
        <option value="">Any signed-in user</option>
        {groups.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
      </NativeSelect>
      <Button
        size="sm"
        disabled={selected === (value ?? "")}
        onClick={async () => {
          const result = await adminApi(`/api/policies/${policyId}`, "PATCH", { approverGroupId: selected || null });
          setStatus(result.ok ? "Saved" : result.error);
          if (result.ok) onSaved(selected || null);
        }}
      >
        Save
      </Button>
      {status ? <span className="text-ui-body-sm text-text2" role="status">{status}</span> : null}
    </div>
  );
}

export default function PolicyDetailClient({ policyId }: { policyId: string }) {
  const router = useRouter();
  const [policy, setPolicy] = useState<PolicyDetail["policy"] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!policyId) return;
    let cancelled = false;
    void fetch(`/api/policies/${policyId}`, { cache: "no-store" })
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json() as Promise<PolicyDetail>;
      })
      .then((data) => {
        if (cancelled) return;
        setPolicy(data.policy);
      })
      .catch((err) => {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : "Failed to load rule");
      });
    return () => {
      cancelled = true;
    };
  }, [policyId]);

  const editInWork = () => {
    if (!policy) return;
    router.push(
      `/work?seed=${encodeURIComponent(`Update the '${policy.name}' rule to `)}`,
    );
  };

  return (
    <>
      <div className="root is-narrow">
        <AppHeader back={{ href: "/admin/rules", label: "Rules" }}>
          <SectionNav current="admin" />
        </AppHeader>

        <PageHeading
          title={policy?.name ?? "Loading rule"}
          description={policy?.description}
          meta={
            policy ? (policy.enabled ? "enabled" : "disabled") : undefined
          }
        />

        {error ? (
          <div className="text-danger text-sm">Couldn&apos;t load rule: {error}</div>
        ) : !policy ? (
          <div className="text-text3 text-sm">Loading rule…</div>
        ) : (
          <article className="bg-card border border-border rounded-2xl p-6">
            <Field label="Mode">
              <code className="font-mono text-ui-body-sm text-text2">{policy.mode}</code>
            </Field>

            <Field label="Applies to">
              <div className="text-ui-body-sm text-text">
                {policy.appliesToKinds.length === 0 ? (
                  <span className="italic text-text3">any action kind</span>
                ) : (
                  policy.appliesToKinds.map((k) => (
                    <code
                      key={k}
                      className="font-mono text-ui-caption bg-neutral-soft text-text2 px-1.5 py-0.5 rounded mr-1.5"
                    >
                      {k}
                    </code>
                  ))
                )}
                <span className="text-text3 ml-1">
                  · scope: {policy.appliesToScopes.join(", ") || "—"}
                </span>
              </div>
            </Field>

            <div className="mb-4">
              <TargetListEditor
                label="Allowed targets"
                hint="One target per line. The rule applies only when every target of an action is on this list. Use *.acme.com for subdomains, or end a line with * to match a prefix. Leave it empty to apply the rule to every target."
                placeholder={"acme.com\n*.acme.com"}
                patterns={targetPatterns(policy.allowedTargets)}
                onMiss={policy.allowedTargets?.on_miss === "next" ? "next" : "deny"}
                onSave={async (text, onMiss) => {
                  const result = await adminApi<{ policy: { allowedTargets: Record<string, unknown> | null } }>(
                    `/api/policies/${policy.id}`,
                    "PATCH",
                    { allowedTargets: { patterns: text, onMiss } },
                  );
                  if (!result.ok) return { ok: false, error: result.error };
                  setPolicy((current) => (current ? { ...current, allowedTargets: result.body.policy.allowedTargets } : current));
                  return { ok: true };
                }}
              />
            </div>

            {targetPatterns(policy.deniedTargets).length > 0 ? (
              <Field label="Denied targets">
                <div className="text-ui-body-sm text-text">
                  {targetPatterns(policy.deniedTargets).map((p) => (
                    <code
                      key={p}
                      className="font-mono text-ui-caption bg-neutral-soft text-text2 px-1.5 py-0.5 rounded mr-1.5"
                    >
                      {p}
                    </code>
                  ))}
                </div>
              </Field>
            ) : null}

            {policy.riskThresholdAutoApprove && (
              <Field label="Auto-approve">
                <span className="text-ui-body-sm">
                  risk ≤{" "}
                  <code className="font-mono text-ui-caption text-text2">
                    {policy.riskThresholdAutoApprove}
                  </code>
                </span>
              </Field>
            )}

            {Object.keys(policy.limits).length > 0 && (
              <Field label="Limits">
                <code className="font-mono text-ui-caption text-text2">
                  {Object.entries(policy.limits)
                    .map(([k, v]) => `${k}: ${String(v)}`)
                    .join(" · ")}
                </code>
              </Field>
            )}

            <Field label="Approver group">
              <ApproverGroupPicker
                policyId={policy.id}
                value={policy.approverGroupId}
                onSaved={(approverGroupId) => setPolicy((current) => (current ? { ...current, approverGroupId } : current))}
              />
            </Field>

            <Field label="Priority">
              <span className="text-ui-body-sm">{policy.priority}</span>
            </Field>

            <Field label="Updated">
              <span className="text-ui-body-sm text-text2">
                {new Date(policy.updatedAt).toLocaleString()}
              </span>
            </Field>

            <div className="mt-6 pt-4 border-t border-border flex items-center justify-between gap-3">
              {policy.createdByThreadId ? (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() =>
                    router.push(`/work/${policy.createdByThreadId}`)
                  }
                >
                  view conversation
                </Button>
              ) : (
                <span />
              )}
              <Button
                variant="primary"
                size="sm"
                onClick={editInWork}
              >
                edit in /work
              </Button>
            </div>
          </article>
        )}
      </div>

      <CreatorCredit />
    </>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="mb-4 last:mb-0">
      <div className="text-ui-caption font-semibold text-text3 mb-1">
        {label}
      </div>
      <div className="text-text">{children}</div>
    </div>
  );
}
