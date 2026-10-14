import type {
  RecordFieldKind,
  RecordFilterExpression,
  RecordListFilter,
} from "@neko/records";

export type RecordFilterField = {
  apiName: string;
  label: string;
  kind: RecordFieldKind;
  picklistValues: unknown[] | null;
  referenceTargets?: string[] | null;
};

export type FilterMatch = "and" | "or";

export type FilterInput =
  | "none"
  | "text"
  | "number"
  | "date"
  | "datetime"
  | "days"
  | "boolean"
  | "options"
  | "reference";

export type FilterCondition = {
  value: string;
  label: string;
  input: FilterInput;
};

export type FilterOption = { value: string; label: string };

const NUMBER_KINDS = new Set<RecordFieldKind>(["integer", "decimal", "currency", "percent"]);

const EMPTY: FilterCondition[] = [
  { value: "empty", label: "is empty", input: "none" },
  { value: "not_empty", label: "is not empty", input: "none" },
];

export function filterOptions(field: Pick<RecordFilterField, "picklistValues"> | undefined): FilterOption[] {
  return (field?.picklistValues ?? []).flatMap((entry) => {
    if (typeof entry === "string") return [{ value: entry, label: entry }];
    if (entry && typeof entry === "object" && !Array.isArray(entry)) {
      const option = entry as Record<string, unknown>;
      return typeof option.value === "string"
        ? [{ value: option.value, label: typeof option.label === "string" ? option.label : option.value }]
        : [];
    }
    return [];
  });
}

function hasSemantic(field: RecordFilterField, semantic: "open" | "closed"): boolean {
  return (field.picklistValues ?? []).some((entry) => {
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) return false;
    const value = (entry as Record<string, unknown>).semantic;
    return Array.isArray(value) ? value.includes(semantic) : value === semantic;
  });
}

/** The conditions that make sense for one field kind, with the input each one needs. */
export function filterConditions(field: RecordFilterField | undefined): FilterCondition[] {
  if (!field) return [];
  const kind = field.kind;
  if (NUMBER_KINDS.has(kind)) {
    return [
      { value: "eq", label: "equals", input: "number" },
      { value: "neq", label: "does not equal", input: "number" },
      { value: "gt", label: "is greater than", input: "number" },
      { value: "gte", label: "is at least", input: "number" },
      { value: "lt", label: "is less than", input: "number" },
      { value: "lte", label: "is at most", input: "number" },
      ...EMPTY,
    ];
  }
  if (kind === "date" || kind === "datetime") {
    const input = kind === "date" ? "date" : "datetime";
    return [
      ...(kind === "date" ? [{ value: "eq", label: "is on", input } as FilterCondition] : []),
      { value: "lt", label: "is before", input },
      { value: "gt", label: "is after", input },
      { value: "this_quarter", label: "is this quarter", input: "none" },
      { value: "last_n_days", label: "is in the last", input: "days" },
      ...EMPTY,
    ];
  }
  if (kind === "boolean") {
    return [{ value: "eq", label: "is", input: "boolean" }, ...EMPTY];
  }
  if (kind === "picklist") {
    return [
      { value: "in", label: "is any of", input: "options" },
      { value: "not_in", label: "is none of", input: "options" },
      ...(hasSemantic(field, "open") ? [{ value: "is_open", label: "is open", input: "none" } as FilterCondition] : []),
      ...(hasSemantic(field, "closed") ? [{ value: "is_closed", label: "is closed", input: "none" } as FilterCondition] : []),
      ...EMPTY,
    ];
  }
  if (kind === "multipicklist" || kind === "readonly_formula" || kind === "id") return EMPTY;
  if (kind === "reference") {
    return [
      { value: "eq", label: "is", input: "reference" },
      { value: "neq", label: "is not", input: "reference" },
      ...EMPTY,
    ];
  }
  return [
    { value: "contains", label: "contains", input: "text" },
    { value: "eq", label: "is", input: "text" },
    { value: "neq", label: "is not", input: "text" },
    { value: "starts_with", label: "starts with", input: "text" },
    ...EMPTY,
  ];
}

/** Turns a condition and its raw input into one filter clause. Returns null when the input is incomplete. */
export function filterLeaf(
  field: RecordFilterField,
  condition: string,
  raw: string | string[],
): RecordListFilter | null {
  const text = Array.isArray(raw) ? "" : raw.trim();
  const base = { field: field.apiName };
  switch (condition) {
    case "empty":
      return { ...base, operator: "is_null", value: true };
    case "not_empty":
      return { ...base, operator: "is_null", value: false };
    case "is_open":
    case "is_closed":
      return { ...base, operator: condition };
    case "this_quarter":
      return { ...base, operator: "relative", value: { macro: "this_quarter" } };
    case "last_n_days": {
      const days = Number(text);
      return Number.isSafeInteger(days) && days >= 1 && days <= 3650
        ? { ...base, operator: "relative", value: { macro: "last_n_days", days } }
        : null;
    }
    case "in":
    case "not_in": {
      const values = (Array.isArray(raw) ? raw : text.split(",")).map((item) => item.trim()).filter(Boolean);
      return values.length > 0 ? { ...base, operator: condition, value: values } : null;
    }
  }
  if (!text) return null;
  const operator = condition as RecordListFilter["operator"];
  if (field.kind === "boolean") return { ...base, operator, value: text === "true" };
  if (NUMBER_KINDS.has(field.kind)) {
    const numeric = Number(text);
    return Number.isFinite(numeric) ? { ...base, operator, value: numeric } : null;
  }
  if (field.kind === "datetime") {
    const date = new Date(text);
    return Number.isNaN(date.getTime()) ? null : { ...base, operator, value: date.toISOString() };
  }
  return { ...base, operator, value: text };
}

/** The condition key and raw input that recreate one clause in the editor. */
export function leafCondition(leaf: RecordListFilter): { condition: string; raw: string | string[] } {
  if (leaf.operator === "is_null") return { condition: leaf.value ? "empty" : "not_empty", raw: "" };
  if (leaf.operator === "relative") {
    const value = leaf.value && typeof leaf.value === "object" ? (leaf.value as Record<string, unknown>) : {};
    return value.macro === "last_n_days"
      ? { condition: "last_n_days", raw: String(value.days ?? "") }
      : { condition: "this_quarter", raw: "" };
  }
  if (Array.isArray(leaf.value)) return { condition: leaf.operator, raw: leaf.value.map(String) };
  return { condition: leaf.operator, raw: leaf.value === undefined || leaf.value === null ? "" : String(leaf.value) };
}

export function parseFilter(raw: string | undefined | null): RecordFilterExpression | null {
  if (!raw || raw === "null") return null;
  try {
    const parsed = JSON.parse(raw) as unknown;
    return parsed && typeof parsed === "object" ? (parsed as RecordFilterExpression) : null;
  } catch {
    return null;
  }
}

function leaves(expression: RecordFilterExpression): RecordListFilter[] {
  return "op" in expression ? expression.clauses.flatMap(leaves) : [expression];
}

/** Flattens a filter into one list of clauses and how they combine. */
export function filterClauses(expression: RecordFilterExpression | null): { match: FilterMatch; clauses: RecordListFilter[] } {
  if (!expression) return { match: "and", clauses: [] };
  return { match: "op" in expression ? expression.op : "and", clauses: leaves(expression) };
}

export function buildFilter(match: FilterMatch, clauses: RecordListFilter[]): RecordFilterExpression | null {
  if (clauses.length === 0) return null;
  if (clauses.length === 1) return clauses[0]!;
  return { op: match, clauses };
}

function optionLabel(field: RecordFilterField | undefined, value: unknown): string {
  const match = filterOptions(field).find((option) => option.value === value);
  return match?.label ?? String(value ?? "");
}

function shownValue(field: RecordFilterField | undefined, value: unknown): string {
  if (Array.isArray(value)) return value.map((item) => optionLabel(field, item)).join(", ");
  if (typeof value === "boolean") return value ? "yes" : "no";
  if (field?.kind === "datetime" && typeof value === "string") {
    const date = new Date(value);
    if (!Number.isNaN(date.getTime())) return date.toLocaleString("en", { dateStyle: "medium", timeStyle: "short" });
  }
  if (field?.kind === "date" && typeof value === "string" && /^\d{4}-\d{2}-\d{2}$/.test(value)) {
    return new Date(`${value}T00:00:00Z`).toLocaleDateString("en", { dateStyle: "medium", timeZone: "UTC" });
  }
  return optionLabel(field, value);
}

/** A sentence-case description of one clause, such as "is any of LinkedIn, Facebook". */
export function describeLeaf(leaf: RecordListFilter, field: RecordFilterField | undefined): string {
  const { condition } = leafCondition(leaf);
  if (condition === "last_n_days") {
    const days = (leaf.value as Record<string, unknown>).days;
    return `is in the last ${String(days)} days`;
  }
  const known = filterConditions(field).find((candidate) => candidate.value === condition);
  const fallback: Record<string, string> = {
    eq: "is", neq: "is not", in: "is any of", not_in: "is none of", contains: "contains",
    starts_with: "starts with", gt: "is greater than", gte: "is at least", lt: "is less than",
    lte: "is at most", empty: "is empty", not_empty: "is not empty", this_quarter: "is this quarter",
    is_open: "is open", is_closed: "is closed",
  };
  const label = known?.label ?? fallback[condition] ?? condition.replaceAll("_", " ");
  if (known?.input === "none" || ["empty", "not_empty", "this_quarter", "is_open", "is_closed"].includes(condition)) {
    return label;
  }
  return `${label} ${shownValue(field, leaf.value)}`.trim();
}

export function hrefWithQuery(
  base: string,
  current: Record<string, string | undefined>,
  changes: Record<string, string | null>,
): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(current)) {
    if (value !== undefined) params.set(key, value);
  }
  for (const [key, value] of Object.entries(changes)) {
    if (value === null) params.delete(key);
    else params.set(key, value);
  }
  const query = params.toString();
  return query ? `${base}?${query}` : base;
}

/** Query keys that reset paging when the result set changes. */
export const PAGING_RESET = { after: null, page: null, back: null } as const;

export const RECORD_PAGE_SIZES = [25, 50, 100] as const;
