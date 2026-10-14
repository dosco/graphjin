"use client";

import { useEffect, useId, useState } from "react";
import { Input, NativeSelect } from "@/components/ui/field";

type ReferenceOption = { id: string; label: string };

export function RecordReferenceInput({
  appId,
  targets,
  label,
  value,
  onChange,
  id,
  name,
  required,
}: {
  appId: string;
  targets: string[];
  label: string;
  value: string;
  onChange: (value: string) => void;
  id?: string;
  name?: string;
  required?: boolean;
}) {
  const [target, setTarget] = useState(targets[0] ?? "");
  const [options, setOptions] = useState<ReferenceOption[]>([]);
  const listId = `record-reference-${useId()}`;

  useEffect(() => {
    const search = value.trim();
    if (!target || !search) return;
    const controller = new AbortController();
    const timeout = window.setTimeout(() => {
      fetch(
        `/api/a/${encodeURIComponent(appId)}/references/${encodeURIComponent(target)}?q=${encodeURIComponent(search)}`,
        { signal: controller.signal },
      )
        .then(async (response) =>
          response.ok ? ((await response.json()) as { options?: ReferenceOption[] }) : { options: [] },
        )
        .then((payload) => setOptions(payload.options ?? []))
        .catch((error: unknown) => {
          if (!(error instanceof DOMException && error.name === "AbortError")) setOptions([]);
        });
    }, 180);
    return () => {
      window.clearTimeout(timeout);
      controller.abort();
    };
  }, [appId, value, target]);

  return (
    <div className="records-reference-control">
      {targets.length > 1 && (
        <NativeSelect
          aria-label={`${label} target object`}
          value={target}
          onChange={(event) => {
            setTarget(event.target.value);
            onChange("");
            setOptions([]);
          }}
        >
          {targets.map((candidate) => (
            <option value={candidate} key={candidate}>
              {candidate}
            </option>
          ))}
        </NativeSelect>
      )}
      <Input
        id={id}
        name={name}
        required={required}
        value={value}
        onChange={(event) => {
          onChange(event.target.value);
          if (!event.target.value.trim()) setOptions([]);
        }}
        list={listId}
        autoComplete="off"
        role="combobox"
        aria-autocomplete="list"
        aria-controls={listId}
        aria-expanded={options.length > 0}
        aria-label={id ? undefined : label}
        placeholder="Search by record name or enter an ID"
        spellCheck={false}
      />
      <datalist id={listId}>
        {options.map((option) => (
          <option value={option.id} key={option.id}>
            {option.label}
          </option>
        ))}
      </datalist>
    </div>
  );
}
