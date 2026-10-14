"use client";

/**
 * OpenNeko Component Registrations
 *
 * Registers React renderers for each A2UI component type
 * in the OpenNeko catalog. Import this module once at app startup
 * to populate the registry.
 */

import React from "react";
import { ChevronRight, MessageSquare } from "lucide-react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Button } from "@/components/ui/button";
import { CheckboxControl } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/ui/native-select";
import { Separator } from "@/components/ui/separator";
import { Segment, SegmentedControl, Tab, Tabs } from "@/components/ui/tabs";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import {
  WORKSPACE_MARKDOWN_COMPONENTS,
  linkifyWorkspacePaths,
} from "@/lib/linkify-workspace-paths";
import { registerComponent, renderChildren } from "./renderer";
import { bodyChildIds, resolveComponent } from "./surface";
import type { RenderContext } from "./renderer";
import type { A2UIComponent } from "./types";
import type {
  AnswerProps,
  BriefingCardProps,
  BriefingProps,
  CalloutProps,
  ChartProps,
  ChoiceProps,
  ConfirmationProps,
  DividerProps,
  KeyFiguresProps,
  MarkdownProps,
  SectionProps,
  TableColumn,
  TableProps,
  ButtonProps,
  CheckBoxProps,
  ChoicePickerProps,
  ConditionalProps,
  LayoutProps,
  ManagedFileSourceInputProps,
  OpenApiSpecInputProps,
  TabsProps,
  TextFieldProps,
  TextProps,
} from "./catalog";
import BriefingCard from "@/components/BriefingCard";
import Chart from "@/components/Chart";
import { displayFigure } from "./answer-format";
import { formatCompact } from "@/lib/format-number";
import { AskCard, AskChip } from "./ask";
import { drillInto, drillPrompt } from "./drill";
import "./answer-blocks";
import "./answer-diagram";
import "./answer-map";
import "./answer-tool";

// ─── Answer / Briefing root ───
// The card frame for a conversational answer (Answer) and the dashboard's daily
// briefing (Briefing). Thread-scale header — the dashboard's hero classes
// (.greet, 52px) would overwhelm an answer, so this mirrors the eyebrow +
// display-title language at card proportions. `eyebrow` is optional: the
// dashboard passes "Briefing", an Answer passes its own kicker or none.
function surfaceFrame(
  opts: {
    id: string;
    eyebrow?: string;
    title?: string;
    subtitle?: string;
    draft?: boolean;
    childIds: string[];
  },
  ctx: RenderContext,
) {
  return (
    <div key={opts.id} className="work-surface" data-stage={opts.draft ? "draft" : undefined}>
      {opts.draft ? (
        <div className="answer-draft-note" role="status">
          <span className="answer-draft-pulse" aria-hidden="true" />
          Early figures · OpenNeko is still checking
        </div>
      ) : null}
      {opts.eyebrow ? (
        <div
          className="work-surface-eyebrow"
          style={{ animation: "fadeUp 0.5s ease both" }}
        >
          <span className="work-surface-eyebrow-rule" aria-hidden="true" />
          {opts.eyebrow}
        </div>
      ) : null}
      {opts.title ? (
        <div
          className="work-surface-title"
          style={{ animation: "fadeUp 0.5s ease 0.04s both" }}
        >
          {opts.title}
        </div>
      ) : null}
      {opts.subtitle ? (
        <div
          className="work-surface-sub"
          style={{ animation: "fadeUp 0.5s ease 0.08s both" }}
        >
          {opts.subtitle}
        </div>
      ) : null}
      {opts.childIds.length > 0 ? renderChildren(opts.childIds, ctx) : null}
    </div>
  );
}

registerComponent("Answer", (comp: A2UIComponent, ctx: RenderContext) => {
  const props = comp as unknown as AnswerProps & { id: string };
  return surfaceFrame(
    {
      id: props.id,
      eyebrow: props.eyebrow,
      title: props.title,
      subtitle: props.subtitle,
      draft: props.stage === "draft",
      childIds: bodyChildIds(ctx.surface, comp),
    },
    ctx,
  );
});

registerComponent("Briefing", (comp: A2UIComponent, ctx: RenderContext) => {
  const props = comp as unknown as BriefingProps & { id: string };
  return surfaceFrame(
    {
      id: props.id,
      eyebrow: "Briefing",
      title: props.greeting,
      subtitle: props.subtitle,
      childIds: bodyChildIds(ctx.surface, comp),
    },
    ctx,
  );
});

// ─── Confirmation ───
registerComponent("Confirmation", (comp: A2UIComponent, ctx: RenderContext) => {
  const props = comp as unknown as ConfirmationProps & { id: string };
  return (
    <div
      key={props.id}
      className="work-confirm"
      style={{ animation: "fadeUp 0.4s ease both" }}
    >
      <div className="work-confirm-head">
        <span className="work-confirm-check" aria-hidden="true">
          <svg
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="3"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <path d="M5 13l4 4L19 7" />
          </svg>
        </span>
        <span className="work-confirm-label">{props.label}</span>
      </div>
      {props.title ? (
        <div className="work-confirm-title">{props.title}</div>
      ) : null}
      {props.children ? (
        <div className="work-confirm-body">
          {renderChildren(props.children, ctx)}
        </div>
      ) : null}
    </div>
  );
});

// ─── Markdown ───
registerComponent("Markdown", (comp: A2UIComponent) => {
  const props = comp as unknown as MarkdownProps & { id: string };
  return (
    <div key={props.id} className="work-markdown">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={WORKSPACE_MARKDOWN_COMPONENTS}
      >
        {linkifyWorkspacePaths(props.text)}
      </ReactMarkdown>
    </div>
  );
});

// ─── Key figures ───
// A cardless evidence strip owned by one answer. It is intentionally quieter
// than the dashboard KPI cards: ruled columns, explicit provenance, no mood
// labels that pretend a number is a recommendation.
registerComponent("KeyFigures", (comp: A2UIComponent) => {
  const props = comp as unknown as KeyFiguresProps & { id: string };
  const items = Array.isArray(props.items) ? props.items : [];
  if (items.length === 0) return null;
  return (
    <section
      key={props.id}
      className="work-key-figures"
      aria-label="Key figures"
    >
      <div className="work-key-figures-label">Key figures</div>
      <dl className="work-key-figures-grid">
        {items.map((item, index) => {
          const provenance = [item.basis, item.asOf, item.source].filter(
            (value): value is string => Boolean(value),
          );
          return (
            <div className="work-key-figure" key={`${item.label}-${index}`}>
              <dt>{item.label}</dt>
              <KeyFigureValue value={item.value} />
              {item.sub ? (
                <div className="work-key-figure-sub">{item.sub}</div>
              ) : null}
              {provenance.length > 0 ? (
                <div className="work-key-figure-proof">
                  {provenance.join(" · ")}
                </div>
              ) : null}
            </div>
          );
        })}
      </dl>
    </section>
  );
});

function KeyFigureValue({ value }: { value: string }) {
  const { display, exact } = displayFigure(value);
  if (!exact) return <dd>{value}</dd>;
  return (
    <dd title={exact}>
      <span aria-hidden="true">{display}</span>
      <span className="sr-only">{exact}</span>
    </dd>
  );
}

// ─── MetricCard / BriefingCard ───
// One KPI card, two names: MetricCard in a work/Ask Answer, BriefingCard on the
// dashboard (and in already-stored surfaces). Same renderer.
const renderMetricCard = (comp: A2UIComponent, ctx: RenderContext) => {
  const props = comp as unknown as BriefingCardProps & { id: string };
  const extras = ctx.extras as
    | {
        onDismiss?: (id: string) => void;
        indexMap?: Map<string, number>;
      }
    | undefined;

  const index = extras?.indexMap?.get(props.id) ?? 0;

  const insight = {
    id: props.id,
    metricId: props.metricId,
    source: props.source,
    mood: props.mood,
    text: props.text,
    metric: props.metric,
    label: props.label,
    detail: props.detail,
    chart: props.chartType,
    chartData: props.chartData,
  };

  return (
    <BriefingCard
      key={props.id}
      ins={insight}
      index={index}
      onDismiss={
        extras?.onDismiss ? () => extras.onDismiss?.(props.id) : undefined
      }
    />
  );
};
registerComponent("MetricCard", renderMetricCard);
registerComponent("BriefingCard", renderMetricCard);

// An answer chart uses the Briefing visualization without the saved-metric
// lifecycle, mood, or card actions. Exact values remain available to readers.
registerComponent("Chart", (comp: A2UIComponent, ctx: RenderContext) => (
  <AnswerChart key={comp.id} props={comp as unknown as ChartProps & { id: string }} ctx={ctx} />
));

function AnswerChart({ props, ctx }: { props: ChartProps & { id: string }; ctx: RenderContext }) {
  const [picked, setPicked] = React.useState<string | null>(null);
  const drill = typeof props.drill === "string" && props.drill.includes("{label}") ? props.drill : undefined;
  const data = Array.isArray(props.data) ? props.data : [];
  const validPoints = data.every((point) =>
    point && typeof point.d === "string" && point.d.trim() &&
    typeof point.v === "number" && Number.isFinite(point.v) &&
    (point.t === undefined || (typeof point.t === "number" && Number.isFinite(point.t))),
  );
  const validDonut = props.type !== "donut" ||
    (validPoints && data.length <= 8 && data.every((point) => point.v >= 0) &&
      data.reduce((sum, point) => sum + point.v, 0) > 0);
  if (typeof props.title !== "string" || !props.title.trim() ||
    typeof props.valueLabel !== "string" || !props.valueLabel.trim() ||
    !["line", "bar", "area", "donut"].includes(props.type) ||
    data.length < 2 || data.length > 60 || !validPoints || !validDonut) {
    return null;
  }
  const hasBaseline = data.some((point) => point.t !== undefined);
  return (
    <figure className="work-chart" aria-label={props.title}>
      <figcaption className="work-chart-title">{props.title}</figcaption>
      <Chart
        type={props.type}
        data={data}
        h={220}
        valueLabel={props.valueLabel}
        baselineLabel={props.baselineLabel}
        maxBarSize={44}
        onSelect={drill ? setPicked : undefined}
      />
      {picked ? (
        <AskCard
          ctx={ctx}
          componentId={props.id}
          template={drill}
          label={picked}
          value={chartValue(data.find((point) => point.d === picked)?.v, props.valueLabel)}
        />
      ) : null}
      <table className="sr-only">
        <caption>{props.title} values</caption>
        <thead>
          <tr>
            <th scope="col">Category</th>
            <th scope="col">{props.valueLabel}</th>
            {hasBaseline ? <th scope="col">{props.baselineLabel ?? "Prior"}</th> : null}
          </tr>
        </thead>
        <tbody>
          {data.map((point, index) => (
            <tr key={`${point.d}-${index}`}>
              <th scope="row">{point.d}</th>
              <td>{point.v}</td>
              {hasBaseline ? <td>{point.t ?? ""}</td> : null}
            </tr>
          ))}
        </tbody>
      </table>
      {props.source || props.asOf ? (
        <p className="work-chart-source">
          {[
            props.source ? `Source: ${props.source}` : null,
            props.asOf ? `As of ${props.asOf}` : null,
          ].filter(Boolean).join(" · ")}
        </p>
      ) : null}
    </figure>
  );
}

function chartValue(value: number | undefined, label: string): string | undefined {
  return value === undefined ? undefined : `${label}: ${formatCompact(value)}`;
}

// ─── Table ───
// Structured tabular data. Tables used to only exist as Markdown GFM, which
// can't carry alignment or survive a non-web channel; this is a first-class
// component the agent addresses by columns + rows.
function DataTable({ columns, rows, className }: { columns: TableColumn[]; rows: Record<string, string | number>[]; className: string }) {
  return (
    <Table className={className}>
      <TableHeader>
        <TableRow>
          {columns.map((col) => (
            <TableHead key={col.key} style={col.align ? { textAlign: col.align } : undefined}>{col.label}</TableHead>
          ))}
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((row, r) => (
          <TableRow key={r}>
            {columns.map((col) => (
              <TableCell key={col.key} style={col.align ? { textAlign: col.align } : undefined}>{String(row[col.key] ?? "")}</TableCell>
            ))}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function TableBlock({ props, ctx }: { props: TableProps & { id: string }; ctx: RenderContext }) {
  const columns = Array.isArray(props.columns) ? props.columns : [];
  const rows = Array.isArray(props.rows) ? props.rows : [];
  const details = props.details && typeof props.details === "object" ? props.details : {};
  const firstKey = columns[0]?.key;
  const [open, setOpen] = React.useState<Set<string>>(() => new Set());
  const hasOpenable = rows.some((row) => {
    const name = firstKey ? String(row[firstKey] ?? "") : "";
    return Boolean(name && (details[name] || drillPrompt(props.drill, name)));
  });
  const toggle = (name: string) => setOpen((current) => {
    const next = new Set(current);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    return next;
  });
  return (
    <div className="work-table-wrap" data-columns={columns.length}>
      {props.caption ? (
        <div className="work-table-caption">{props.caption}</div>
      ) : null}
      <Table className="work-table">
        <TableHeader>
          <TableRow>
            {columns.map((col) => (
              <TableHead
                key={col.key}
                style={col.align ? { textAlign: col.align } : undefined}
              >
                {col.label}
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row, r) => {
            const name = firstKey ? String(row[firstKey] ?? "") : "";
            const drill = name ? drillPrompt(props.drill, name) : null;
            const detail = name ? details[name] : undefined;
            const openable = Boolean(detail || drill);
            const expanded = openable && open.has(name);
            const detailId = `${props.id}-detail-${r}`;
            return (
              <React.Fragment key={r}>
                <TableRow data-expanded={openable ? String(expanded) : undefined} data-indent={!openable && hasOpenable ? "" : undefined}>
                  {columns.map((col, c) => (
                    <TableCell
                      key={col.key}
                      style={col.align ? { textAlign: col.align } : undefined}
                    >
                      {c === 0 && openable ? (
                        <span className="work-table-name">
                          <Button
                            variant="ghost"
                            className="work-table-toggle"
                            aria-expanded={expanded}
                            aria-controls={detailId}
                            onClick={() => toggle(name)}
                          >
                            <span className="work-table-chevron" aria-hidden="true">
                              <ChevronRight strokeWidth={2} />
                            </span>
                            {name}
                          </Button>
                          {drill ? (
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              className="work-table-ask"
                              aria-label={drill}
                              title={drill}
                              onClick={() => drillInto(ctx, props.id, props.drill, name)}
                            >
                              <MessageSquare aria-hidden="true" strokeWidth={2.2} />
                            </Button>
                          ) : null}
                        </span>
                      ) : (
                        String(row[col.key] ?? "")
                      )}
                    </TableCell>
                  ))}
                </TableRow>
                {expanded ? (
                  <TableRow className="work-table-detail-row" id={detailId}>
                    <TableCell colSpan={columns.length}>
                      {detail ? <DataTable columns={detail.columns ?? []} rows={detail.rows ?? []} className="work-table work-subtable" /> : null}
                      <AskChip ctx={ctx} componentId={props.id} template={props.drill} label={name} className="work-table-detail-ask" />
                    </TableCell>
                  </TableRow>
                ) : null}
              </React.Fragment>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}

registerComponent("Table", (comp: A2UIComponent, ctx: RenderContext) => (
  <TableBlock key={comp.id} props={comp as unknown as TableProps & { id: string }} ctx={ctx} />
));

// ─── Section ───
// A titled group of components. `collapsible` renders a native <details data-ui-bespoke-reason="agent-rendered A2UI catalog"> so
// long answers can fold their supporting detail without any client state.
registerComponent("Section", (comp: A2UIComponent, ctx: RenderContext) => {
  const props = comp as unknown as SectionProps & { id: string };
  const childIds = Array.isArray(props.children) ? props.children : [];
  const body = (
    <div className="work-section-body">{renderChildren(childIds, ctx)}</div>
  );
  if (props.collapsible) {
    return (
      <details
        data-ui-bespoke-reason="agent-rendered A2UI catalog"
        key={props.id}
        className="work-section is-collapsible"
        open={props.defaultOpen !== false}
      >
        <summary
          data-ui-bespoke-reason="agent-rendered A2UI catalog"
          className="work-section-summary"
        >
          {props.title}
        </summary>
        {body}
      </details>
    );
  }
  return (
    <div key={props.id} className="work-section">
      <div className="work-section-title">{props.title}</div>
      {body}
    </div>
  );
});

// ─── Callout ───
// Mood-tinted prose block (markdown body). Distinct from BriefingCard: it
// carries an argument, not a KPI tile.
registerComponent("Callout", (comp: A2UIComponent) => {
  const props = comp as unknown as CalloutProps & { id: string };
  const mood = props.mood ?? "watch";
  return (
    <div key={props.id} className={`work-callout work-callout-${mood}`}>
      {props.title ? (
        <div className="work-callout-title">{props.title}</div>
      ) : null}
      <div className="work-callout-body work-markdown">
        <ReactMarkdown
          remarkPlugins={[remarkGfm]}
          components={WORKSPACE_MARKDOWN_COMPONENTS}
        >
          {linkifyWorkspacePaths(props.text)}
        </ReactMarkdown>
      </div>
    </div>
  );
});

// ─── Choice ───
// Interactive: each option submits its `prompt` as the next turn via the A2UI
// action loop (ctx.onAction → the work screen's follow-up sender). This is what
// turns a static answer into a drill-down.
registerComponent("Choice", (comp: A2UIComponent, ctx: RenderContext) => {
  const props = comp as unknown as ChoiceProps & { id: string };
  const options = Array.isArray(props.options) ? props.options : [];
  return (
    <div key={props.id} className="work-choice">
      {options.map((opt, i) => (
        <Button
          key={i}
          type="button"
          className="work-choice-btn"
          data-mood={opt.mood ?? undefined}
          onClick={() =>
            ctx.onAction?.(props.id, "select", {
              prompt: opt.prompt,
              value: opt.label,
            })
          }
        >
          <span>{opt.label}</span>
          <span className="work-choice-arrow" aria-hidden="true">
            →
          </span>
        </Button>
      ))}
    </div>
  );
});

// ─── Divider ───
registerComponent("Divider", (comp: A2UIComponent) => {
  const props = comp as unknown as DividerProps & { id: string };
  if (props.label) {
    return (
      <div key={props.id} className="work-divider is-labeled">
        <span>{props.label}</span>
      </div>
    );
  }
  return <Separator key={props.id} className="work-divider" />;
});

function bindingPath(
  ctx: RenderContext,
  componentId: string,
  property: string,
): string | null {
  const raw = ctx.surface.components.get(componentId)?.[property];
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return null;
  return typeof (raw as { path?: unknown }).path === "string"
    ? (raw as { path: string }).path
    : null;
}

registerComponent("Text", (comp: A2UIComponent) => {
  const props = comp as unknown as TextProps & { id: string };
  return (
    <div
      key={props.id}
      className={`work-a2ui-text is-${props.variant ?? "body"}`}
    >
      <ReactMarkdown remarkPlugins={[remarkGfm]}>
        {props.text ?? ""}
      </ReactMarkdown>
    </div>
  );
});

const layoutRenderer = (direction: "row" | "column") =>
  function LayoutRenderer(comp: A2UIComponent, ctx: RenderContext) {
    const props = comp as unknown as LayoutProps & { id: string };
    const justify = {
      start: "flex-start",
      center: "center",
      end: "flex-end",
      spaceBetween: "space-between",
      spaceAround: "space-around",
      spaceEvenly: "space-evenly",
      stretch: "stretch",
    }[props.justify ?? "start"];
    const align = {
      start: "flex-start",
      center: "center",
      end: "flex-end",
      stretch: "stretch",
    }[props.align ?? "stretch"];
    const style = {
      "--a2ui-justify": justify,
      "--a2ui-align": align,
    } as React.CSSProperties;
    return (
      <div
        key={props.id}
        className={`work-a2ui-layout is-${direction}`}
        style={style}
      >
        {renderChildren(
          Array.isArray(props.children) ? props.children : [],
          ctx,
        )}
      </div>
    );
  };
registerComponent("Row", layoutRenderer("row"));
registerComponent("Column", layoutRenderer("column"));

function TabsView({
  props,
  ctx,
}: {
  props: TabsProps & { id: string };
  ctx: RenderContext;
}) {
  const tabs = Array.isArray(props.tabs) ? props.tabs : [];
  const [active, setActive] = React.useState(0);
  const selected = tabs[Math.min(active, Math.max(tabs.length - 1, 0))];
  return (
    <div className="work-a2ui-tabs">
      <Tabs className="work-a2ui-tab-list" aria-label="Configuration sections">
        {tabs.map((tab, index) => (
          <Tab
            key={`${tab.child}-${index}`}
            selected={index === active}
            className="work-a2ui-tab"
            onClick={() => setActive(index)}
          >
            {tab.title}
          </Tab>
        ))}
      </Tabs>
      {selected ? (
        <div className="work-a2ui-tab-panel">
          {renderChildren([selected.child], ctx)}
        </div>
      ) : null}
    </div>
  );
}

registerComponent("Tabs", (comp: A2UIComponent, ctx: RenderContext) => (
  <TabsView props={comp as unknown as TabsProps & { id: string }} ctx={ctx} />
));

registerComponent("TextField", (comp: A2UIComponent, ctx: RenderContext) => {
  const props = comp as unknown as TextFieldProps & { id: string };
  const path = bindingPath(ctx, props.id, "value");
  const inputType =
    props.variant === "number"
      ? "number"
      : props.variant === "obscured"
        ? "password"
        : "text";
  const common = {
    id: `${ctx.surface.surfaceId}-${props.id}`,
    className: "work-a2ui-input",
    placeholder: props.placeholder,
    value: props.value == null ? "" : String(props.value),
    onChange: (
      event: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>,
    ) => {
      if (!path) return;
      const next =
        props.variant === "number" && event.target.value !== ""
          ? Number(event.target.value)
          : event.target.value;
      ctx.onDataChange?.(path, next);
    },
  };
  return (
    <label className="work-a2ui-field" htmlFor={common.id}>
      <span className="work-a2ui-label">{props.label}</span>
      {props.variant === "longText" ? (
        <Textarea {...common} rows={4} />
      ) : (
        <Input {...common} type={inputType} />
      )}
    </label>
  );
});

registerComponent("CheckBox", (comp: A2UIComponent, ctx: RenderContext) => {
  const props = comp as unknown as CheckBoxProps & { id: string };
  const path = bindingPath(ctx, props.id, "value");
  return (
    <label className="work-a2ui-checkbox">
      <CheckboxControl
        checked={Boolean(props.value)}
        onCheckedChange={(checked) =>
          path && ctx.onDataChange?.(path, checked === true)
        }
      />
      <span>{props.label}</span>
    </label>
  );
});

registerComponent("ChoicePicker", (comp: A2UIComponent, ctx: RenderContext) => {
  const props = comp as unknown as ChoicePickerProps & { id: string };
  const path = bindingPath(ctx, props.id, "value");
  const options = Array.isArray(props.options) ? props.options : [];
  const selected = Array.isArray(props.value)
    ? props.value
    : typeof props.value === "string" && props.value
      ? [props.value]
      : [];
  if (props.variant !== "multipleSelection") {
    return (
      <label className="work-a2ui-field">
        {props.label ? (
          <span className="work-a2ui-label">{props.label}</span>
        ) : null}
        <NativeSelect
          className="work-a2ui-input"
          value={selected[0] ?? ""}
          onChange={(event) =>
            path &&
            ctx.onDataChange?.(
              path,
              event.target.value ? [event.target.value] : [],
            )
          }
        >
          <option value="">Select…</option>
          {options.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </NativeSelect>
      </label>
    );
  }
  return (
    <fieldset className="work-a2ui-picker">
      {props.label ? (
        <legend className="work-a2ui-label">{props.label}</legend>
      ) : null}
      <div
        className={
          props.displayStyle === "chips"
            ? "work-a2ui-chips"
            : "work-a2ui-check-list"
        }
      >
        {options.map((option) => {
          const checked = selected.includes(option.value);
          return (
            <label key={option.value} className="work-a2ui-check-option">
              <CheckboxControl
                checked={checked}
                onCheckedChange={() => {
                  if (!path) return;
                  ctx.onDataChange?.(
                    path,
                    checked
                      ? selected.filter((v) => v !== option.value)
                      : [...selected, option.value],
                  );
                }}
              />
              <span>{option.label}</span>
            </label>
          );
        })}
      </div>
    </fieldset>
  );
});

registerComponent("Conditional", (comp: A2UIComponent, ctx: RenderContext) => {
  const props = comp as unknown as ConditionalProps & { id: string };
  const value = props.when;
  const candidates = Array.isArray(value) ? value : [value];
  const matches = Array.isArray(props.oneOf)
    ? candidates.some((candidate) =>
        props.oneOf?.some((allowed) => Object.is(candidate, allowed)),
      )
    : props.equals === undefined
      ? Boolean(value)
      : candidates.some((candidate) => Object.is(candidate, props.equals));
  if (!matches) return null;
  return (
    <React.Fragment key={props.id}>
      {renderChildren(Array.isArray(props.children) ? props.children : [], ctx)}
    </React.Fragment>
  );
});

type ImportedOpenApiAsset = {
  id: string;
  originalName: string;
  title: string;
  version: string | null;
  baseUrl: string;
  operationCount: number;
  readOperationCount: number;
  mutatingOperationCount: number;
  authSchemes: string[];
  warnings: string[];
  checksumSha256: string;
};

function OpenApiSpecInputView({
  props,
  ctx,
}: {
  props: OpenApiSpecInputProps & { id: string };
  ctx: RenderContext;
}) {
  const path = bindingPath(ctx, props.id, "value");
  const asset =
    props.value && typeof props.value === "object"
      ? (props.value as ImportedOpenApiAsset)
      : null;
  const [mode, setMode] = React.useState<"url" | "upload">("url");
  const [url, setUrl] = React.useState("");
  const [baseUrl, setBaseUrl] = React.useState("");
  const [file, setFile] = React.useState<File | null>(null);
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  async function importSpec() {
    if (!path) return;
    setBusy(true);
    setError(null);
    try {
      let response: Response;
      if (mode === "url") {
        if (!url.trim()) throw new Error("Enter the hosted OpenAPI URL.");
        response = await fetch("/api/settings/openapi-specs", {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({
            url: url.trim(),
            baseUrl: baseUrl.trim() || undefined,
          }),
        });
      } else {
        if (!file) throw new Error("Choose an OpenAPI YAML or JSON file.");
        const body = new FormData();
        body.set("file", file);
        if (baseUrl.trim()) body.set("baseUrl", baseUrl.trim());
        response = await fetch("/api/settings/openapi-specs", {
          method: "POST",
          body,
        });
      }
      const result = (await response.json().catch(() => ({}))) as {
        asset?: ImportedOpenApiAsset;
        error?: string;
      };
      if (!response.ok || !result.asset) {
        throw new Error(result.error || "OpenAPI import failed.");
      }
      ctx.onDataChange?.(path, result.asset);
    } catch (caught) {
      setError(
        caught instanceof Error ? caught.message : "OpenAPI import failed.",
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="work-openapi-import">
      <div className="work-a2ui-label">
        {props.label ?? "OpenAPI specification"}
      </div>
      <SegmentedControl
        className="work-openapi-mode"
        aria-label="OpenAPI import method"
      >
        <Segment selected={mode === "url"} onClick={() => setMode("url")}>
          Hosted URL
        </Segment>
        <Segment selected={mode === "upload"} onClick={() => setMode("upload")}>
          Upload file
        </Segment>
      </SegmentedControl>
      {mode === "url" ? (
        <label className="work-a2ui-field">
          <span className="work-a2ui-label">OpenAPI YAML or JSON URL</span>
          <Input
            className="work-a2ui-input"
            type="url"
            value={url}
            placeholder="https://api.example.com/openapi.yaml"
            onChange={(event) => setUrl(event.target.value)}
          />
        </label>
      ) : (
        <label className="work-a2ui-field">
          <span className="work-a2ui-label">OpenAPI file</span>
          <Input
            className="work-a2ui-input work-openapi-file"
            type="file"
            accept=".yaml,.yml,.json,application/yaml,application/json,text/yaml"
            onChange={(event) => setFile(event.target.files?.[0] ?? null)}
          />
        </label>
      )}
      <label className="work-a2ui-field">
        <span className="work-a2ui-label">Base URL override (optional)</span>
        <Input
          className="work-a2ui-input"
          type="url"
          value={baseUrl}
          placeholder="https://api.example.com/v1"
          onChange={(event) => setBaseUrl(event.target.value)}
        />
      </label>
      <Button
        type="button"
        className="work-a2ui-button"
        disabled={busy}
        onClick={() => void importSpec()}
      >
        {busy
          ? "Validating…"
          : asset
            ? "Replace imported spec"
            : "Import and validate"}
      </Button>
      {error ? (
        <div className="work-openapi-error" role="alert">
          {error}
        </div>
      ) : null}
      {asset ? (
        <div className="work-openapi-summary">
          <div className="work-openapi-summary-head">
            <div>
              <strong>{asset.title}</strong>
              <span>
                {asset.version ? `v${asset.version}` : asset.originalName}
              </span>
            </div>
            <span className="work-openapi-ready">Validated</span>
          </div>
          <dl>
            <div>
              <dt>Base URL</dt>
              <dd>{asset.baseUrl}</dd>
            </div>
            <div>
              <dt>Operations</dt>
              <dd>
                {asset.operationCount} ({asset.readOperationCount} read,{" "}
                {asset.mutatingOperationCount} mutating)
              </dd>
            </div>
            <div>
              <dt>Authentication</dt>
              <dd>
                {asset.authSchemes.length
                  ? asset.authSchemes.join(", ")
                  : "Not declared"}
              </dd>
            </div>
            <div>
              <dt>SHA-256</dt>
              <dd className="work-openapi-checksum">{asset.checksumSha256}</dd>
            </div>
          </dl>
          {asset.warnings.length ? (
            <ul>
              {asset.warnings.map((warning) => (
                <li key={warning}>{warning}</li>
              ))}
            </ul>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

registerComponent(
  "OpenApiSpecInput",
  (comp: A2UIComponent, ctx: RenderContext) => (
    <OpenApiSpecInputView
      props={comp as unknown as OpenApiSpecInputProps & { id: string }}
      ctx={ctx}
    />
  ),
);

type ManagedLocalFileSet = {
  sourceName: string;
  ready: boolean;
  files: Array<{
    name: string;
    size: number;
    contentType: string;
    checksumSha256: string;
  }>;
  totalSize: number;
};

function ManagedFileSourceInputView({
  props,
  ctx,
}: {
  props: ManagedFileSourceInputProps & { id: string };
  ctx: RenderContext;
}) {
  const path = bindingPath(ctx, props.id, "value");
  const sourceName =
    typeof props.sourceName === "string" ? props.sourceName.trim() : "";
  const staged =
    props.value && typeof props.value === "object"
      ? (props.value as ManagedLocalFileSet)
      : null;
  const current = staged?.sourceName === sourceName ? staged : null;
  const [files, setFiles] = React.useState<File[]>([]);
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  async function uploadFiles() {
    if (!path) return;
    if (!sourceName) {
      setError("Enter a source name before uploading files.");
      return;
    }
    if (files.length === 0) {
      setError("Choose at least one file.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const body = new FormData();
      body.set("sourceName", sourceName);
      files.forEach((file) => body.append("files", file));
      const response = await fetch("/api/settings/file-sources", {
        method: "POST",
        body,
      });
      const result = (await response.json().catch(() => ({}))) as {
        managedLocalFiles?: ManagedLocalFileSet;
        error?: string;
      };
      if (!response.ok || !result.managedLocalFiles) {
        throw new Error(result.error || "Managed file upload failed.");
      }
      // The server returns the complete directory manifest so approval covers
      // every file GraphJin will expose, including earlier upload batches.
      ctx.onDataChange?.(path, result.managedLocalFiles);
      setFiles([]);
    } catch (caught) {
      setError(
        caught instanceof Error
          ? caught.message
          : "Managed file upload failed.",
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="work-openapi-import work-managed-files">
      <div className="work-a2ui-label">
        {props.label ?? "Managed local files"}
      </div>
      <div className="work-managed-files-note">
        OpenNeko creates an isolated GraphJin directory for this source.
        Uploaded files become readable to authenticated organization members
        after admin approval. Upload only team-approved content.
      </div>
      <label className="work-a2ui-field">
        <span className="work-a2ui-label">Files</span>
        <Input
          className="work-a2ui-input work-openapi-file"
          type="file"
          multiple
          onChange={(event) => setFiles(Array.from(event.target.files ?? []))}
        />
      </label>
      <Button
        type="button"
        className="work-a2ui-button"
        disabled={busy || !sourceName || files.length === 0}
        onClick={() => void uploadFiles()}
      >
        {busy ? "Uploading securely…" : "Stage files"}
      </Button>
      {staged && !current ? (
        <div className="work-openapi-error" role="alert">
          The source name changed. Stage files for “
          {sourceName || "the new source"}”.
        </div>
      ) : null}
      {error ? (
        <div className="work-openapi-error" role="alert">
          {error}
        </div>
      ) : null}
      {current ? (
        <div className="work-openapi-summary">
          <div className="work-openapi-summary-head">
            <div>
              <strong>
                {current.files.length} staged file
                {current.files.length === 1 ? "" : "s"}
              </strong>
              <span>{current.totalSize.toLocaleString()} bytes</span>
            </div>
            <span className="work-openapi-ready">Isolated</span>
          </div>
          <ul className="work-managed-files-list">
            {current.files.map((file) => (
              <li key={`${file.name}:${file.checksumSha256}`}>
                <span>{file.name}</span>
                <code>{file.checksumSha256.slice(0, 12)}…</code>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </div>
  );
}

registerComponent(
  "ManagedFileSourceInput",
  (comp: A2UIComponent, ctx: RenderContext) => (
    <ManagedFileSourceInputView
      props={comp as unknown as ManagedFileSourceInputProps & { id: string }}
      ctx={ctx}
    />
  ),
);

registerComponent("Button", (comp: A2UIComponent, ctx: RenderContext) => {
  const props = comp as unknown as ButtonProps & { id: string };
  const event = props.action?.event;
  const labelComponent = props.child
    ? ctx.surface.components.get(props.child)
    : undefined;
  const resolvedLabel = labelComponent
    ? resolveComponent(labelComponent, ctx.surface.dataModel)
    : undefined;
  const label =
    resolvedLabel?.component === "Text"
      ? String(resolvedLabel.text ?? "")
      : null;
  const requiresValue = props.requires;
  const hasRequirement = Object.hasOwn(
    ctx.surface.components.get(props.id) ?? {},
    "requires",
  );
  const requirementSatisfied = Array.isArray(requiresValue)
    ? requiresValue.length > 0 && requiresValue.every(Boolean)
    : Boolean(requiresValue);
  return (
    <Button
      type="button"
      className={`work-a2ui-button is-${props.variant ?? "default"}`}
      disabled={hasRequirement && !requirementSatisfied}
      onClick={() =>
        event && ctx.onAction?.(props.id, event.name, event.context)
      }
    >
      {label !== null ? (
        <span className="work-a2ui-button-label">{label}</span>
      ) : (
        renderChildren(props.child ? [props.child] : [], ctx)
      )}
      <span aria-hidden="true">→</span>
    </Button>
  );
});
