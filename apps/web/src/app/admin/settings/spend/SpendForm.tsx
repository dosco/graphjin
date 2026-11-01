"use client";

import { useState } from "react";
import { toast } from "sonner";
import type { MetricRefresh, SpendLimitsUsd, SpendSettings, SpendWindowUsage, WorkflowSpendRow } from "@neko/llm/spend";
import { AdminError } from "@/components/admin/AdminError";
import { adminApi } from "@/components/admin/admin-api";
import AppHeader from "@/components/AppHeader";
import CreatorCredit from "@/components/CreatorCredit";
import PageHeading from "@/components/PageHeading";
import SectionNav from "@/components/SectionNav";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Button } from "@/components/ui/button";
import { Field, Input, NativeSelect } from "@/components/ui/field";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

const usd = (value: number) =>
  value.toLocaleString("en-US", { style: "currency", currency: "USD", minimumFractionDigits: 2 });

const utcTime = (iso: string) => `${iso.slice(11, 16)} UTC`;

type DollarKey = Exclude<keyof SpendLimitsUsd, "warnPercent">;

const LIMIT_FIELDS: Array<{ key: DollarKey; label: string; hint: string }> = [
  { key: "runCapUsd", label: "Per-run cap", hint: "Held for each run while it runs. A run stops at its next tool call once it spends more. A turn with no known price is charged this amount." },
  { key: "orgHourlyUsd", label: "Organization hourly budget", hint: "All spend in this organization, per UTC hour." },
  { key: "orgDailyUsd", label: "Organization daily budget", hint: "All spend in this organization, per UTC day." },
  { key: "workflowHourlyUsd", label: "Workflow hourly budget", hint: "Default for each workflow, per UTC hour. A workflow can override it below." },
  { key: "workflowDailyUsd", label: "Workflow daily budget", hint: "Default for each workflow, per UTC day." },
];

const METRIC_REFRESH_OPTIONS: Array<{ value: MetricRefresh; label: string }> = [
  { value: "card", label: "On each card's schedule" },
  { value: "daily", label: "At most once a day" },
  { value: "weekly", label: "At most once a week" },
  { value: "off", label: "Off" },
];

export default function SpendForm({ initial }: { initial: SpendSettings }) {
  const [settings, setSettings] = useState(initial);
  const [draft, setDraft] = useState<Record<keyof SpendLimitsUsd, string>>(() => toDraft(initial.limits));
  const [metricRefresh, setMetricRefresh] = useState<MetricRefresh>(initial.metricRefresh);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function saveLimits(event: React.FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError(null);
    const body = {
      ...Object.fromEntries(Object.entries(draft).map(([key, value]) => [key, Number(value)])),
      metricRefresh,
    };
    const result = await adminApi<SpendSettings>("/api/admin/spend", "PUT", body);
    setSaving(false);
    if (!result.ok) return setError(result.error);
    setSettings(result.body);
    setDraft(toDraft(result.body.limits));
    setMetricRefresh(result.body.metricRefresh);
    toast.success("Spending limits saved.");
  }

  return (
    <>
      <div className="root">
        <AppHeader back={{ href: "/admin/settings", label: "Settings" }}>
          <SectionNav current="admin" />
        </AppHeader>

        <PageHeading
          title="Spending limits"
          description="Model spend for chat, channels, workflows and the API. A run that would pass a budget does not start."
        />

        <SpendAlerts settings={settings} onSaved={setSettings} />

        <section className="grid grid-cols-2 gap-4 max-[720px]:grid-cols-1">
          <UsageCard title="This hour" usage={settings.org.hour} warnPercent={settings.limits.warnPercent} />
          <UsageCard title="Today" usage={settings.org.day} warnPercent={settings.limits.warnPercent} />
        </section>

        <form onSubmit={saveLimits} className="settings-card mt-6 flex flex-col gap-5">
          <div>
            <h2 className="settings-card-title">Limits</h2>
            <p className="settings-card-copy">
              Amounts are in US dollars. The operator ceiling for each limit is shown under its field.
            </p>
          </div>
          <AdminError message={error} />
          <div className="grid grid-cols-2 gap-4 max-[720px]:grid-cols-1">
            {LIMIT_FIELDS.map(({ key, label, hint }) => (
              <Field
                key={key}
                label={label}
                htmlFor={`spend-${key}`}
                hint={`${hint} Ceiling ${usd(settings.ceilings[key])}.`}
              >
                <Input
                  id={`spend-${key}`}
                  type="number"
                  inputMode="decimal"
                  min={0.01}
                  max={settings.ceilings[key]}
                  step={0.01}
                  required
                  value={draft[key]}
                  onChange={(e) => setDraft((d) => ({ ...d, [key]: e.target.value }))}
                />
              </Field>
            ))}
            <Field
              label="Warning threshold (%)"
              htmlFor="spend-warnPercent"
              hint="Administrators get an alert when a budget reaches this share."
            >
              <Input
                id="spend-warnPercent"
                type="number"
                inputMode="numeric"
                min={50}
                max={99}
                step={1}
                required
                value={draft.warnPercent}
                onChange={(e) => setDraft((d) => ({ ...d, warnPercent: e.target.value }))}
              />
            </Field>
            <Field
              label="Metric refresh"
              htmlFor="spend-metricRefresh"
              hint="How often briefing metrics refresh on schedule. Each refresh is a model run. A person can still refresh a card by hand."
            >
              <NativeSelect
                id="spend-metricRefresh"
                value={metricRefresh}
                onChange={(e) => setMetricRefresh(e.target.value as MetricRefresh)}
              >
                {METRIC_REFRESH_OPTIONS.map(({ value, label }) => (
                  <option key={value} value={value}>{label}</option>
                ))}
              </NativeSelect>
            </Field>
          </div>
          <div className="flex justify-end">
            <Button type="submit" variant="primary" disabled={saving}>
              {saving ? "Saving…" : "Save limits"}
            </Button>
          </div>
        </form>

        <WorkflowBudgets settings={settings} onSaved={setSettings} />
      </div>

      <CreatorCredit />
    </>
  );
}

function SpendAlerts({ settings, onSaved }: { settings: SpendSettings; onSaved: (s: SpendSettings) => void }) {
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  if (settings.alerts.length === 0) return null;

  async function acknowledge(alertId: string) {
    setBusy(alertId);
    setError(null);
    const result = await adminApi<SpendSettings>(`/api/admin/spend/alerts/${alertId}`, "DELETE");
    setBusy(null);
    if (!result.ok) return setError(result.error);
    onSaved(result.body);
  }

  return (
    <section className="settings-card mb-6 flex flex-col gap-3" aria-label="Spend alerts">
      <h2 className="settings-card-title">Open alerts</h2>
      <AdminError message={error} />
      <ul className="flex flex-col gap-3">
        {settings.alerts.map((alert) => (
          <li key={alert.id} className="flex items-start justify-between gap-4 max-[720px]:flex-col">
            <div className="flex flex-col gap-1">
              <span className={`text-ui-body-sm ${alert.kind === "spend.budget_warning" ? "text-warn-ink" : "text-danger"}`}>
                {alert.kind === "spend.budget_warning"
                  ? "Near a budget"
                  : alert.kind === "spend.budget_blocked"
                    ? "Run blocked"
                    : "Unknown price"}
              </span>
              <p className="text-ui-body-sm text-text2">{alert.message}</p>
              <p className="text-ui-caption tabular-nums text-text3">{alert.createdAt.slice(0, 16).replace("T", " ")} UTC</p>
            </div>
            <Button
              type="button"
              size="sm"
              variant="secondary"
              disabled={busy === alert.id}
              onClick={() => void acknowledge(alert.id)}
            >
              Acknowledge
            </Button>
          </li>
        ))}
      </ul>
    </section>
  );
}

function toDraft(limits: SpendLimitsUsd): Record<keyof SpendLimitsUsd, string> {
  return Object.fromEntries(Object.entries(limits).map(([key, value]) => [key, String(value)])) as Record<
    keyof SpendLimitsUsd,
    string
  >;
}

function UsageCard({ title, usage, warnPercent }: { title: string; usage: SpendWindowUsage; warnPercent: number }) {
  const committed = usage.spentUsd + usage.heldUsd;
  const percent = usage.limitUsd > 0 ? Math.min(100, (committed / usage.limitUsd) * 100) : 100;
  const tone = percent >= 100 ? "danger" : percent >= warnPercent ? "watch" : "ok";
  const status = tone === "danger" ? "At limit" : tone === "watch" ? "Near limit" : "Within limit";
  return (
    <div className="settings-card flex flex-col gap-3">
      <div className="flex items-center justify-between gap-2">
        <h2 className="settings-card-title">{title}</h2>
        <span
          className={`text-ui-body-sm tabular-nums ${tone === "danger" ? "text-danger" : tone === "watch" ? "text-warn-ink" : "text-success-mid"}`}
        >
          {status} · {Math.round(percent)}%
        </span>
      </div>
      <p className="text-ui-body-sm tabular-nums text-text2">
        {usd(usage.spentUsd)} spent · {usd(usage.heldUsd)} held for running work · limit {usd(usage.limitUsd)}
      </p>
      <div
        className="relative h-2 overflow-hidden rounded-full bg-border"
        role="meter"
        aria-label={`${title} spend`}
        aria-valuemin={0}
        aria-valuemax={usage.limitUsd}
        aria-valuenow={committed}
      >
        <div
          className={tone === "danger" ? "h-full bg-danger" : tone === "watch" ? "h-full bg-watch" : "h-full bg-accent"}
          style={{ width: `${percent}%` }}
        />
        <div className="absolute top-0 h-full w-px bg-text3" style={{ left: `${warnPercent}%` }} aria-hidden="true" />
      </div>
      <p className="text-ui-caption text-text3">Resets at {utcTime(usage.resetsAt)}.</p>
    </div>
  );
}

function WorkflowBudgets({ settings, onSaved }: { settings: SpendSettings; onSaved: (s: SpendSettings) => void }) {
  const [error, setError] = useState<string | null>(null);
  return (
    <section className="settings-card mt-6 flex flex-col gap-4">
      <div>
        <h2 className="settings-card-title">Workflow budgets</h2>
        <p className="settings-card-copy">
          Leave a field empty to use the default of {usd(settings.limits.workflowHourlyUsd)} per hour and{" "}
          {usd(settings.limits.workflowDailyUsd)} per day.
        </p>
      </div>
      <AdminError message={error} />
      {settings.workflows.length === 0 ? (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>No workflows yet</EmptyTitle>
            <EmptyDescription>Workflows appear here once they are saved.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Workflow</TableHead>
              <TableHead>Spent today</TableHead>
              <TableHead>Hourly budget</TableHead>
              <TableHead>Daily budget</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {settings.workflows.map((row) => (
              <WorkflowBudgetRow
                key={`${row.workflowId}:${row.hourlyOverrideUsd}:${row.dailyOverrideUsd}`}
                row={row}
                settings={settings}
                onSaved={onSaved}
                onError={setError}
              />
            ))}
          </TableBody>
        </Table>
      )}
    </section>
  );
}

function WorkflowBudgetRow({
  row,
  settings,
  onSaved,
  onError,
}: {
  row: WorkflowSpendRow;
  settings: SpendSettings;
  onSaved: (s: SpendSettings) => void;
  onError: (message: string | null) => void;
}) {
  const [hourly, setHourly] = useState(row.hourlyOverrideUsd === null ? "" : String(row.hourlyOverrideUsd));
  const [daily, setDaily] = useState(row.dailyOverrideUsd === null ? "" : String(row.dailyOverrideUsd));
  const [busy, setBusy] = useState(false);
  const hasOverride = row.hourlyOverrideUsd !== null || row.dailyOverrideUsd !== null;

  async function submit(method: "PUT" | "DELETE") {
    setBusy(true);
    onError(null);
    const body =
      method === "PUT"
        ? { hourlyUsd: hourly.trim() ? Number(hourly) : null, dailyUsd: daily.trim() ? Number(daily) : null }
        : undefined;
    const result = await adminApi<SpendSettings>(`/api/admin/spend/workflows/${row.workflowId}`, method, body);
    setBusy(false);
    if (!result.ok) return onError(`${row.name}: ${result.error}`);
    onSaved(result.body);
    toast.success(method === "PUT" ? `Budget saved for ${row.name}.` : `${row.name} uses the default budgets.`);
  }

  return (
    <TableRow>
      <TableCell>
        <span className="font-medium">{row.name}</span>
        {row.enabled ? null : <span className="ml-2 text-ui-caption text-text3">disabled</span>}
      </TableCell>
      <TableCell className="tabular-nums">
        {usd(row.spentTodayUsd)} of {usd(row.dailyLimitUsd)}
      </TableCell>
      <TableCell>
        <Input
          aria-label={`${row.name} hourly budget`}
          type="number"
          inputMode="decimal"
          min={0.01}
          max={settings.ceilings.workflowHourlyUsd}
          step={0.01}
          placeholder={String(settings.limits.workflowHourlyUsd)}
          value={hourly}
          onChange={(e) => setHourly(e.target.value)}
        />
      </TableCell>
      <TableCell>
        <Input
          aria-label={`${row.name} daily budget`}
          type="number"
          inputMode="decimal"
          min={0.01}
          max={settings.ceilings.workflowDailyUsd}
          step={0.01}
          placeholder={String(settings.limits.workflowDailyUsd)}
          value={daily}
          onChange={(e) => setDaily(e.target.value)}
        />
      </TableCell>
      <TableCell className="text-right">
        <div className="flex justify-end gap-2">
          <Button type="button" size="sm" disabled={busy} onClick={() => void submit("PUT")}>
            Save
          </Button>
          {hasOverride ? (
            <Button type="button" size="sm" variant="ghost" disabled={busy} onClick={() => void submit("DELETE")}>
              Use default
            </Button>
          ) : null}
        </div>
      </TableCell>
    </TableRow>
  );
}
