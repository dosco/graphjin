"use client";

import { useState, type ReactNode } from "react";
import type { RecordViewColumn } from "@neko/records";
import { Checkbox } from "@/components/ui/checkbox";
import { Input, NativeSelect, Textarea } from "@/components/ui/field";
import { RecordReferenceInput } from "./RecordReferenceInput";

function picklistOptions(
  column: RecordViewColumn,
): Array<{ value: string; label: string }> {
  return (column.picklistValues ?? []).flatMap((option) => {
    if (typeof option === "string") return [{ value: option, label: option }];
    if (option && typeof option === "object" && !Array.isArray(option)) {
      const entry = option as Record<string, unknown>;
      if (typeof entry.value === "string") {
        return [
          {
            value: entry.value,
            label: typeof entry.label === "string" ? entry.label : entry.value,
          },
        ];
      }
    }
    return [];
  });
}

function stringValue(value: unknown): string {
  return value === null || value === undefined ? "" : String(value);
}

function datetimeLocalValue(value: unknown): string {
  const date = new Date(stringValue(value));
  if (Number.isNaN(date.getTime())) return "";
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
  return local.toISOString().slice(0, 16);
}

function multiValue(value: unknown): string[] {
  if (Array.isArray(value)) return value.map(String);
  if (typeof value === "string") {
    try {
      const parsed = JSON.parse(value) as unknown;
      return Array.isArray(parsed) ? parsed.map(String) : [];
    } catch {
      return value ? [value] : [];
    }
  }
  return [];
}

function FieldControl({
  column,
  value,
  invalid,
}: {
  column: RecordViewColumn;
  value: unknown;
  invalid?: boolean;
}): ReactNode {
  const common = {
    id: `record-field-${column.apiName}`,
    name: column.apiName,
    required: column.required,
    "aria-invalid": invalid || undefined,
    "aria-describedby": invalid ? `record-field-${column.apiName}-error` : undefined,
  };
  if (column.readOnly || column.kind === "readonly_formula") {
    return (
      <Input
        {...common}
        type="text"
        value={stringValue(value)}
        disabled
        readOnly
      />
    );
  }
  if (column.kind === "textarea") {
    return <Textarea {...common} defaultValue={stringValue(value)} rows={5} />;
  }
  if (column.kind === "boolean") {
    return (
      <Checkbox {...common} label="Enabled" defaultChecked={value === true} />
    );
  }
  if (column.kind === "picklist") {
    return (
      <NativeSelect {...common} defaultValue={stringValue(value)}>
        {!column.required && <option value="">Not set</option>}
        {picklistOptions(column).map((option) => (
          <option value={option.value} key={option.value}>
            {option.label}
          </option>
        ))}
      </NativeSelect>
    );
  }
  if (column.kind === "multipicklist") {
    return (
      <NativeSelect
        {...common}
        multiple
        defaultValue={multiValue(value)}
        size={Math.min(5, Math.max(3, picklistOptions(column).length))}
      >
        {picklistOptions(column).map((option) => (
          <option value={option.value} key={option.value}>
            {option.label}
          </option>
        ))}
      </NativeSelect>
    );
  }
  const type =
    column.kind === "email"
      ? "email"
      : column.kind === "phone"
        ? "tel"
        : column.kind === "url"
          ? "url"
          : column.kind === "date"
            ? "date"
            : column.kind === "datetime"
              ? "datetime-local"
              : ["integer", "decimal", "currency", "percent"].includes(
                    column.kind,
                  )
                ? "number"
                : "text";
  return (
    <Input
      {...common}
      type={type}
      defaultValue={
        column.kind === "datetime"
          ? datetimeLocalValue(value)
          : stringValue(value)
      }
      {...(column.kind === "integer" ? { step: 1 } : {})}
      {...(["decimal", "currency", "percent"].includes(column.kind)
        ? { step: column.scale === null ? "any" : 10 ** -column.scale }
        : {})}
      {...(column.kind === "reference" ? { spellCheck: false } : {})}
    />
  );
}


function ReferenceControl({
  appId,
  column,
  value,
}: {
  appId: string;
  column: RecordViewColumn;
  value: unknown;
}) {
  const [current, setCurrent] = useState(stringValue(value));
  return (
    <RecordReferenceInput
      appId={appId}
      targets={column.referenceTargets ?? []}
      label={column.label}
      id={`record-field-${column.apiName}`}
      name={column.apiName}
      required={column.required}
      value={current}
      onChange={setCurrent}
    />
  );
}

/** The input for one record field, named by its API name so FormData can read it. */
export function RecordFieldInput({
  appId,
  column,
  value,
  invalid,
}: {
  appId: string;
  column: RecordViewColumn;
  value: unknown;
  invalid?: boolean;
}) {
  if (column.kind === "reference" && !column.readOnly) {
    return <ReferenceControl appId={appId} column={column} value={value} />;
  }
  return <FieldControl column={column} value={value} invalid={invalid} />;
}

/** Turns a write error from the records engine into a sentence with the next step. */
export function writeErrorSentence(message: string | undefined, fallback: string): string {
  if (!message) return fallback;
  if (/changed after it was read/i.test(message)) {
    return "Someone changed this record after you opened it. Refresh the page and try again.";
  }
  return issueSentence(message);
}

/** Turns a validation message such as "is required" into a sentence for the field. */
export function issueSentence(message: string): string {
  const text = message.trim();
  if (!text) return "This value is not valid.";
  const sentence = text[0]!.toUpperCase() + text.slice(1);
  return /[.!?]$/.test(sentence) ? sentence : `${sentence}.`;
}

export function submittedValue(formData: FormData, column: RecordViewColumn): unknown {
  if (column.kind === "boolean") return formData.has(column.apiName);
  if (column.kind === "multipicklist") {
    return formData.getAll(column.apiName).map(String);
  }
  const raw = formData.get(column.apiName);
  const value = typeof raw === "string" ? raw : "";
  if (value === "") return undefined;
  if (column.kind === "integer") return Number(value);
  if (column.kind === "datetime") return new Date(value).toISOString();
  return value;
}

