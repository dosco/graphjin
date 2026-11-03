import React from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { Activity, MessageSquare, Pause, Play, Plus, Trash2 } from "lucide-react";

import { listProfiles } from "../../services/agent";
import { deleteWatch, previewQuery, saveMetricWatch, setWatchEnabled } from "../../services/mission";
import { parseJSON, relativeTime } from "../../services/graphql";
import { DataErrorState } from "../../components/ui";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

// Metric watches check a number from an aggregate query on a schedule and fire
// when it crosses a threshold. The database computes the number; these
// components show and edit the definition and the state the server keeps.

const OP_LABELS = {
  gt: ">",
  gte: "≥",
  lt: "<",
  lte: "≤",
  eq: "=",
  ne: "≠",
  changed: "changes",
  change_pct: "moves by ≥",
};

const CADENCES = [
  { label: "Every 5 minutes", cron: "*/5 * * * *" },
  { label: "Every 15 minutes", cron: "*/15 * * * *" },
  { label: "Every hour", cron: "0 * * * *" },
  { label: "Every day at 9:00", cron: "0 9 * * *" },
  { label: "Weekdays at 9:00", cron: "0 9 * * 1-5" },
];

export function isMetricWatch(watch) {
  return Boolean(metricOf(watch));
}

function metricOf(watch) {
  const metric = typeof watch?.metric === "string" ? parseJSON(watch.metric) : watch?.metric;
  return metric && metric.path ? metric : null;
}

function stateOf(watch) {
  const state = typeof watch?.metric_state === "string" ? parseJSON(watch.metric_state) : watch?.metric_state;
  return state || {};
}

function scheduleOf(watch) {
  return (typeof watch?.schedule === "string" ? parseJSON(watch.schedule) : watch?.schedule) || {};
}

function deliveryOf(watch) {
  return (typeof watch?.delivery_json === "string" ? parseJSON(watch.delivery_json) : watch?.delivery_json) || {};
}

export function conditionLabel(metric) {
  if (!metric) {
    return "";
  }
  const subject = metric.key ? `${metric.value} per ${metric.key}` : metric.path.split(".").pop();
  const op = OP_LABELS[metric.op] || metric.op;
  if (metric.op === "changed") {
    return `${subject} changes`;
  }
  return `${subject} ${op} ${metric.threshold}${metric.op === "change_pct" ? "%" : ""}`;
}

export function cadenceLabel(cron) {
  return CADENCES.find((item) => item.cron === cron)?.label || (cron ? `cron ${cron}` : "");
}

function formatNumber(value) {
  if (typeof value === "number") {
    return Number.isInteger(value) ? value.toLocaleString() : value.toLocaleString(undefined, { maximumFractionDigits: 2 });
  }
  if (value === undefined || value === null) {
    return "—";
  }
  return String(value);
}

// metricEventSummary turns a fired metric event into one readable line.
export function metricEventSummary(data, watch) {
  if (!data || data.kind !== "metric") {
    return "";
  }
  const metric = data.metric || metricOf(watch);
  if (Array.isArray(data.crossings)) {
    const keys = data.crossings.map((c) => `${c.key} ${formatNumber(c.value)}`).join(", ");
    return `${keys} · ${conditionLabel(metric)}`;
  }
  const previous = data.previous !== undefined ? ` (was ${formatNumber(data.previous)})` : "";
  return `${formatNumber(data.value)}${previous} · ${conditionLabel(metric)}`;
}

function Sparkline({ points, threshold, className }) {
  const values = (points || []).map((p) => p.value).filter((v) => typeof v === "number");
  if (values.length < 2) {
    return <div className={cn("h-10 text-xs text-muted-foreground", className)}>History starts with the next checks.</div>;
  }
  const hasThreshold = typeof threshold === "number";
  const all = hasThreshold ? [...values, threshold] : values;
  const min = Math.min(...all);
  const max = Math.max(...all);
  const span = max - min || 1;
  const w = 160;
  const h = 40;
  const x = (i) => (i / (values.length - 1)) * w;
  const y = (v) => h - 2 - ((v - min) / span) * (h - 4);
  const line = values.map((v, i) => `${i ? "L" : "M"}${x(i).toFixed(1)},${y(v).toFixed(1)}`).join(" ");
  return (
    <svg viewBox={`0 0 ${w} ${h}`} className={cn("h-10 w-full", className)} preserveAspectRatio="none" role="img" aria-label="Recent readings">
      {hasThreshold && (
        <line x1="0" x2={w} y1={y(threshold)} y2={y(threshold)} className="stroke-amber-500/70" strokeDasharray="3 3" strokeWidth="1" />
      )}
      <path d={line} fill="none" className="stroke-primary" strokeWidth="1.5" vectorEffect="non-scaling-stroke" />
      <circle cx={x(values.length - 1)} cy={y(values[values.length - 1])} r="2.5" className="fill-primary" />
    </svg>
  );
}

function MetricWatchCard({ watch, profiles, onEdit, onChanged }) {
  const metric = metricOf(watch);
  const state = stateOf(watch);
  const schedule = scheduleOf(watch);
  const delivery = deliveryOf(watch);
  const toggle = useMutation({ mutationFn: () => setWatchEnabled(watch.id, !watch.enabled), onSuccess: onChanged });
  const remove = useMutation({ mutationFn: () => deleteWatch(watch.id), onSuccess: onChanged });
  const breached = Boolean(state.breached);
  const groups = state.groups ? Object.entries(state.groups) : [];

  return (
    <article className="flex min-w-0 flex-col gap-3 rounded-xl border bg-card p-4">
      <header className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <button type="button" className="block max-w-full truncate text-left text-sm font-semibold hover:underline" onClick={() => onEdit(watch)}>
            {watch.name}
          </button>
          <p className="mt-0.5 text-xs text-muted-foreground">
            {conditionLabel(metric)} · {cadenceLabel(schedule.cron)}
          </p>
        </div>
        {!watch.enabled ? (
          <Badge variant="muted">Paused</Badge>
        ) : breached ? (
          <Badge variant="warning">Over threshold</Badge>
        ) : (
          <Badge variant="success">OK</Badge>
        )}
      </header>

      {groups.length ? (
        <div className="grid gap-1 text-sm">
          {groups.slice(0, 6).map(([key, value]) => (
            <div key={key} className="flex items-center justify-between gap-3">
              <span className="truncate text-muted-foreground">{key}</span>
              <span className={cn("font-mono tabular-nums", value.breached && "font-semibold text-amber-700 dark:text-amber-400")}>{formatNumber(value.value)}</span>
            </div>
          ))}
          {groups.length > 6 && <span className="text-xs text-muted-foreground">+{groups.length - 6} more groups</span>}
        </div>
      ) : (
        <div className="flex items-baseline gap-2">
          <span className={cn("font-mono text-3xl font-semibold tabular-nums", breached && "text-amber-700 dark:text-amber-400")}>{formatNumber(state.last_value)}</span>
          {metric?.op !== "changed" && <span className="text-xs text-muted-foreground">threshold {formatNumber(metric?.threshold)}</span>}
        </div>
      )}

      <Sparkline points={state.history} threshold={groups.length ? undefined : typeof metric?.threshold === "number" ? metric.threshold : undefined} />

      {(state.last_error || watch.last_error) && (
        <p className="truncate text-xs text-red-700 dark:text-red-400" title={state.last_error || watch.last_error}>{state.last_error || watch.last_error}</p>
      )}

      <footer className="flex flex-wrap items-center justify-between gap-2 border-t pt-3 text-xs text-muted-foreground">
        <span>{state.last_checked_at ? `Checked ${relativeTime(state.last_checked_at)}` : "Not checked yet"}</span>
        <div className="flex items-center gap-1">
          {delivery.kind === "agent" && watch.last_fired_at && (
            <Button asChild type="button" size="sm" variant="ghost" title="Open the thread the agent answers in">
              <Link to={`/user/agent?thread=${encodeURIComponent(watch.routine_thread_id || delivery.thread_id || "")}`}>
                <MessageSquare aria-hidden="true" />
                {profiles.find((p) => p.id === delivery.profile)?.name || "Agent"}
              </Link>
            </Button>
          )}
          <Button type="button" size="sm" variant="ghost" disabled={toggle.isPending} onClick={() => toggle.mutate()}>
            {watch.enabled ? <Pause aria-hidden="true" /> : <Play aria-hidden="true" />}
            {watch.enabled ? "Pause" : "Resume"}
          </Button>
          <Button
            type="button"
            size="sm"
            variant="ghost"
            disabled={remove.isPending}
            onClick={() => window.confirm(`Delete the watch "${watch.name}"?`) && remove.mutate()}
          >
            <Trash2 aria-hidden="true" />
            <span className="sr-only">Delete</span>
          </Button>
        </div>
      </footer>
      {(toggle.error || remove.error) && <DataErrorState error={toggle.error || remove.error} queryMessage="GraphJin did not change this watch." />}
    </article>
  );
}

export function MetricWatchGrid({ watches, identityKey }) {
  const queryClient = useQueryClient();
  const [editing, setEditing] = React.useState(null);
  const refresh = () => queryClient.invalidateQueries({ queryKey: ["mission", "watches", identityKey] });
  const metricWatches = watches.filter(isMetricWatch);
  // Agents are optional: without threads or a signed-in user the list is empty.
  const profilesQuery = useQuery({ queryKey: ["agent-profiles", identityKey], queryFn: listProfiles, retry: false });
  const profiles = profilesQuery.data || [];

  return (
    <div className="grid gap-3">
      <div className="flex items-center justify-between gap-3">
        <div>
          <h3 className="text-sm font-semibold">Metric watches</h3>
          <p className="text-sm text-muted-foreground">Numbers from aggregate queries, checked on a schedule.</p>
        </div>
        <Button type="button" size="sm" onClick={() => setEditing({})}>
          <Plus aria-hidden="true" />
          New metric watch
        </Button>
      </div>
      {metricWatches.length === 0 ? (
        <div className="flex items-center gap-3 rounded-xl border border-dashed p-4 text-sm text-muted-foreground">
          <Activity aria-hidden="true" className="size-4 shrink-0" />
          <span>Watch a count, sum or average and get told when it crosses a line. You can also ask the agent: "tell me when open tickets go above 20".</span>
        </div>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {metricWatches.map((watch) => (
            <MetricWatchCard key={watch.id} watch={watch} profiles={profiles} onEdit={setEditing} onChanged={refresh} />
          ))}
        </div>
      )}
      <MetricWatchEditor watch={editing} profiles={profiles} onClose={() => setEditing(null)} onSaved={() => { setEditing(null); refresh(); }} />
    </div>
  );
}

// numericPaths lists every number in a query result as a metric path, plus
// lists of rows that have both a text and a number column (grouped metrics).
export function numericPaths(data, prefix = "", out = []) {
  if (Array.isArray(data)) {
    data.slice(0, 1).forEach((item, i) => numericPaths(item, prefix ? `${prefix}.${i}` : String(i), out));
    if (data.length > 1 && data[0] && typeof data[0] === "object" && !Array.isArray(data[0])) {
      const keys = Object.entries(data[0]);
      const group = keys.find(([, v]) => typeof v === "string");
      const nums = keys.filter(([, v]) => typeof v === "number");
      nums.forEach(([value]) => out.push({ path: prefix, key: group?.[0], value, sample: `${data.length} groups` }));
    }
    return out;
  }
  if (data && typeof data === "object") {
    for (const [key, value] of Object.entries(data)) {
      numericPaths(value, prefix ? `${prefix}.${key}` : key, out);
    }
    return out;
  }
  if (typeof data === "number" || (typeof data === "string" && data.trim() !== "" && !Number.isNaN(Number(data)))) {
    out.push({ path: prefix, sample: data });
  }
  return out;
}

const blankForm = {
  name: "",
  query: "",
  path: "",
  key: "",
  value: "",
  op: "gt",
  threshold: "",
  cron: "*/15 * * * *",
  timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
  prompt: "",
  profile: "",
};

function formFromWatch(watch) {
  if (!watch || !watch.id) {
    return { ...blankForm };
  }
  const metric = metricOf(watch) || {};
  const schedule = scheduleOf(watch);
  const delivery = deliveryOf(watch);
  return {
    id: watch.id,
    name: watch.name,
    query: watch.query || "",
    path: metric.path || "",
    key: metric.key || "",
    value: metric.value || "",
    op: metric.op || "gt",
    threshold: metric.threshold ?? "",
    cron: schedule.cron || blankForm.cron,
    timezone: schedule.timezone || blankForm.timezone,
    prompt: delivery.kind === "agent" ? delivery.prompt || "" : "",
    profile: delivery.kind === "agent" ? delivery.profile || "" : "",
  };
}

function MetricWatchEditor({ watch, profiles, onClose, onSaved }) {
  const [form, setForm] = React.useState(blankForm);
  const [preview, setPreview] = React.useState(null);
  React.useEffect(() => {
    setForm(formFromWatch(watch));
    setPreview(null);
  }, [watch]);
  const set = (field) => (event) => setForm((prev) => ({ ...prev, [field]: event.target.value }));

  const run = useMutation({ mutationFn: () => previewQuery(form.query), onSuccess: setPreview });
  const save = useMutation({
    mutationFn: () => {
      const metric = { path: form.path.trim(), op: form.op };
      if (form.key) {
        metric.key = form.key;
        metric.value = form.value;
      }
      if (form.op !== "changed") {
        const n = Number(form.threshold);
        metric.threshold = form.op === "eq" || form.op === "ne" ? (Number.isNaN(n) ? form.threshold : n) : n;
      }
      return saveMetricWatch({
        id: form.id,
        name: form.name.trim(),
        query: form.query.trim(),
        schedule: { cron: form.cron.trim(), timezone: form.timezone.trim() || "UTC" },
        metric,
        prompt: form.prompt,
        profile: form.profile,
      });
    },
    onSuccess: onSaved,
  });
  const candidates = preview ? numericPaths(preview) : [];
  const canSave = form.name.trim() && form.query.trim() && form.path.trim() && (form.op === "changed" || String(form.threshold).trim() !== "");
  const cadencePreset = CADENCES.some((c) => c.cron === form.cron) ? form.cron : "custom";

  return (
    <Sheet open={Boolean(watch)} onOpenChange={(open) => !open && onClose()}>
      <SheetContent className="w-full overflow-y-auto sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>{form.id ? "Edit metric watch" : "New metric watch"}</SheetTitle>
          <SheetDescription>
            GraphJin runs the query on the schedule and fires once each time the number crosses the threshold.
          </SheetDescription>
        </SheetHeader>
        <form
          className="mt-5 grid gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            if (canSave) {
              save.mutate();
            }
          }}
        >
          <Field label="Name">
            <Input value={form.name} onChange={set("name")} placeholder="open_tickets_over_20" disabled={Boolean(form.id)} required />
          </Field>
          <Field label="Query" hint="A plain GraphQL query with an aggregate, e.g. query { customer_tickets(where: { status: { eq: &quot;open&quot; } }) { count_id } }">
            <Textarea value={form.query} onChange={set("query")} rows={4} className="font-mono text-xs" placeholder={'query { orders { count_id } }'} required />
            <div className="mt-2 flex items-center gap-2">
              <Button type="button" size="sm" variant="outline" disabled={!form.query.trim() || run.isPending} onClick={() => run.mutate()}>
                Run query
              </Button>
              {run.error && <span className="truncate text-xs text-red-700">{run.error.message}</span>}
            </div>
          </Field>
          {candidates.length > 0 && (
            <div className="grid gap-1">
              <span className="text-xs font-medium text-muted-foreground">Pick the number to watch</span>
              {candidates.map((c) => (
                <button
                  key={`${c.path}:${c.value || ""}`}
                  type="button"
                  className={cn(
                    "flex items-center justify-between gap-3 rounded-lg border px-3 py-1.5 text-left text-sm hover:bg-muted/50",
                    form.path === c.path && (form.value || "") === (c.value || "") && "border-primary bg-primary/5"
                  )}
                  onClick={() => setForm((prev) => ({ ...prev, path: c.path, key: c.key || "", value: c.value || "" }))}
                >
                  <span className="truncate font-mono text-xs">{c.value ? `${c.path} · ${c.value} per ${c.key}` : c.path}</span>
                  <span className="font-mono text-xs text-muted-foreground">{String(c.sample)}</span>
                </button>
              ))}
            </div>
          )}
          <Field label="Value path" hint="Dot path to the number in the result.">
            <Input value={form.path} onChange={set("path")} className="font-mono text-xs" placeholder="orders.0.count_id" required />
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Condition">
              <Select value={form.op} onChange={set("op")}>
                <option value="gt">is above</option>
                <option value="gte">is at or above</option>
                <option value="lt">is below</option>
                <option value="lte">is at or below</option>
                <option value="eq">equals</option>
                <option value="ne">is not</option>
                <option value="change_pct">moves by percent</option>
                <option value="changed">changes at all</option>
              </Select>
            </Field>
            {form.op !== "changed" && (
              <Field label={form.op === "change_pct" ? "Percent" : "Threshold"}>
                <Input value={form.threshold} onChange={set("threshold")} inputMode="decimal" required />
              </Field>
            )}
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Check">
              <Select
                value={cadencePreset}
                onChange={(event) => event.target.value !== "custom" && setForm((prev) => ({ ...prev, cron: event.target.value }))}
              >
                {CADENCES.map((c) => (
                  <option key={c.cron} value={c.cron}>{c.label}</option>
                ))}
                <option value="custom">Custom cron</option>
              </Select>
            </Field>
            <Field label="Time zone">
              <Input value={form.timezone} onChange={set("timezone")} />
            </Field>
          </div>
          {cadencePreset === "custom" && (
            <Field label="Cron" hint="Five fields: minute hour day-of-month month day-of-week.">
              <Input value={form.cron} onChange={set("cron")} className="font-mono text-xs" />
            </Field>
          )}
          <Field label="When it fires, ask the agent (optional)" hint="Leave both empty to only notify the inbox. The agent answers read-only.">
            <Textarea value={form.prompt} onChange={set("prompt")} rows={3} placeholder="Which customers are behind the spike?" />
          </Field>
          {profiles.length > 0 && (
            <Field label="Answer in">
              <Select value={form.profile} onChange={set("profile")}>
                <option value="">This watch's own thread</option>
                {profiles.map((p) => (
                  <option key={p.id} value={p.id}>{p.name}'s chat</option>
                ))}
              </Select>
            </Field>
          )}
          {save.error && <DataErrorState error={save.error} queryMessage="GraphJin did not save this watch." />}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="ghost" onClick={onClose}>Cancel</Button>
            <Button type="submit" disabled={!canSave || save.isPending}>{save.isPending ? "Saving" : "Save watch"}</Button>
          </div>
        </form>
      </SheetContent>
    </Sheet>
  );
}

function Field({ label, hint, children }) {
  return (
    <label className="grid gap-1.5 text-sm">
      <span className="font-medium">{label}</span>
      {children}
      {hint && <span className="text-xs text-muted-foreground">{hint}</span>}
    </label>
  );
}
