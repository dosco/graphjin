"use client";

import { useEffect, useState } from "react";
import { SkeletonList } from "@/components/ui/skeleton";
import { useRouter } from "next/navigation";
import { Plus } from "lucide-react";
import AppHeader from "@/components/AppHeader";
import CreatorCredit from "@/components/CreatorCredit";
import PageHeading from "@/components/PageHeading";
import SectionNav from "@/components/SectionNav";
import { ActionGroup } from "@/components/ui/action-group";
import { Button } from "@/components/ui/button";
import { Disclosure } from "@/components/ui/disclosure";
import { Badge, type BadgeVariant } from "@/components/ui/badge";
import { SearchInput } from "@/components/ui/search-input";
import { cn } from "@/lib/cn";
import { matchesListSearch } from "@/lib/list-search";
import SkillLearnCard from "./SkillLearnCard";

type Policy = {
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
  priority: number;
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
};

function describeMode(mode: string): string {
  switch (mode) {
    case "auto_approve":
      return "auto-approves";
    case "approval_required":
      return "requires approval";
    case "observe_only":
      return "observes only";
    case "draft_only":
      return "drafts only";
    case "never":
      return "never executes";
    default:
      return mode.replace(/_/g, " ");
  }
}

function modePillVariant(mode: string): BadgeVariant {
  switch (mode) {
    case "auto_approve":
      return "success";
    case "approval_required":
      return "watch";
    case "never":
      return "danger";
    default:
      return "muted";
  }
}

function describeAutoApproveThreshold(level: string | null): string | null {
  if (!level) return null;
  return `auto-approves risk ${level} and below`;
}

function describeTargets(
  obj: Record<string, unknown> | null,
  label: "Allowed" | "Denied",
): string | null {
  const patterns = targetPatterns(obj);
  if (patterns.length === 0) return null;
  const miss = label === "Allowed" && obj?.on_miss === "next" ? "; others go to the next rule" : "";
  return `${label} targets: ${patterns.join(", ")}${miss}`;
}

export function targetPatterns(obj: Record<string, unknown> | null): string[] {
  return Array.isArray(obj?.patterns) ? obj.patterns.filter((p): p is string => typeof p === "string") : [];
}

function describeLimits(limits: Record<string, unknown>): string | null {
  if (!limits || Object.keys(limits).length === 0) return null;
  const parts: string[] = [];
  if (typeof limits.daily_cap === "number") {
    parts.push(`${limits.daily_cap}/day`);
  }
  if (typeof limits.hourly_cap === "number") {
    parts.push(`${limits.hourly_cap}/hour`);
  }
  if (typeof limits.concurrency === "number") {
    parts.push(`${limits.concurrency} concurrent`);
  }
  if (parts.length === 0) return null;
  return `Limits: ${parts.join(" · ")}`;
}

type ActionDescriptor = {
  kind: string;
  description: string;
  default_mode?:
    | "auto"
    | "ask"
    | "deny"
    | {
        external?: "auto" | "ask" | "deny";
        internal?: "auto" | "ask" | "deny";
      };
};

function resolveEffectiveMode(
  kind: string,
  policies: Policy[],
): { mode: string; policyId: string | null } {
  // Most specific match wins: a kind-explicit, external-scoped,
  // enabled, highest-priority policy. Fall back to a kind-empty
  // external_default if nothing kind-specific exists. Mirrors how
  // evaluateActionPolicy walks policies at runtime — but kept
  // visually simple here (a single "current mode" per kind).
  const candidates = policies
    .filter((p) => p.enabled)
    .filter(
      (p) => p.appliesToScopes.length === 0 || p.appliesToScopes.includes("external"),
    );
  const kindSpecific = candidates
    .filter((p) => p.appliesToKinds.includes(kind))
    .sort((a, b) => a.priority - b.priority);
  if (kindSpecific[0]) {
    return { mode: kindSpecific[0].mode, policyId: kindSpecific[0].id };
  }
  const generic = candidates
    .filter((p) => p.appliesToKinds.length === 0)
    .sort((a, b) => a.priority - b.priority);
  if (generic[0]) {
    return { mode: generic[0].mode, policyId: generic[0].id };
  }
  return { mode: "no policy", policyId: null };
}

export default function RulesClient() {
  const router = useRouter();
  const [policies, setPolicies] = useState<Policy[] | null>(null);
  const [actionDescriptors, setActionDescriptors] = useState<
    ActionDescriptor[]
  >([]);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");

  useEffect(() => {
    let cancelled = false;
    void Promise.all([
      fetch("/api/policies", { cache: "no-store" }).then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json() as Promise<{ policies: Policy[] }>;
      }),
      fetch("/api/action-descriptors", { cache: "no-store" })
        .then((r) => (r.ok ? r.json() : { descriptors: [] }))
        .then(
          (data) =>
            (data as { descriptors?: ActionDescriptor[] }).descriptors ?? [],
        )
        .catch(() => [] as ActionDescriptor[]),
    ])
      .then(([policiesResult, descriptors]) => {
        if (cancelled) return;
        setPolicies(policiesResult.policies ?? []);
        setActionDescriptors(descriptors);
      })
      .catch((err) => {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : "Failed to load");
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const filteredPolicies = policies?.filter((policy) =>
    matchesListSearch(
      query,
      policy.name,
      policy.description,
      policy.mode,
      ...policy.appliesToKinds,
      ...policy.appliesToScopes,
    ),
  );

  return (
    <>
      <div
        className="root"
      >
        <AppHeader back={{ href: "/admin", label: "Admin" }}>
          <SectionNav current="admin" />
        </AppHeader>

        <PageHeading
          title="Rules"
          description="Control skill learning and what agents may execute automatically, queue for review, or never run."
          actions={
          <Button
            variant="primary"
            onClick={() =>
              router.push(
                `/work?seed=${encodeURIComponent("Add a new rule that ")}`,
              )
            }
          >
            <Plus aria-hidden="true" />
            New rule
          </Button>
          }
        />

        <div className="mb-5 max-w-[520px]">
          <SearchInput
            label="Search rules and action kinds"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search rules and action kinds"
          />
        </div>

        <SkillLearnCard />

        {error ? (
          <div className="py-14 text-center text-sm text-danger">{error}</div>
        ) : policies === null ? (
          <SkeletonList rows={3} variant="row" label="Loading rules" />
        ) : (
          <>
            {actionDescriptors.length > 0 ? (
              <BuiltInActionsSection
                descriptors={actionDescriptors}
                policies={policies}
                query={query}
                onEditPolicy={(policyId) => {
                  const policy = policies.find((p) => p.id === policyId);
                  const name = policy?.name ?? "this rule";
                  router.push(
                    `/work?seed=${encodeURIComponent(`Update the '${name}' rule to `)}`,
                  );
                }}
              />
            ) : null}

            {policies.length === 0 ? (
              <div className="py-14 px-6 text-center text-sm text-text3 leading-[1.55] max-w-[520px] mx-auto">
                No rules yet. Defaults are seeded automatically the first time a
                workflow proposes an action that needs gating.
              </div>
            ) : filteredPolicies?.length === 0 ? (
              <div className="py-10 text-center text-ui-body-sm text-text2">
                No rules match “{query}”.
              </div>
            ) : (
              <section className="mt-8">
              <h2 className="mb-2.5 font-display text-ui-section font-bold tracking-[-0.01em] text-text">
                Rules
              </h2>
              <ul className="list-none p-0 m-0 flex flex-col gap-2">
                {filteredPolicies?.map((p) => (
                  <PolicyCard
                    key={p.id}
                    policy={p}
                    onEdit={() =>
                      router.push(
                        `/work?seed=${encodeURIComponent(`Update the '${p.name}' rule to `)}`,
                      )
                    }
                    onOpen={() => router.push(`/admin/rules/${p.id}`)}
                  />
                ))}
              </ul>
              </section>
            )}
          </>
        )}
      </div>

      <CreatorCredit />
    </>
  );
}

function PolicyCard({
  policy,
  onEdit,
  onOpen,
}: {
  policy: Policy;
  onEdit: () => void;
  onOpen: () => void;
}) {
  const appliesKinds = policy.appliesToKinds.length
    ? policy.appliesToKinds.join(", ")
    : "any action";
  const appliesScopes = policy.appliesToScopes.length
    ? policy.appliesToScopes.join(", ")
    : null;
  const autoApprove = describeAutoApproveThreshold(
    policy.riskThresholdAutoApprove,
  );
  const allowed = describeTargets(policy.allowedTargets, "Allowed");
  const denied = describeTargets(policy.deniedTargets, "Denied");
  const limits = describeLimits(policy.limits);

  return (
    <Disclosure
      title={policy.name}
      meta={
        <Badge variant={modePillVariant(policy.mode)}>
          {describeMode(policy.mode)}
        </Badge>
      }
      className={cn(!policy.enabled && "opacity-60")}
    >
      {policy.description && (
        <p className="mb-3 text-ui-body text-text2 leading-[1.55]">
          {policy.description}
        </p>
      )}

      <dl className="m-0 flex flex-col gap-1 text-ui-body-sm">
        <Row label="Applies to">
          <span className="font-mono text-xs text-text2">{appliesKinds}</span>
          {appliesScopes && (
            <>
              {" "}· scope{" "}
              <span className="font-mono text-xs text-text2">{appliesScopes}</span>
            </>
          )}
        </Row>
        {autoApprove && <Row label="Auto-approve">{autoApprove}</Row>}
        {allowed && <Row label="Allowed">{allowed.replace(/^Allowed targets:\s*/, "")}</Row>}
        {denied && <Row label="Denied">{denied.replace(/^Denied targets:\s*/, "")}</Row>}
        {limits && <Row label="Limits">{limits.replace(/^Limits:\s*/, "")}</Row>}
        {policy.approverRole && <Row label="Approver">{policy.approverRole}</Row>}
        {!policy.enabled && <Row label="Status">disabled</Row>}
      </dl>
      <ActionGroup align="start" className="mt-4">
        <Button
          size="sm"
          onClick={onOpen}
          aria-label={`Open rule ${policy.name}`}
        >
          View details
        </Button>
        <Button
          variant="ghost"
          size="sm"
          onClick={onEdit}
          aria-label={`Edit rule ${policy.name}`}
        >
          Edit in Work
        </Button>
      </ActionGroup>
    </Disclosure>
  );
}

function Row({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="grid grid-cols-[110px_minmax(0,1fr)] gap-2.5 items-baseline max-[480px]:grid-cols-1 max-[480px]:gap-0.5">
      <dt className="text-ui-caption font-semibold text-text3">
        {label}
      </dt>
      <dd className="m-0 text-text break-words">{children}</dd>
    </div>
  );
}

function groupNameFromKind(kind: string): string {
  // Kinds are namespaced like `records.create` or `records_backfill`;
  // group them for display by the leading token.
  return kind.split(/[._]/)[0] ?? kind;
}

function describeDefaultMode(
  mode: ActionDescriptor["default_mode"],
): string {
  if (mode === undefined) return "no default declared";
  if (typeof mode === "string") {
    if (mode === "auto") return "auto by default";
    if (mode === "ask") return "asks by default";
    if (mode === "deny") return "denied by default";
    return "no default declared";
  }
  const parts: string[] = [];
  if (mode.external) parts.push(`external: ${mode.external}`);
  if (mode.internal) parts.push(`internal: ${mode.internal}`);
  return parts.length > 0 ? parts.join(" · ") : "no default declared";
}

function BuiltInActionsSection({
  descriptors,
  policies,
  query,
  onEditPolicy,
}: {
  descriptors: ActionDescriptor[];
  policies: Policy[];
  query: string;
  onEditPolicy: (policyId: string) => void;
}) {
  // Descriptors are flat; group them for display by the kind's leading token.
  const normalizedQuery = query.trim().toLowerCase();
  const visibleDescriptors = descriptors.filter((descriptor) =>
    [descriptor.kind, descriptor.description, groupNameFromKind(descriptor.kind)]
      .some((value) => value.toLowerCase().includes(normalizedQuery)),
  );
  const groups = new Map<string, ActionDescriptor[]>();
  for (const d of visibleDescriptors) {
    const key = groupNameFromKind(d.kind);
    const list = groups.get(key) ?? [];
    list.push(d);
    groups.set(key, list);
  }

  return (
    <section className="mt-8 mb-6">
      <div className="flex items-baseline justify-between mb-2.5">
        <h2 className="font-display text-ui-section font-bold tracking-[-0.01em] text-text">
          Built-in actions
        </h2>
        <span className="font-mono text-ui-label text-text3">
          {visibleDescriptors.length} of {descriptors.length} action kind
          {descriptors.length === 1 ? "" : "s"}
        </span>
      </div>
      <p className="text-ui-body-sm leading-[1.5] text-text2 mb-3 max-w-[640px]">
        Built-in action kinds the agent can call from /work. Each
        kind&apos;s effective approval mode comes from the rule whose
        <em> applies-to-kinds </em>
        includes it; everything else falls through to{" "}
        <code className="font-mono text-ui-caption bg-neutral px-1 rounded">
          external_default
        </code>
        . Click a kind to edit the rule that governs it.
      </p>
      {groups.size === 0 ? (
        <div className="border-y border-border py-8 text-center text-ui-body-sm text-text2">
          No action kinds match “{query}”.
        </div>
      ) : (
        <ul className="list-none p-0 m-0 flex flex-col gap-2">
          {[...groups.entries()].map(([groupName, kinds]) => (
            <li key={groupName}>
              <Disclosure
                title={groupName}
                meta={`${kinds.length} kind${kinds.length === 1 ? "" : "s"}`}
                open={normalizedQuery ? true : undefined}
              >
                <div className="flex flex-col gap-2">
                  {kinds.map((descriptor) => {
                    const { mode, policyId } = resolveEffectiveMode(
                      descriptor.kind,
                      policies,
                    );
                    return (
                      <div
                        key={descriptor.kind}
                        className="flex min-w-0 items-center justify-between gap-3 border-b border-border px-1 py-2.5 last:border-0 max-[640px]:items-stretch max-[640px]:flex-col"
                      >
                        <div className="flex flex-col gap-0.5 min-w-0">
                          <code className="font-mono text-ui-caption text-text [overflow-wrap:anywhere]">
                            {descriptor.kind}
                          </code>
                          <span className="text-ui-caption text-text3 max-[640px]:whitespace-normal [overflow-wrap:anywhere]">
                            {descriptor.description}
                          </span>
                        </div>
                        <div className="flex min-w-0 items-center justify-end gap-2 flex-wrap max-[640px]:justify-start">
                          <Badge variant={modePillVariant(mode)}>
                            {describeMode(mode)}
                          </Badge>
                          <span className="min-w-0 text-ui-label text-text3 [overflow-wrap:anywhere]">
                            {describeDefaultMode(descriptor.default_mode)}
                          </span>
                          {policyId ? (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => onEditPolicy(policyId)}
                            >
                              Edit rule
                            </Button>
                          ) : null}
                        </div>
                      </div>
                    );
                  })}
                </div>
              </Disclosure>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
