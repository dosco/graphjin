import type { JsonObject, RecordObjectView, RecordViewColumn } from "@neko/records";
import type { RecordOwnerIdentity } from "@/lib/records";
import {
  recordReferenceIdentityKey,
  type RecordReferenceIdentity,
} from "@/lib/records-reference";
import { RecordCell } from "./RecordCell";
import { RecordDetailField } from "./RecordDetailField";

function editable(column: RecordViewColumn): boolean {
  return (
    !column.readOnly &&
    column.kind !== "readonly_formula" &&
    column.kind !== "id" &&
    column.kind !== "owner" &&
    column.kind !== "system_datetime"
  );
}

export function RecordDetailCard({
  appId,
  view,
  row,
  owners,
  references = {},
  layout,
  canUpdate = false,
}: {
  appId: string;
  view: RecordObjectView;
  row: Record<string, unknown>;
  owners: Record<string, RecordOwnerIdentity>;
  references?: Record<string, RecordReferenceIdentity>;
  layout?: JsonObject | null;
  canUpdate?: boolean;
}) {
  const recordId = String(row.id ?? "");
  const ownerId =
    typeof row.owner_user_id === "string" ? row.owner_user_id : null;
  const sections = detailSections(layout, view.columns);
  return (
    <section className="records-detail-card">
      {sections.map((section) => (
        <section className="records-detail-section" key={section.label}>
          <h2>{section.label}</h2>
          <dl className="records-detail-grid">
            {section.columns.map((column) => (
              <RecordDetailField
                key={column.apiName}
                appId={appId}
                objectApiName={view.object.apiName}
                recordId={recordId}
                column={column}
                value={row[column.columnName] ?? null}
                editable={canUpdate && Boolean(recordId) && editable(column)}
              >
                <RecordCell
                  appId={appId}
                  column={column}
                  value={row[column.columnName]}
                  owner={
                    column.kind === "owner" && ownerId
                      ? owners[ownerId]
                      : undefined
                  }
                  reference={references[
                    recordReferenceIdentityKey(
                      column.apiName,
                      String(row[column.columnName] ?? ""),
                    )
                  ]}
                />
              </RecordDetailField>
            ))}
          </dl>
        </section>
      ))}
      <footer className="records-detail-id">
        <span>Record ID</span>
        <code>{String(row.id)}</code>
      </footer>
    </section>
  );
}

function detailSections(
  layout: JsonObject | null | undefined,
  columns: RecordViewColumn[],
): Array<{ label: string; columns: RecordViewColumn[] }> {
  const byName = new Map(columns.map((column) => [column.apiName, column]));
  const used = new Set<string>();
  const rawSections = Array.isArray(layout?.sections) ? layout.sections : [];
  const sections = rawSections.flatMap((raw, index) => {
    if (!raw || typeof raw !== "object" || Array.isArray(raw)) return [];
    const section = raw as Record<string, unknown>;
    if (!Array.isArray(section.fields)) return [];
    const selected = section.fields.flatMap((entry): RecordViewColumn[] => {
      const name = typeof entry === "string"
        ? entry
        : entry && typeof entry === "object" && !Array.isArray(entry)
          ? String((entry as Record<string, unknown>).field ?? "")
          : "";
      const column = byName.get(name);
      if (!column || used.has(column.apiName)) return [];
      used.add(column.apiName);
      return [column];
    });
    return selected.length > 0
      ? [{
          label: typeof section.label === "string" && section.label.trim()
            ? section.label
            : `Details ${index + 1}`,
          columns: selected,
        }]
      : [];
  });
  const remaining = columns.filter((column) => !used.has(column.apiName));
  if (remaining.length > 0) {
    sections.push({ label: sections.length > 0 ? "Record metadata" : "Details", columns: remaining });
  }
  return sections;
}
