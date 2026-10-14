import { cn } from "@/lib/cn";

type RecordActionPayload = {
  app: string;
  object: string;
  id: string;
  fields: Record<string, unknown>;
  expected: Record<string, unknown> | null;
};

function objectValue(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

export function parseRecordUpdatePayload(
  kind: string | undefined,
  value: unknown,
): RecordActionPayload | null {
  if (kind !== "record_update") return null;
  const payload = objectValue(value);
  const fields = objectValue(payload?.fields);
  if (
    !payload ||
    typeof payload.app !== "string" ||
    typeof payload.object !== "string" ||
    typeof payload.id !== "string" ||
    !fields ||
    Object.keys(fields).length === 0
  ) return null;
  return {
    app: payload.app,
    object: payload.object,
    id: payload.id,
    fields,
    expected: objectValue(payload.expected),
  };
}

type NewField = { api_name: string; label: string; kind: string };

const KIND_LABELS: Record<string, string> = {
  text: "text",
  textarea: "long text",
  integer: "whole number",
  decimal: "decimal number",
  boolean: "yes or no",
  date: "date",
  datetime: "date and time",
  email: "email",
  url: "web link",
  multipicklist: "list of choices",
};

export function parseRecordNewFields(kind: string | undefined, value: unknown): NewField[] {
  if (kind !== "record_create" && kind !== "record_update") return [];
  const list = objectValue(value)?.new_fields;
  if (!Array.isArray(list)) return [];
  return list.flatMap((entry) => {
    const field = objectValue(entry);
    return field && typeof field.api_name === "string" && typeof field.label === "string" && typeof field.kind === "string"
      ? [{ api_name: field.api_name, label: field.label, kind: field.kind }]
      : [];
  });
}

function displayValue(value: unknown, fallback: string): string {
  if (value === undefined) return fallback;
  if (value === null) return "Empty";
  if (typeof value === "boolean") return value ? "Yes" : "No";
  if (typeof value === "string") return value || "Empty";
  if (typeof value === "number" || typeof value === "bigint") return String(value);
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

/** The fields a record write adds to its object, shown before approval. */
export function RecordNewFieldsNotice({ kind, payload }: { kind?: string; payload: unknown }) {
  const newFields = parseRecordNewFields(kind, payload);
  if (newFields.length === 0) return null;
  return (
    <div className="record-action-new-fields" aria-label="New fields">
      <strong>
        Adds {newFields.length === 1 ? "a new field" : `${newFields.length} new fields`} to this
        object
      </strong>
      <ul>
        {newFields.map((field) => (
          <li key={field.api_name}>
            {field.label} <span>· {KIND_LABELS[field.kind] ?? field.kind}</span>
          </li>
        ))}
      </ul>
      <p>An administrator must approve. Approving adds the fields, then writes the record.</p>
    </div>
  );
}

export function RecordActionDiff({
  kind,
  payload,
  compact = false,
  policyContext,
}: {
  kind?: string;
  payload: unknown;
  compact?: boolean;
  policyContext?: string | null;
}) {
  const parsed = parseRecordUpdatePayload(kind, payload);
  const newFields = parseRecordNewFields(kind, payload);
  if (!parsed && newFields.length === 0) return null;
  const newFieldsNotice = <RecordNewFieldsNotice kind={kind} payload={payload} />;
  if (!parsed) return <div className={cn("record-action-diff", compact && "is-compact")}>{newFieldsNotice}</div>;
  return (
    <div
      className={cn("record-action-diff", compact && "is-compact")}
      aria-label="Proposed record changes"
    >
      {newFieldsNotice}
      {!compact && (
        <div className="record-action-diff-target">
          <span>Record</span>
          <code>{parsed.app} / {parsed.object} / {parsed.id}</code>
        </div>
      )}
      <dl>
        {Object.entries(parsed.fields).map(([field, next]) => (
          <div key={field}>
            <dt>{field.replace(/_/g, " ")}</dt>
            <dd>
              <span className="record-action-diff-old">
                {displayValue(parsed.expected?.[field], "Current value")}
              </span>
              <span className="record-action-diff-arrow" aria-hidden="true">→</span>
              <strong>{displayValue(next, "Empty")}</strong>
            </dd>
          </div>
        ))}
      </dl>
      {parsed.expected && (
        <p className="record-action-diff-freshness">
          Expected values captured with this request · rechecked immediately before write.
          {policyContext && <> <strong>Rule:</strong> {policyContext}.</>}
        </p>
      )}
    </div>
  );
}
