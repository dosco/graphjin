"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { toast } from "sonner";
import AppHeader from "@/components/AppHeader";
import PageHeading from "@/components/PageHeading";
import SectionNav from "@/components/SectionNav";
import { ActionGroup } from "@/components/ui/action-group";
import { Button, ButtonLink } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Disclosure } from "@/components/ui/disclosure";
import { Field, Input, NativeSelect, Textarea } from "@/components/ui/field";
import { LocalDateTime } from "@/components/ui/local-date-time";
import { Badge, type BadgeVariant } from "@/components/ui/badge";

type PackStatus = {
  packId: string;
  version: string;
  status: string;
  readiness: Record<string, { status: string; reason: string | null }>;
  installedAt: string | null;
  lastError: string | null;
};

type DoctorResult = {
  packId: string;
  status: string;
  checks: Array<{ id: string; status: string; detail: string }>;
};

type StoreControl = {
  domain: string;
  automationEligible: boolean;
  enabled: boolean;
  autoExecute: boolean;
  readiness: string;
  readinessReason: string | null;
  readinessMessage: string;
  caps: Record<string, number>;
};

type ActivityItem = {
  id: string;
  kind: "change" | "handoff";
  title: string;
  description: string;
  outcome: "completed" | "reverted" | "awaiting_approval" | "in_progress" | "needs_attention" | "failed" | "cancelled";
  outcomeLabel: string;
  affectedCount: number;
  source: "requested_change" | "automatic_rule" | "test";
  sourceLabel: string;
  isTest: boolean;
  occurredAt: string;
  currentState: string | null;
  technical: {
    reference: string;
    area: string;
    operation: string;
    execution: string;
    originalRequest: string;
    bulkReference: string | null;
    inverseOfReference: string | null;
  };
};

export type StoreManagement = {
  controls: StoreControl[];
  rules: Array<{
    id: string;
    name: string;
    instruction: string;
    domain: string;
    actionKind: string;
    dailyCap: number;
    cooldownSeconds: number;
    enabled: boolean;
    suspendedReason: string | null;
    isTest: boolean;
  }>;
  changesets: Array<{
    id: string;
    domain: string;
    operationId: string;
    executionMode: string;
    status: string;
    summary: string;
    bulkUuid: string | null;
    inverseOfId: string | null;
    createdAt: string;
  }>;
  handoffs: Array<{
    id: string;
    kind: string;
    entityRef: string;
    status: string;
    createdAt: string;
  }>;
  activity: ActivityItem[];
  handoffOnly: { executePath: false; handoffKinds: string[] };
};

const CAP_LABELS: Record<string, string> = {
  maxRowsPerChangeset: "Items per change",
  maxPriceDeltaPercent: "Price delta (%)",
  maxDiscountPercent: "Discount (%)",
  maxCouponCount: "Coupons",
  maxProjectedExposure: "Projected exposure",
  maxDailyAutoActions: "Automatic actions/day",
  skuCooldownSeconds: "Time between changes (seconds)",
};

const DOMAIN_LABELS: Record<string, string> = {
  catalog: "Catalog",
  content: "Content",
  customers: "Customers",
  inventory: "Inventory",
  orders: "Orders",
  promotions: "Promotions",
};

function domainLabel(domain: string): string {
  return DOMAIN_LABELS[domain]
    ?? domain.replaceAll("_", " ").replace(/^./, (letter) => letter.toUpperCase());
}

function visibleCaps(control: StoreControl): Array<[string, number]> {
  const keys = ["maxRowsPerChangeset", "maxDailyAutoActions", "skuCooldownSeconds"];
  if (control.domain === "catalog") keys.push("maxPriceDeltaPercent");
  if (control.domain === "promotions") {
    keys.push("maxDiscountPercent", "maxCouponCount", "maxProjectedExposure");
  }
  return keys.flatMap((key) =>
    typeof control.caps[key] === "number" ? [[key, control.caps[key]] as [string, number]] : [],
  );
}

async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, { ...init, cache: "no-store" });
  const body = (await response.json().catch(() => null)) as (T & { error?: string }) | null;
  if (!response.ok) throw new Error(body?.error ?? `Request failed (HTTP ${response.status})`);
  return body as T;
}

function healthLabel(status: string): string {
  if (status === "ready" || status === "installed") return "Healthy";
  if (status === "degraded") return "Needs attention";
  if (status === "removed") return "Not installed";
  return status.replaceAll("_", " ");
}

function checkLabel(id: string): string {
  const changeArea = id.match(/^changes-(catalog|content|customers|inventory|orders|promotions)$/)?.[1];
  if (changeArea) return `${domainLabel(changeArea)} changes`;
  if (id === "changes") return "Magento changes";
  if (id === "analytics") return "Reporting access";
  if (id === "magento") return "Magento connection";
  if (id === "graphjin") return "Reporting service";
  if (id === "analytics-query") return "Live reporting check";
  if (id === "bulk-consumers") return "Bulk updates";
  return domainLabel(id.replaceAll("-", " "));
}

function checkDescription(check: DoctorResult["checks"][number]): string {
  const healthy = check.status === "ready";
  if (check.id === "analytics") {
    return healthy
      ? "OpenNeko can read Magento reporting data without permission to change it."
      : "OpenNeko cannot use the read-only Magento reporting login. Check the database connection and permissions.";
  }
  if (check.id === "magento") {
    return healthy
      ? "OpenNeko can reach the Magento store and identify its store configuration."
      : "OpenNeko cannot reach the Magento store. Check the store address and integration token.";
  }
  if (check.id === "graphjin") {
    return healthy
      ? "The Magento reporting service is ready."
      : "The Magento reporting service is unavailable. Check the reporting connection and try again.";
  }
  if (check.id === "analytics-query") {
    return healthy
      ? "A live Magento order query completed successfully."
      : "OpenNeko could not run a live Magento reporting query. Check the reporting login and database.";
  }
  if (check.id === "bulk-consumers") {
    return healthy
      ? "Magento finished its recent bulk updates."
      : "Magento has not completed its recent bulk updates. Check the Magento queue before trying another bulk change.";
  }
  return check.detail;
}

function checkStatusLabel(id: string, status: string): string {
  if (id === "changes" || id.startsWith("changes-")) {
    return status === "ready" ? "Changes available" : "View only";
  }
  if (status === "ready") return "Healthy";
  if (status === "optional") return "Optional";
  if (status === "blocked") return "Needs attention";
  return status.replaceAll("_", " ");
}

function checkTone(status: string): BadgeVariant {
  if (status === "ready") return "success";
  if (status === "optional") return "muted";
  return "danger";
}

function executionModeLabel(mode: string): string {
  if (mode === "approval_required") return "Administrator approval required";
  if (mode === "controlled_automation_eligible") return "Automatic under the configured store limits";
  if (mode === "handoff_only") return "Complete in Magento Admin";
  return mode.replaceAll("_", " ");
}

function dailyLimitLabel(limit: number): string {
  return `Up to ${limit} ${limit === 1 ? "change" : "changes"} per day`;
}

function cooldownLabel(seconds: number): string {
  if (seconds === 0) return "No waiting period";
  if (seconds % 3600 === 0) {
    const hours = seconds / 3600;
    return `At least ${hours} ${hours === 1 ? "hour" : "hours"} between changes`;
  }
  if (seconds % 60 === 0) {
    const minutes = seconds / 60;
    return `At least ${minutes} ${minutes === 1 ? "minute" : "minutes"} between changes`;
  }
  return `At least ${seconds} seconds between changes`;
}

function pausedRuleLabel(reason: string | null): string | null {
  if (!reason) return null;
  if (reason === "suspended_by_admin") return "Paused by an administrator";
  return `Paused: ${reason.replaceAll("_", " ")}`;
}

function activityTone(outcome: ActivityItem["outcome"]): BadgeVariant {
  if (outcome === "completed" || outcome === "reverted") return "success";
  if (outcome === "awaiting_approval" || outcome === "in_progress") return "watch";
  if (outcome === "needs_attention" || outcome === "failed") return "danger";
  return "muted";
}

export type MagentoPackAdminFixture = {
  status: PackStatus;
  doctor: DoctorResult;
  management: StoreManagement;
};

export default function MagentoPackAdmin({ fixture }: { fixture?: MagentoPackAdminFixture }) {
  const [status, setStatus] = useState<PackStatus | null>(fixture?.status ?? null);
  const [doctor, setDoctor] = useState<DoctorResult | null>(fixture?.doctor ?? null);
  const [management, setManagement] = useState<StoreManagement | null>(fixture?.management ?? null);
  const [loading, setLoading] = useState(!fixture);
  const [busy, setBusy] = useState<string | null>(null);
  const [rule, setRule] = useState({
    name: "",
    instruction: "",
    domain: "catalog",
    dailyCap: "5",
    cooldownSeconds: "3600",
  });

  const refresh = useCallback(async (runDoctor = false) => {
    setLoading(true);
    try {
      const nextStatus = await api<PackStatus>("/api/admin/packs/magento/status").catch((error) => {
        if (error instanceof Error && error.message.includes("not installed")) return null;
        throw error;
      });
      setStatus(nextStatus);
      if (nextStatus && nextStatus.status !== "removed") {
        const [nextManagement, nextDoctor] = await Promise.all([
          api<StoreManagement>("/api/admin/packs/magento/store-management"),
          runDoctor ? api<DoctorResult>("/api/admin/packs/magento/doctor") : Promise.resolve(null),
        ]);
        setManagement(nextManagement);
        if (nextDoctor) setDoctor(nextDoctor);
      } else if (!nextStatus || nextStatus.status === "removed") {
        setDoctor(null);
        setManagement(null);
      }
    } catch (error) {
      toast.error(error instanceof Error ? error.message : String(error));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (fixture) return;
    const initial = window.setTimeout(() => void refresh(true), 0);
    return () => window.clearTimeout(initial);
  }, [fixture, refresh]);

  async function runAction(action: "doctor" | "upgrade" | "uninstall") {
    if (action === "uninstall" && !window.confirm("Remove the Magento pack? Historical metrics and operation records will be kept.")) return;
    setBusy(action);
    try {
      if (action === "doctor") {
        setDoctor(await api<DoctorResult>("/api/admin/packs/magento/doctor"));
        toast.success("Connection and permissions checked.");
      } else {
        await api<PackStatus>(`/api/admin/packs/magento/${action}`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: "{}",
        });
        toast.success(action === "upgrade" ? "Magento pack updated." : "Magento pack removed safely.");
        await refresh(action === "upgrade");
      }
    } catch (error) {
      toast.error(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(null);
    }
  }

  async function updateStoreManagement(
    key: string,
    input: Record<string, unknown>,
    success: string,
  ) {
    setBusy(key);
    try {
      const next = await api<StoreManagement>("/api/admin/packs/magento/store-management", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(input),
      });
      setManagement(next);
      toast.success(success);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(null);
    }
  }

  async function createRule(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const eligibleDomains = management?.controls.filter(
      (control) => control.enabled && control.autoExecute && control.automationEligible,
    ) ?? [];
    const domain = eligibleDomains.some((control) => control.domain === rule.domain)
      ? rule.domain
      : eligibleDomains[0]?.domain;
    if (!domain) {
      toast.error("Allow routine changes to run automatically in at least one store area first.");
      return;
    }
    await updateStoreManagement(
      "create-rule",
      {
        action: "create_rule",
        name: rule.name,
        instruction: rule.instruction,
        domain,
        actionKind: `magento.manage_${domain}`,
        dailyCap: Number(rule.dailyCap),
        cooldownSeconds: Number(rule.cooldownSeconds),
        enabled: true,
      },
      "Automatic rule saved with its daily limit and waiting period.",
    );
    setRule((current) => ({ ...current, name: "", instruction: "" }));
  }

  const installed = status && status.status !== "removed";
  const visibleRules = management?.rules.filter((item) => !item.isTest) ?? [];

  return (
    <div className="root">
      <AppHeader back={{ href: "/admin/settings", label: "Settings" }}>
        <SectionNav current="admin" />
      </AppHeader>
      <PageHeading
        title="Store operations"
        description="Manage the installed pack’s operational controls."
      />

      {loading && !status ? (
        <section className="settings-card"><p className="settings-card-copy">Checking Magento…</p></section>
      ) : installed ? (
        <>
        <PackGroup
          kicker="Installed"
          title="Installed packs"
          description="The packs running in this workspace right now."
        >
          <section className="settings-card">
            <div className="settings-card-head">
              <div>
                <h2 className="settings-card-title">Magento pack</h2>
                <p className="settings-card-copy">Version {status.version} · installed {status.installedAt ? <LocalDateTime value={status.installedAt} fallback="recently" /> : "recently"}</p>
              </div>
              <div className="settings-source">
                <strong className={doctor?.status === "blocked" ? "is-warn" : "is-ok"}>{healthLabel(doctor?.status ?? status.status)}</strong>
              </div>
            </div>
            {status.lastError ? <p className="mt-3 text-sm text-danger">{status.lastError}</p> : null}
            <ActionGroup align="start" className="mt-5">
              <Button type="button" disabled={busy !== null} onClick={() => void runAction("doctor")}>{busy === "doctor" ? "Checking…" : "Check health"}</Button>
              <Button type="button" variant="secondary" disabled={busy !== null} onClick={() => void runAction("upgrade")}>{busy === "upgrade" ? "Updating…" : "Update pack"}</Button>
              <ButtonLink href="/admin/settings/packs?pack=magento">Configure pack</ButtonLink>
            </ActionGroup>
          </section>

          <section className="settings-card">
            <div className="settings-card-head">
              <div>
                <h2 className="settings-card-title">Use your Magento pack</h2>
                <p className="settings-card-copy">Everything is installed already. These are the everyday places your team will use.</p>
              </div>
            </div>
            <div className="mt-4 grid gap-3 sm:grid-cols-2">
              {[
                { href: "/chat", title: "Ask about the store", copy: "Open a chat and ask how the store is doing." },
                { href: "/workflows", title: "Run automations", copy: "Start or review Magento workflows." },
                { href: "/actions", title: "Review proposed changes", copy: "Approve or reject a specific Magento change prepared by OpenNeko." },
                { href: "/skills", title: "Magento skills", copy: "Choose a focused skill for orders, fulfillment, refunds, inventory, performance, or platform health." },
              ].map((item) => (
                <Link key={item.href} href={item.href} className="rounded-xl border border-border px-4 py-3 transition hover:border-accent">
                  <strong className="font-display text-ui-body font-bold text-text">{item.title}</strong>
                  <p className="mt-1 text-ui-body-sm leading-[1.45] text-text3">{item.copy}</p>
                </Link>
              ))}
            </div>
          </section>

        </PackGroup>

        <PackGroup
          kicker="Configure"
          title="Settings for installed packs"
          description="Change access, automations, health, and credentials for what's installed."
        >
          {management ? (
            <section className="settings-card">
              <div className="settings-card-head">
                <div>
                  <h2 className="settings-card-title">Store change access</h2>
                  <p className="settings-card-copy">Choose which areas can prepare changes. Sensitive changes always require administrator approval, and every write is reconciled against Magento.</p>
                </div>
              </div>

              <div className="mt-4 grid gap-3 sm:grid-cols-2">
                {management.controls.map((control) => (
                  <div key={control.domain} className="rounded-xl border border-border px-4 py-4">
                    <div className="flex items-start justify-between gap-4">
                      <div className="min-w-0 flex-1">
                        <strong className="font-display text-ui-body font-bold text-text">{domainLabel(control.domain)}</strong>
                        <p className="mt-1 text-ui-body-sm text-text3">
                          {control.automationEligible
                            ? "Routine changes in this area can be automated. Higher-risk changes still wait for administrator approval."
                            : "OpenNeko can prepare changes in this area, but an administrator must approve every one."}
                        </p>
                        <p className={`mt-2 font-body text-ui-caption font-semibold ${control.readiness === "ready" && control.enabled ? "text-success-ink" : "text-text3"}`}>
                          {control.readiness !== "ready" || !control.enabled
                            ? control.readinessMessage
                            : control.autoExecute
                              ? "Routine changes can run automatically"
                              : "Every change waits for approval"}
                        </p>
                      </div>
                      <Checkbox
                        label="Enabled"
                        className="shrink-0 whitespace-nowrap text-ui-caption font-semibold"
                        checked={control.enabled}
                        disabled={busy !== null}
                        onCheckedChange={(checked) => void updateStoreManagement(
                          `domain-${control.domain}`,
                          { action: "update_domain", domain: control.domain, enabled: checked === true },
                          `${domainLabel(control.domain)} change access updated.`,
                        )}
                      />
                    </div>
                    <Checkbox
                      label="Allow routine changes to run automatically"
                      className="mt-4"
                      checked={control.autoExecute}
                      disabled={busy !== null || !control.enabled || !control.automationEligible}
                      onCheckedChange={(checked) => void updateStoreManagement(
                        `auto-${control.domain}`,
                        { action: "update_domain", domain: control.domain, autoExecute: checked === true },
                        `${domainLabel(control.domain)} automatic execution updated.`,
                      )}
                    />
                    <p className="mt-2 text-ui-caption leading-[var(--leading-compact)] text-text3">Up to {control.caps.maxRowsPerChangeset ?? 0} items per change; {control.caps.maxDailyAutoActions ?? 0} automatic actions per day.</p>
                    <Disclosure title="Edit limits" className="mt-3">
                      <div className="grid gap-3 sm:grid-cols-2">
                        {visibleCaps(control).map(([key, value]) => (
                          <Field
                            key={`${control.domain}-${key}`}
                            label={CAP_LABELS[key] ?? key}
                            htmlFor={`${control.domain}-${key}`}
                          >
                            <Input
                              id={`${control.domain}-${key}`}
                              key={`${control.domain}-${key}-${value}`}
                              type="number"
                              min="0"
                              defaultValue={value}
                              disabled={busy !== null}
                              onBlur={(event) => {
                                const next = Number(event.target.value);
                                if (Number.isFinite(next) && next >= 0 && next !== value) {
                                  void updateStoreManagement(
                                    `cap-${control.domain}-${key}`,
                                    { action: "update_domain", domain: control.domain, caps: { [key]: next } },
                                    `${domainLabel(control.domain)} limit updated.`,
                                  );
                                }
                              }}
                            />
                          </Field>
                        ))}
                      </div>
                    </Disclosure>
                  </div>
                ))}
              </div>

              <div className="mt-4 rounded-xl border border-danger/30 bg-danger/5 px-4 py-3">
                <strong className="text-sm text-text">Actions OpenNeko will not perform</strong>
                <p className="mt-1 text-ui-body-sm leading-[1.5] text-text2">OpenNeko cannot issue online refunds, approve returns, change financial configuration, or perform money-out operations. It prepares evidence and a Magento Admin handoff only.</p>
              </div>

              <div className="mt-6 border-t border-border pt-5">
                <h3>Automatic rules</h3>
                <p className="mt-1 text-ui-body-sm text-text3">Each rule stops at its daily limit and waits the configured time before changing the same item again.</p>
                {management.controls.some((control) => control.enabled && control.autoExecute && control.automationEligible) ? (
                  <form className="mt-4 grid gap-3 sm:grid-cols-2" onSubmit={createRule}>
                    <Field label="Rule name" htmlFor="magento-rule-name">
                      <Input id="magento-rule-name" required maxLength={120} value={rule.name} onChange={(event) => setRule((current) => ({ ...current, name: event.target.value }))} />
                    </Field>
                    <Field label="Store area" htmlFor="magento-rule-domain">
                      <NativeSelect id="magento-rule-domain" value={rule.domain} onChange={(event) => setRule((current) => ({ ...current, domain: event.target.value }))}>
                        {management.controls.filter((control) => control.enabled && control.autoExecute && control.automationEligible).map((control) => <option key={control.domain} value={control.domain}>{domainLabel(control.domain)}</option>)}
                      </NativeSelect>
                    </Field>
                    <Field label="Plain-language instruction" htmlFor="magento-rule-instruction" className="sm:col-span-2">
                      <Textarea id="magento-rule-instruction" required maxLength={1000} rows={3} value={rule.instruction} onChange={(event) => setRule((current) => ({ ...current, instruction: event.target.value }))} />
                    </Field>
                    <Field label="Daily limit" htmlFor="magento-rule-daily-limit">
                      <Input id="magento-rule-daily-limit" required type="number" min="1" value={rule.dailyCap} onChange={(event) => setRule((current) => ({ ...current, dailyCap: event.target.value }))} />
                    </Field>
                    <Field label="Time between changes (seconds)" htmlFor="magento-rule-cooldown">
                      <Input id="magento-rule-cooldown" required type="number" min="0" value={rule.cooldownSeconds} onChange={(event) => setRule((current) => ({ ...current, cooldownSeconds: event.target.value }))} />
                    </Field>
                    <div className="sm:col-span-2"><Button type="submit" disabled={busy !== null}>{busy === "create-rule" ? "Saving…" : "Save automatic rule"}</Button></div>
                  </form>
                ) : (
                  <p className="mt-3 rounded-lg bg-bg2 px-3 py-3 text-ui-body-sm text-text3">Turn on “Allow routine changes to run automatically” for an eligible area before creating a rule.</p>
                )}
                {visibleRules.length > 0 ? (
                  <ul className="mt-4 flex flex-col gap-2">
                    {visibleRules.map((item) => {
                      const pausedLabel = pausedRuleLabel(item.suspendedReason);
                      return (
                        <li key={item.id} className="flex items-start justify-between gap-3 rounded-xl border border-border px-4 py-3">
                          <div>
                            <strong className="font-display text-ui-body font-bold text-text">{item.name}</strong>
                            <p className="mt-1 text-ui-body-sm text-text3">{item.instruction}</p>
                            <p className="mt-1 text-ui-caption text-text3">{domainLabel(item.domain)} · {dailyLimitLabel(item.dailyCap)} · {cooldownLabel(item.cooldownSeconds)}{pausedLabel ? ` · ${pausedLabel}` : ""}</p>
                          </div>
                          <Button
                            type="button"
                            variant="secondary"
                            disabled={busy !== null}
                            onClick={() => void updateStoreManagement(
                              `rule-${item.id}`,
                              { action: "set_rule_status", ruleId: item.id, enabled: !item.enabled },
                              item.enabled ? "Automatic rule suspended." : "Automatic rule enabled.",
                            )}
                          >{item.enabled ? "Suspend" : "Enable"}</Button>
                        </li>
                      );
                    })}
                  </ul>
                ) : null}
              </div>

              <div className="mt-6 border-t border-border pt-5">
                <h3>Recent activity</h3>
                <p className="mt-1 text-ui-body-sm text-text3">What changed in Magento and whether anything still needs attention.</p>
                {management.activity.filter((item) => !item.isTest).length === 0 ? (
                  <p className="mt-3 text-ui-body-sm text-text3">No store changes yet.</p>
                ) : (
                  <ActivityList items={management.activity.filter((item) => !item.isTest).slice(0, 8)} />
                )}
                {management.activity.some((item) => item.isTest) ? (
                  <Disclosure
                    title="Test activity"
                    meta={`${management.activity.filter((item) => item.isTest).length} hidden`}
                    className="mt-3"
                  >
                    <p className="mb-3 text-ui-body-sm text-text3">Local acceptance checks are kept for audit and hidden from everyday activity.</p>
                    <ActivityList items={management.activity.filter((item) => item.isTest)} />
                  </Disclosure>
                ) : null}
              </div>
            </section>
          ) : null}

          {doctor ? (
            <section className="settings-card">
              <div className="settings-card-head">
                <div>
                  <h2 className="settings-card-title">Health</h2>
                  <p className="settings-card-copy">Plain-language checks for Magento, the reporting connection, and OpenNeko.</p>
                </div>
              </div>
              <ul className="mt-4 flex flex-col gap-3">
                {doctor.checks.map((check) => {
                  const description = checkDescription(check);
                  return (
                    <li key={check.id} className="flex flex-col gap-1 rounded-xl border border-border px-4 py-3 sm:flex-row sm:items-start sm:justify-between">
                      <div className="min-w-0 flex-1">
                        <strong className="font-display text-ui-body font-bold text-text">{checkLabel(check.id)}</strong>
                        <p className="mt-1 text-ui-body-sm leading-[1.45] text-text3">{description}</p>
                        {description !== check.detail ? (
                          <Disclosure title="Technical details" className="mt-2">
                            <p className="break-all text-ui-caption leading-[var(--leading-compact)] text-text3">{check.detail}</p>
                          </Disclosure>
                        ) : null}
                      </div>
                      <Badge variant={checkTone(check.status)} className="mt-1">{checkStatusLabel(check.id, check.status)}</Badge>
                    </li>
                  );
                })}
              </ul>
            </section>
          ) : null}

          <section className="settings-card">
            <div className="settings-card-head">
              <div>
                <h2 className="settings-card-title">Remove pack</h2>
                <p className="settings-card-copy">Disables its live configuration and automations. Historical metrics and audit records stay available.</p>
              </div>
              <Button type="button" variant="danger" disabled={busy !== null} onClick={() => void runAction("uninstall")}>{busy === "uninstall" ? "Removing…" : "Remove"}</Button>
            </div>
          </section>
        </PackGroup>
        </>
      ) : null}


    </div>
  );
}

function PackGroup({
  kicker,
  title,
  description,
  children,
}: {
  kicker: string;
  title: string;
  description?: string;
  children: React.ReactNode;
}) {
  return (
    <section className="settings-group">
      <div className="settings-group-head">
        <span className="settings-group-kicker">{kicker}</span>
        <h2 className="settings-group-title">{title}</h2>
        {description ? <p className="settings-group-copy">{description}</p> : null}
      </div>
      {children}
    </section>
  );
}

function ActivityList({ items }: { items: ActivityItem[] }) {
  return (
    <ul className="mt-3 flex flex-col gap-2">
      {items.map((item) => (
        <li key={item.id} className="rounded-inner border border-border px-4 py-3">
          <div className="flex min-w-0 flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
            <div className="min-w-0">
              <strong className="font-display text-ui-body font-bold text-text">{item.title}</strong>
              <p className="mt-1 text-ui-body-sm leading-[var(--leading-body)] text-text2">{item.description}</p>
              <p className="mt-1.5 text-ui-caption leading-[var(--leading-compact)] text-text3">
                <LocalDateTime value={item.occurredAt} /> · {item.sourceLabel}
                {item.currentState ? ` · ${item.currentState}` : ""}
              </p>
            </div>
            <Badge variant={activityTone(item.outcome)}>{item.outcomeLabel}</Badge>
          </div>
          <Disclosure title="View details" meta={item.technical.area} className="mt-3">
            <dl className="grid gap-x-5 gap-y-3 text-ui-caption sm:grid-cols-2">
              <div>
                <dt className="font-bold text-text2">Requested as</dt>
                <dd className="mt-0.5 text-text3">{item.technical.originalRequest}</dd>
              </div>
              <div>
                <dt className="font-bold text-text2">How it runs</dt>
                <dd className="mt-0.5 text-text3">{executionModeLabel(item.technical.execution)}</dd>
              </div>
              <div>
                <dt className="font-bold text-text2">Operation</dt>
                <dd className="mt-0.5 font-mono text-text3">{item.technical.operation.replaceAll("_", " ")}</dd>
              </div>
              <div>
                <dt className="font-bold text-text2">Audit reference</dt>
                <dd className="mt-0.5 break-all font-mono text-text3">{item.technical.reference}</dd>
              </div>
              {item.technical.inverseOfReference ? (
                <div>
                  <dt className="font-bold text-text2">Restores change</dt>
                  <dd className="mt-0.5 break-all font-mono text-text3">{item.technical.inverseOfReference}</dd>
                </div>
              ) : null}
              {item.technical.bulkReference ? (
                <div>
                  <dt className="font-bold text-text2">Magento job reference</dt>
                  <dd className="mt-0.5 break-all font-mono text-text3">{item.technical.bulkReference}</dd>
                </div>
              ) : null}
            </dl>
          </Disclosure>
        </li>
      ))}
    </ul>
  );
}
