/**
 * A2UI Component Catalog
 *
 * The pre-approved components an agent may compose into a web answer surface —
 * briefings AND conversational answers share this one vocabulary. Display
 * components (Markdown, Table, Callout, BriefingCard) plus layout (Section,
 * Divider) plus the interactive Choice, which round-trips a follow-up back to
 * the agent via the A2UI action loop.
 *
 * All property values support A2UI data binding via { path: "/..." }
 */

/** Dashboard/old-chat catalog retained for persisted v0.9 surfaces. */
export const CATALOG_ID = "urn:app:catalog:briefing:v1";
/** Conversational catalog used by new v1.0 surfaces. */
export const WORK_CATALOG_ID = "urn:openneko:catalog:work:v2";

// Component type names agents can reference. "Briefing" is the dashboard's
// daily-briefing root; the conversational work/Ask surface uses "Answer". Both
// render the same frame, so MetricCard is the neutral name for the KPI card in
// an Answer (BriefingCard stays for the dashboard + already-stored surfaces).
export const ComponentTypes = {
  Answer: "Answer",
  KeyFigures: "KeyFigures",
  MetricCard: "MetricCard",
  Chart: "Chart",
  Confirmation: "Confirmation",
  Markdown: "Markdown",
  Table: "Table",
  Section: "Section",
  Callout: "Callout",
  Choice: "Choice",
  Divider: "Divider",
  Text: "Text",
  Row: "Row",
  Column: "Column",
  Tabs: "Tabs",
  Button: "Button",
  TextField: "TextField",
  CheckBox: "CheckBox",
  ChoicePicker: "ChoicePicker",
  Conditional: "Conditional",
  OpenApiSpecInput: "OpenApiSpecInput",
  ManagedFileSourceInput: "ManagedFileSourceInput",
  Compare: "Compare",
  WhatIf: "WhatIf",
  AnswerMap: "AnswerMap",
  Diagram: "Diagram",
  AnswerTool: "AnswerTool",
  // Dashboard / back-compat aliases (same renderers):
  Briefing: "Briefing",
  BriefingCard: "BriefingCard",
} as const;

// Mood enum shared across components
export type Mood = "good" | "watch" | "act";

// Chart type enum — 5 types cover 92% of exec needs
export type ChartType = "kpi" | "line" | "bar" | "area" | "donut";

// Chart data point
export interface ChartDataPoint {
  d: string;  // label (e.g. day name)
  v: number;  // primary value
  t?: number; // secondary/comparison value
}

/** An evidence-backed chart in an Ask answer, independent of a saved metric. */
export interface ChartProps {
  component: "Chart";
  title: string;
  type: Exclude<ChartType, "kpi">;
  data: ChartDataPoint[];
  valueLabel: string;
  baselineLabel?: string;
  source?: string;
  asOf?: string;
  drill?: string;
}

// --- Component Property Schemas ---

export interface BriefingProps {
  component: "Briefing";
  greeting: string;
  subtitle: string;
  role: string;
  isExample?: boolean; // true when cards are the legacy ROLE_DATA mock
  children: string[]; // IDs of BriefingCard components
}

// The conversational answer root (work/Ask). Same frame as Briefing, but no
// dashboard "Briefing" framing — an optional `eyebrow` kicker instead.
export interface AnswerProps {
  component: "Answer";
  title: string;
  subtitle?: string;
  eyebrow?: string;
  /** "draft" while the agent is still checking the figures. */
  stage?: "draft" | "final";
  children: string[]; // ids of body components, in display order
}

export interface MarkdownProps {
  component: "Markdown";
  text: string;
}

export interface KeyFigure {
  label: string;
  value: string;
  sub?: string;
  basis?: "observed" | "calculated" | "estimated";
  asOf?: string;
  source?: string;
}

export interface KeyFiguresProps {
  component: "KeyFigures";
  items: KeyFigure[];
}

export interface TableColumn {
  key: string;
  label: string;
  align?: "left" | "right" | "center";
}

export interface TableProps {
  component: "Table";
  columns: TableColumn[];
  rows: Record<string, string | number>[];
  caption?: string;
  drill?: string;
  /** Sub-tables keyed by a row's first-column value; the row opens its sub-table in place. */
  details?: Record<string, { columns: TableColumn[]; rows: Record<string, string | number>[] }>;
}

/**
 * A follow-up question for one row, point, place, or step. `{label}` is
 * replaced with the item's name before the question is sent.
 */
export type DrillTemplate = string;

/** Table rows as side-by-side panels, one panel per row. */
export interface CompareProps {
  component: "Compare";
  columns: TableColumn[];
  rows: Record<string, string | number>[];
  drill?: DrillTemplate;
}

export interface WhatIfControl {
  name: string;
  label: string;
  value: number;
  min: number;
  max: number;
  step: number;
  unit?: string;
  /** A choice control: the reader picks one option, and formulas use its value. */
  options?: Array<{ label: string; value: number }>;
}

export interface WhatIfOutput {
  label: string;
  unit?: string;
  expression: string;
}

/** Controls the reader moves; outputs recompute in the browser. */
export interface WhatIfProps {
  component: "WhatIf";
  controls: WhatIfControl[];
  values: Record<string, number>;
  outputs: WhatIfOutput[];
}

export interface MapPoint {
  label: string;
  lat: number;
  lon: number;
  v: number;
}

export interface AnswerMapProps {
  component: "AnswerMap";
  title: string;
  valueLabel: string;
  points: MapPoint[];
  drill?: DrillTemplate;
}

export interface DiagramNode {
  id: string;
  label: string;
}

export interface DiagramEdge {
  from: string;
  to: string;
  label?: string;
}

export interface DiagramProps {
  component: "Diagram";
  title: string;
  nodes: DiagramNode[];
  edges: DiagramEdge[];
  drill?: DrillTemplate;
}

/** A self-contained HTML tool shown in an isolated frame. */
export interface AnswerToolProps {
  component: "AnswerTool";
  title: string;
  html: string;
}

export interface SectionProps {
  component: "Section";
  title: string;
  collapsible?: boolean; // render as a native <details> disclosure
  defaultOpen?: boolean; // open state when collapsible (default true)
  children: string[];    // ids of body components
}

export interface CalloutProps {
  component: "Callout";
  mood: Mood;        // tints the left rail + background
  title?: string;
  text: string;      // markdown body
}

export interface ChoiceOption {
  label: string;     // button text
  prompt: string;    // the follow-up turn sent to the agent when clicked
  mood?: Mood;
}

// Interactive: each option, when clicked, fires an A2UI action that submits
// `prompt` as the next turn — the in-answer drill-down / follow-up loop.
export interface ChoiceProps {
  component: "Choice";
  options: ChoiceOption[];
}

export interface DividerProps {
  component: "Divider";
  label?: string;
}

export interface TextProps {
  component: "Text";
  text: string;
  variant?: "caption" | "body";
}

export interface LayoutProps {
  component: "Row" | "Column";
  children: string[];
  justify?: "start" | "center" | "end" | "spaceBetween" | "spaceAround" | "spaceEvenly" | "stretch";
  align?: "start" | "center" | "end" | "stretch";
}

export interface TabsProps {
  component: "Tabs";
  tabs: Array<{ title: string; child: string }>;
}

export interface ButtonProps {
  component: "Button";
  child: string;
  variant?: "default" | "primary" | "borderless";
  action: {
    event: {
      name: string;
      context?: Record<string, unknown>;
      wantResponse?: boolean;
      responsePath?: string;
    };
  };
  /** When provided, the action stays disabled until this bound value exists. */
  requires?: unknown;
}

export interface TextFieldProps {
  component: "TextField";
  label: string;
  value?: string | number;
  placeholder?: string;
  variant?: "longText" | "number" | "shortText" | "obscured";
}

export interface CheckBoxProps {
  component: "CheckBox";
  label: string;
  value: boolean;
}

export interface ChoicePickerProps {
  component: "ChoicePicker";
  label?: string;
  options: Array<{ label: string; value: string }>;
  value: string[];
  variant?: "multipleSelection" | "mutuallyExclusive";
  displayStyle?: "checkbox" | "chips";
  filterable?: boolean;
}

export interface ConditionalProps {
  component: "Conditional";
  when: unknown;
  equals?: string | number | boolean;
  oneOf?: Array<string | number | boolean>;
  children: string[];
}

export interface OpenApiSpecInputProps {
  component: "OpenApiSpecInput";
  label?: string;
  value?: unknown;
}

export interface ManagedFileSourceInputProps {
  component: "ManagedFileSourceInput";
  label?: string;
  sourceName?: string;
  value?: unknown;
}

export interface ConfirmationProps {
  component: "Confirmation";
  label: string; // action eyebrow, e.g. "Created workflow"
  title: string; // the saved entity's name
  children: string[]; // IDs of body components (typically a Markdown)
}

export interface BriefingCardProps {
  component: "BriefingCard";
  metricId: string;   // DB UUID — needed for pin/dashboard API calls
  source: string;     // "bootstrap" | "chat"
  state?: "ok" | "pending" | "failed"; // discriminator for the card chrome
  error?: string;     // populated when state="failed"
  mood: Mood;
  text: string;       // headline
  metric: string;     // e.g. "$4.7M"
  label: string;      // e.g. "Revenue MTD"
  detail: string;     // expanded explanation
  chartType: ChartType;
  chartData: ChartDataPoint[];
}

// The KPI card as named in a work/Ask Answer — identical shape to BriefingCard,
// which stays the dashboard's name and renders through the same component.
export type MetricCardProps = Omit<BriefingCardProps, "component"> & {
  component: "MetricCard";
};

// Union of all component props
export type ComponentProps =
  | AnswerProps
  | BriefingProps
  | MetricCardProps
  | ChartProps
  | BriefingCardProps
  | ConfirmationProps
  | MarkdownProps
  | KeyFiguresProps
  | TableProps
  | SectionProps
  | CalloutProps
  | ChoiceProps
  | DividerProps
  | TextProps
  | LayoutProps
  | TabsProps
  | ButtonProps
  | TextFieldProps
  | CheckBoxProps
  | ChoicePickerProps
  | ConditionalProps
  | OpenApiSpecInputProps
  | ManagedFileSourceInputProps
  | CompareProps
  | WhatIfProps
  | AnswerMapProps
  | DiagramProps
  | AnswerToolProps;
