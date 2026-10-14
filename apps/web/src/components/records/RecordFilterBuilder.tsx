"use client";

import { useState, type FormEvent, type ReactNode } from "react";
import type { RecordListFilter } from "@neko/records";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Field, Input, NativeSelect } from "@/components/ui/field";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import {
  filterConditions,
  filterLeaf,
  filterOptions,
  leafCondition,
  type RecordFilterField,
} from "@/lib/records-filter";
import { RecordReferenceInput } from "./RecordReferenceInput";

function FilterEditor({
  appId,
  fields,
  initial,
  onApply,
}: {
  appId: string;
  fields: RecordFilterField[];
  initial?: RecordListFilter;
  onApply: (leaf: RecordListFilter) => void;
}) {
  const start = initial ? leafCondition(initial) : null;
  const [fieldName, setFieldName] = useState(initial?.field ?? fields[0]?.apiName ?? "");
  const field = fields.find((candidate) => candidate.apiName === fieldName);
  const conditions = filterConditions(field);
  const [conditionValue, setConditionValue] = useState(start?.condition ?? conditions[0]?.value ?? "");
  const condition = conditions.find((candidate) => candidate.value === conditionValue) ?? conditions[0];
  const [text, setText] = useState(typeof start?.raw === "string" ? start.raw : "");
  const [selected, setSelected] = useState<string[]>(Array.isArray(start?.raw) ? start.raw : []);
  const [error, setError] = useState<string | null>(null);

  function changeField(next: string) {
    setFieldName(next);
    const nextField = fields.find((candidate) => candidate.apiName === next);
    setConditionValue(filterConditions(nextField)[0]?.value ?? "");
    setText("");
    setSelected([]);
    setError(null);
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    event.stopPropagation();
    if (!field || !condition) return;
    const raw = condition.input === "options" ? selected : condition.input === "boolean" ? text || "true" : text;
    const leaf = filterLeaf(field, condition.value, raw);
    if (!leaf) {
      setError(condition.input === "options" ? "Choose at least one value." : "Enter a value.");
      return;
    }
    onApply(leaf);
  }

  let valueControl: ReactNode = null;
  const input = condition?.input ?? "none";
  if (input === "options") {
    const options = filterOptions(field);
    valueControl = (
      <fieldset className="records-filter-options" aria-label="Values">
        {options.map((option) => (
          <Checkbox
            key={option.value}
            label={option.label}
            checked={selected.includes(option.value)}
            onCheckedChange={(checked) =>
              setSelected((current) =>
                checked === true
                  ? [...current, option.value]
                  : current.filter((item) => item !== option.value),
              )
            }
          />
        ))}
      </fieldset>
    );
  } else if (input === "boolean") {
    valueControl = (
      <NativeSelect id="records-filter-value" value={text || "true"} onChange={(event) => setText(event.target.value)}>
        <option value="true">Yes</option>
        <option value="false">No</option>
      </NativeSelect>
    );
  } else if (input === "reference" && field) {
    valueControl = (
      <RecordReferenceInput
        appId={appId}
        targets={field.referenceTargets ?? []}
        label="Value"
        id="records-filter-value"
        value={text}
        onChange={setText}
      />
    );
  } else if (input !== "none") {
    valueControl = (
      <Input
        id="records-filter-value"
        value={text}
        onChange={(event) => setText(event.target.value)}
        type={
          input === "number" || input === "days"
            ? "number"
            : input === "date"
              ? "date"
              : input === "datetime"
                ? "datetime-local"
                : "text"
        }
        min={input === "days" ? 1 : undefined}
        max={input === "days" ? 3650 : undefined}
        step={input === "number" ? "any" : undefined}
        autoFocus={Boolean(initial)}
      />
    );
  }

  return (
    <form onSubmit={submit} className="grid gap-3" aria-label="Filter">
      <Field label="Field" htmlFor="records-filter-field">
        <NativeSelect id="records-filter-field" value={fieldName} onChange={(event) => changeField(event.target.value)}>
          {fields.map((candidate) => (
            <option value={candidate.apiName} key={candidate.apiName}>
              {candidate.label}
            </option>
          ))}
        </NativeSelect>
      </Field>
      <Field label="Condition" htmlFor="records-filter-condition">
        <NativeSelect
          id="records-filter-condition"
          value={condition?.value ?? ""}
          onChange={(event) => {
            setConditionValue(event.target.value);
            setError(null);
          }}
        >
          {conditions.map((candidate) => (
            <option value={candidate.value} key={candidate.value}>
              {candidate.label}
            </option>
          ))}
        </NativeSelect>
      </Field>
      {valueControl && (
        <Field
          label={input === "days" ? "Days" : "Value"}
          htmlFor={input === "options" ? undefined : "records-filter-value"}
          error={error}
        >
          {valueControl}
        </Field>
      )}
      <Button variant="primary" type="submit" disabled={!field}>
        {initial ? "Update filter" : "Apply filter"}
      </Button>
    </form>
  );
}

export function RecordFilterPopover({
  appId,
  fields,
  initial,
  trigger,
  onApply,
}: {
  appId: string;
  fields: RecordFilterField[];
  initial?: RecordListFilter;
  trigger: ReactNode;
  onApply: (leaf: RecordListFilter) => void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>{trigger}</PopoverTrigger>
      <PopoverContent aria-label={initial ? "Edit filter" : "Add filter"}>
        {open && (
          <FilterEditor
            appId={appId}
            fields={fields}
            initial={initial}
            onApply={(leaf) => {
              setOpen(false);
              onApply(leaf);
            }}
          />
        )}
      </PopoverContent>
    </Popover>
  );
}
