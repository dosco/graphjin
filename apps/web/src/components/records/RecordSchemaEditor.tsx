"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type FormEvent, type ReactNode } from "react";
import { ArrowDown, ArrowUp, Pencil, Plus, X } from "lucide-react";
import type { RecordFieldKind } from "@neko/records";
import { confirmDialog } from "@/components/ConfirmModal";
import { Badge } from "@/components/ui/badge";
import { Button, IconButton } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Field, Input, NativeSelect, Textarea } from "@/components/ui/field";
import { MenuItem, OverflowMenu } from "@/components/ui/overflow-menu";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Tab, Tabs } from "@/components/ui/tabs";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type {
  SchemaEditKind,
  SchemaEditorField,
  SchemaEditorModel,
  SchemaEditorObject,
  SchemaEditorSection,
} from "@/lib/records-schema-edit";

const KIND_LABELS: Partial<Record<RecordFieldKind, string>> = {
  text: "Text",
  textarea: "Long text",
  integer: "Whole number",
  decimal: "Decimal number",
  currency: "Currency",
  percent: "Percent",
  date: "Date",
  datetime: "Date and time",
  boolean: "Yes or no",
  email: "Email",
  phone: "Phone",
  url: "Web link",
  picklist: "Choice list",
  multipicklist: "Multi-choice list",
  reference: "Link to record",
  readonly_formula: "Formula",
};

const ADDABLE_KINDS = Object.keys(KIND_LABELS).filter((kind) => kind !== "readonly_formula") as RecordFieldKind[];

function apiNameFrom(label: string): string {
  const slug = label
    .toLowerCase()
    .normalize("NFKD")
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "")
    .slice(0, 60);
  return /^[0-9]/.test(slug) ? `f_${slug}` : slug;
}

function choicesFrom(text: string): string[] {
  return [...new Set(text.split("\n").map((line) => line.trim()).filter(Boolean))];
}

function move<T>(items: T[], index: number, offset: number): T[] {
  const target = index + offset;
  if (target < 0 || target >= items.length) return items;
  const next = [...items];
  [next[index], next[target]] = [next[target]!, next[index]!];
  return next;
}

type Submit = (kind: SchemaEditKind, payload: Record<string, unknown>, done: string) => Promise<boolean>;

function useSchemaSubmit(appId: string) {
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<{ tone: "info" | "error"; text: string; requestId?: string } | null>(null);
  const submit: Submit = async (kind, payload, done) => {
    setBusy(true);
    setNotice(null);
    try {
      const response = await fetch(`/api/a/${encodeURIComponent(appId)}/admin/schema`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ kind, payload }),
      });
      const body = (await response.json()) as { error?: string; status?: string; actionRequestId?: string };
      if (!response.ok) {
        setNotice({ tone: "error", text: body.error ?? "The change was not applied. Try again.", requestId: body.actionRequestId });
        return false;
      }
      setNotice(
        body.status === "queued"
          ? { tone: "info", text: "The change is still being applied. Refresh this page in a minute.", requestId: body.actionRequestId }
          : { tone: "info", text: done, requestId: body.actionRequestId },
      );
      router.refresh();
      return true;
    } catch {
      setNotice({ tone: "error", text: "The records service did not respond. The change was not applied." });
      return false;
    } finally {
      setBusy(false);
    }
  };
  return { busy, notice, submit };
}

function FieldSheet({
  open,
  onOpenChange,
  object,
  objects,
  field,
  busy,
  submit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  object: SchemaEditorObject;
  objects: SchemaEditorObject[];
  field: SchemaEditorField | null;
  busy: boolean;
  submit: Submit;
}) {
  const adding = field === null;
  const [label, setLabel] = useState(field?.label ?? "");
  const [apiName, setApiName] = useState(field?.apiName ?? "");
  const [apiNameEdited, setApiNameEdited] = useState(false);
  const [kind, setKind] = useState<RecordFieldKind>(field?.kind ?? "text");
  const [choices, setChoices] = useState((field?.picklistValues ?? []).join("\n"));
  const [target, setTarget] = useState(field?.referenceTargets[0] ?? objects[0]?.apiName ?? "");
  const [required, setRequired] = useState(field?.required ?? false);
  const [readOnly, setReadOnly] = useState(field?.readOnly ?? false);
  const [error, setError] = useState<string | null>(null);
  const picklist = kind === "picklist" || kind === "multipicklist";
  const name = apiNameEdited ? apiName : apiNameFrom(label);

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    const values = choicesFrom(choices);
    if (!label.trim()) return setError("Enter a label.");
    if (picklist && values.length === 0) return setError("Enter at least one choice.");
    let ok: boolean;
    if (adding) {
      if (!name) return setError("Enter a field name with letters or numbers.");
      if (object.fields.some((candidate) => candidate.apiName === name)) {
        return setError("A field with this name already exists. Change the field name.");
      }
      ok = await submit(
        "app_field_add",
        {
          object: object.apiName,
          field: {
            api_name: name,
            label: label.trim(),
            kind,
            required: false,
            ...(picklist ? { picklist_values: values } : {}),
            ...(kind === "reference" ? { reference_targets: [target] } : {}),
          },
        },
        `Added ${label.trim()}.`,
      );
    } else {
      const changes: Record<string, unknown> = {};
      if (label.trim() !== field.label) changes.label = label.trim();
      if (required !== field.required) changes.required = required;
      if (readOnly !== field.readOnly) changes.read_only = readOnly;
      if (picklist && values.join("\n") !== field.picklistValues.join("\n")) changes.picklist_values = values;
      if (Object.keys(changes).length === 0) return onOpenChange(false);
      ok = await submit(
        "app_field_modify",
        { object: object.apiName, field: field.apiName, changes },
        `Saved ${label.trim()}.`,
      );
    }
    if (ok) onOpenChange(false);
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="w-full overflow-y-auto sm:max-w-md">
        <form onSubmit={save} className="flex min-h-full flex-col">
          <SheetHeader>
            <SheetTitle className="font-bold">{adding ? `Add a field to ${object.pluralLabel}` : `Edit ${field.label}`}</SheetTitle>
            <SheetDescription>
              {adding
                ? "New fields start optional. Existing records keep an empty value until someone fills it in."
                : "The field type and name are permanent. You can change the label, the rules and the choices."}
            </SheetDescription>
          </SheetHeader>
          <div className="grid gap-4 px-4">
            <Field label="Label" htmlFor="schema-field-label">
              <Input id="schema-field-label" value={label} onChange={(event) => setLabel(event.target.value)} maxLength={200} required autoFocus />
            </Field>
            {adding ? (
              <>
                <Field label="Field name" htmlFor="schema-field-name" hint="Permanent. Agents and imports use this name.">
                  <Input
                    id="schema-field-name"
                    value={name}
                    onChange={(event) => {
                      setApiNameEdited(true);
                      setApiName(apiNameFrom(event.target.value));
                    }}
                    spellCheck={false}
                  />
                </Field>
                <Field label="Type" htmlFor="schema-field-kind">
                  <NativeSelect id="schema-field-kind" value={kind} onChange={(event) => setKind(event.target.value as RecordFieldKind)}>
                    {ADDABLE_KINDS.map((option) => (
                      <option key={option} value={option}>
                        {KIND_LABELS[option]}
                      </option>
                    ))}
                  </NativeSelect>
                </Field>
                {kind === "reference" && (
                  <Field label="Links to" htmlFor="schema-field-target">
                    <NativeSelect id="schema-field-target" value={target} onChange={(event) => setTarget(event.target.value)}>
                      {objects.map((candidate) => (
                        <option key={candidate.apiName} value={candidate.apiName}>
                          {candidate.label}
                        </option>
                      ))}
                    </NativeSelect>
                  </Field>
                )}
              </>
            ) : (
              <Field label="Type" htmlFor="schema-field-kind-readonly">
                <Input id="schema-field-kind-readonly" value={KIND_LABELS[field.kind] ?? field.kind} disabled readOnly />
              </Field>
            )}
            {picklist && (
              <Field label="Choices" htmlFor="schema-field-choices" hint="One choice per line, in the order people see them.">
                <Textarea id="schema-field-choices" value={choices} onChange={(event) => setChoices(event.target.value)} rows={6} />
              </Field>
            )}
            {!adding && (
              <div className="grid gap-3">
                <Checkbox label="Required" checked={required} onCheckedChange={(checked) => setRequired(checked === true)} />
                <Checkbox label="Read-only in forms" checked={readOnly} onCheckedChange={(checked) => setReadOnly(checked === true)} />
              </div>
            )}
            {error && (
              <p className="records-field-error" role="alert">
                {error}
              </p>
            )}
          </div>
          <SheetFooter className="flex-row justify-end">
            <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={busy}>
              Cancel
            </Button>
            <Button variant="primary" type="submit" disabled={busy}>
              {busy ? "Applying…" : adding ? "Add field" : "Save field"}
            </Button>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  );
}

function FieldsPanel({
  object,
  objects,
  busy,
  submit,
}: {
  object: SchemaEditorObject;
  objects: SchemaEditorObject[];
  busy: boolean;
  submit: Submit;
}) {
  const [sheet, setSheet] = useState<{ field: SchemaEditorField | null; key: number } | null>(null);

  async function archive(field: SchemaEditorField) {
    const confirmed = await confirmDialog({
      title: `Archive ${field.label}?`,
      description: "The field leaves forms, lists and agent tools. Stored values stay in the database, so the change can be undone later.",
      confirmLabel: "Archive field",
      destructive: true,
    });
    if (confirmed) await submit("app_field_archive", { object: object.apiName, field: field.apiName }, `Archived ${field.label}.`);
  }

  return (
    <section className="records-admin-panel">
      <header>
        <div>
          <h2>Fields</h2>
          <p>The information each {object.label.toLowerCase()} holds.</p>
        </div>
        <Button variant="primary" size="sm" onClick={() => setSheet({ field: null, key: Date.now() })} disabled={busy}>
          <Plus aria-hidden="true" /> Add field
        </Button>
      </header>
      <div className="records-admin-table-scroll">
        <Table className="records-admin-table">
          <TableHeader>
            <TableRow>
              <TableHead>Field</TableHead>
              <TableHead>Type</TableHead>
              <TableHead>Rules</TableHead>
              <TableHead>
                <span className="sr-only">Actions</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {object.fields.map((field) => (
              <TableRow key={field.apiName}>
                <TableCell>
                  <strong>{field.label}</strong>
                  <small>{field.apiName}</small>
                </TableCell>
                <TableCell>
                  {KIND_LABELS[field.kind] ?? field.kind}
                  {field.picklistValues.length > 0 && <small>{field.picklistValues.length} choices</small>}
                  {field.referenceTargets.length > 0 && (
                    <small>
                      {field.referenceTargets
                        .map((target) => objects.find((candidate) => candidate.apiName === target)?.label ?? target)
                        .join(", ")}
                    </small>
                  )}
                </TableCell>
                <TableCell>
                  <span className="records-schema-rules">
                    {field.apiName === object.nameField && <Badge variant="muted">Record name</Badge>}
                    {field.required && <Badge variant="muted">Required</Badge>}
                    {field.readOnly && <Badge variant="muted">Read-only</Badge>}
                  </span>
                </TableCell>
                <TableCell className="records-schema-actions">
                  <IconButton
                    size="icon-sm"
                    variant="ghost"
                    label={`Edit ${field.label}`}
                    disabled={busy}
                    onClick={() => setSheet({ field, key: Date.now() })}
                  >
                    <Pencil aria-hidden="true" />
                  </IconButton>
                  {field.apiName !== object.nameField && (
                    <OverflowMenu label={`More actions for ${field.label}`}>
                      <MenuItem danger disabled={busy} onSelect={() => void archive(field)}>
                        Archive field
                      </MenuItem>
                    </OverflowMenu>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      {sheet && (
        <FieldSheet
          key={sheet.key}
          open
          onOpenChange={(open) => !open && setSheet(null)}
          object={object}
          objects={objects}
          field={sheet.field}
          busy={busy}
          submit={submit}
        />
      )}
    </section>
  );
}

function OrderedFields({
  names,
  labels,
  onChange,
  disabled,
  extra,
}: {
  names: string[];
  labels: Map<string, string>;
  onChange: (names: string[]) => void;
  disabled: boolean;
  extra?: (name: string, index: number) => ReactNode;
}) {
  return (
    <ol className="records-layout-list">
      {names.map((name, index) => (
        <li key={name}>
          <span className="records-layout-name">{labels.get(name) ?? name}</span>
          {extra?.(name, index)}
          <IconButton size="icon-sm" variant="ghost" label={`Move ${labels.get(name) ?? name} up`} disabled={disabled || index === 0} onClick={() => onChange(move(names, index, -1))}>
            <ArrowUp aria-hidden="true" />
          </IconButton>
          <IconButton size="icon-sm" variant="ghost" label={`Move ${labels.get(name) ?? name} down`} disabled={disabled || index === names.length - 1} onClick={() => onChange(move(names, index, 1))}>
            <ArrowDown aria-hidden="true" />
          </IconButton>
        </li>
      ))}
    </ol>
  );
}

function ListLayoutPanel({ object, busy, submit }: { object: SchemaEditorObject; busy: boolean; submit: Submit }) {
  const [columns, setColumns] = useState(object.listColumns);
  const labels = new Map(object.fields.map((field) => [field.apiName, field.label]));
  const hidden = object.fields.filter((field) => !columns.includes(field.apiName));
  const dirty = columns.join(",") !== object.listColumns.join(",");
  return (
    <section className="records-admin-panel">
      <header>
        <div>
          <h2>List columns</h2>
          <p>The columns people see in the {object.pluralLabel.toLowerCase()} list, from left to right.</p>
        </div>
        <Button
          variant="primary"
          size="sm"
          disabled={busy || !dirty || columns.length === 0}
          onClick={() => void submit("app_layout_update", { object: object.apiName, kind: "list", definition: { columns } }, "Saved the list columns.")}
        >
          Save columns
        </Button>
      </header>
      <OrderedFields
        names={columns}
        labels={labels}
        onChange={setColumns}
        disabled={busy}
        extra={(name) => (
          <IconButton size="icon-sm" variant="ghost" label={`Hide ${labels.get(name) ?? name}`} disabled={busy || columns.length === 1} onClick={() => setColumns(columns.filter((item) => item !== name))}>
            <X aria-hidden="true" />
          </IconButton>
        )}
      />
      {hidden.length > 0 && (
        <div className="records-layout-add">
          <NativeSelect
            aria-label="Show another column"
            value=""
            disabled={busy}
            onChange={(event) => event.target.value && setColumns([...columns, event.target.value])}
          >
            <option value="">Show another column…</option>
            {hidden.map((field) => (
              <option key={field.apiName} value={field.apiName}>
                {field.label}
              </option>
            ))}
          </NativeSelect>
        </div>
      )}
    </section>
  );
}

function DetailLayoutPanel({ object, busy, submit }: { object: SchemaEditorObject; busy: boolean; submit: Submit }) {
  const [sections, setSections] = useState<SchemaEditorSection[]>(object.detailSections);
  const labels = new Map(object.fields.map((field) => [field.apiName, field.label]));
  const dirty = JSON.stringify(sections) !== JSON.stringify(object.detailSections);
  const placed = new Set(sections.flatMap((section) => section.fields));
  const unplaced = object.fields.map((field) => field.apiName).filter((name) => !placed.has(name));

  function update(index: number, change: Partial<SchemaEditorSection>) {
    setSections(sections.map((section, position) => (position === index ? { ...section, ...change } : section)));
  }

  function moveField(name: string, from: number, to: number) {
    setSections(
      sections.map((section, position) => {
        if (position === from) return { ...section, fields: section.fields.filter((item) => item !== name) };
        if (position === to) return { ...section, fields: [...section.fields, name] };
        return section;
      }),
    );
  }

  return (
    <section className="records-admin-panel">
      <header>
        <div>
          <h2>Record page</h2>
          <p>
            How fields group on each {object.label.toLowerCase()} page.
            {unplaced.length > 0 && ` ${unplaced.length === 1 ? "One field is" : `${unplaced.length} fields are`} not on the page yet.`}
          </p>
        </div>
        <Button
          variant="primary"
          size="sm"
          disabled={busy || !dirty || sections.some((section) => !section.label.trim())}
          onClick={() =>
            void submit(
              "app_layout_update",
              {
                object: object.apiName,
                kind: "detail",
                definition: {
                  sections: sections
                    .filter((section) => section.fields.length > 0)
                    .map((section) => ({ label: section.label.trim(), fields: section.fields })),
                },
              },
              "Saved the record page layout.",
            )
          }
        >
          Save layout
        </Button>
      </header>
      <div className="records-layout-sections">
        {sections.map((section, index) => (
          <div className="records-layout-section" key={index}>
            <div className="records-layout-section-head">
              <Field label="Section name" htmlFor={`schema-section-${index}`}>
                <Input id={`schema-section-${index}`} value={section.label} maxLength={80} onChange={(event) => update(index, { label: event.target.value })} />
              </Field>
              <IconButton size="icon-sm" variant="ghost" label={`Move ${section.label || "section"} up`} disabled={busy || index === 0} onClick={() => setSections(move(sections, index, -1))}>
                <ArrowUp aria-hidden="true" />
              </IconButton>
              <IconButton size="icon-sm" variant="ghost" label={`Move ${section.label || "section"} down`} disabled={busy || index === sections.length - 1} onClick={() => setSections(move(sections, index, 1))}>
                <ArrowDown aria-hidden="true" />
              </IconButton>
              {section.fields.length === 0 && sections.length > 1 && (
                <IconButton size="icon-sm" variant="ghost" label={`Remove ${section.label || "section"}`} disabled={busy} onClick={() => setSections(sections.filter((_, position) => position !== index))}>
                  <X aria-hidden="true" />
                </IconButton>
              )}
            </div>
            {unplaced.length > 0 && (
              <div className="records-layout-place">
                <NativeSelect
                  aria-label={`Add a field to ${section.label || "this section"}`}
                  value=""
                  disabled={busy}
                  onChange={(event) => event.target.value && update(index, { fields: [...section.fields, event.target.value] })}
                >
                  <option value="">Add a field to this section…</option>
                  {unplaced.map((name) => (
                    <option key={name} value={name}>
                      {labels.get(name) ?? name}
                    </option>
                  ))}
                </NativeSelect>
              </div>
            )}
            {section.fields.length === 0 ? (
              <p className="records-layout-empty">Move fields here from another section.</p>
            ) : (
              <OrderedFields
                names={section.fields}
                labels={labels}
                onChange={(fields) => update(index, { fields })}
                disabled={busy}
                extra={(name) =>
                  sections.length > 1 ? (
                    <NativeSelect
                      aria-label={`Section for ${labels.get(name) ?? name}`}
                      className="records-layout-move"
                      value={String(index)}
                      disabled={busy}
                      onChange={(event) => moveField(name, index, Number(event.target.value))}
                    >
                      {sections.map((candidate, position) => (
                        <option key={position} value={position}>
                          {candidate.label || `Section ${position + 1}`}
                        </option>
                      ))}
                    </NativeSelect>
                  ) : null
                }
              />
            )}
          </div>
        ))}
      </div>
      <div className="records-layout-add">
        <Button size="sm" variant="secondary" disabled={busy} onClick={() => setSections([...sections, { label: `Section ${sections.length + 1}`, fields: [] }])}>
          <Plus aria-hidden="true" /> Add section
        </Button>
      </div>
    </section>
  );
}

export function RecordSchemaEditor({ model, initialObject }: { model: SchemaEditorModel; initialObject?: string }) {
  const [selected, setSelected] = useState(
    model.objects.some((object) => object.apiName === initialObject) ? initialObject! : model.objects[0]?.apiName ?? "",
  );
  const { busy, notice, submit } = useSchemaSubmit(model.appId);
  const object = model.objects.find((candidate) => candidate.apiName === selected);
  if (!object) {
    return (
      <section className="records-admin-panel">
        <p>This app has no objects yet. Ask Neko in chat to design one.</p>
      </section>
    );
  }
  return (
    <div className="records-schema-editor">
      {model.objects.length > 1 && (
        <Tabs aria-label="Objects">
          {model.objects.map((candidate) => (
            <Tab key={candidate.apiName} selected={candidate.apiName === selected} onClick={() => setSelected(candidate.apiName)}>
              {candidate.pluralLabel}
            </Tab>
          ))}
        </Tabs>
      )}
      <p className="records-schema-note">
        Each change runs as an approved action and appears in History.
        <Link href={`/a/${model.appId}/${object.apiName}`}>Open {object.pluralLabel.toLowerCase()}</Link>
      </p>
      {notice && (
        <div className={`records-schema-notice${notice.tone === "error" ? " is-error" : ""}`} role={notice.tone === "error" ? "alert" : "status"}>
          <span>{notice.text}</span>
          {notice.requestId && <Link href={`/actions/${notice.requestId}`}>View request</Link>}
        </div>
      )}
      <div key={`${model.revision}-${object.apiName}`} className="records-schema-panels">
        <FieldsPanel object={object} objects={model.objects} busy={busy} submit={submit} />
        <ListLayoutPanel object={object} busy={busy} submit={submit} />
        <DetailLayoutPanel object={object} busy={busy} submit={submit} />
      </div>
    </div>
  );
}
