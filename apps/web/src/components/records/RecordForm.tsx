"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import { CheckCircle2, LockKeyhole } from "lucide-react";
import type { RecordViewColumn } from "@neko/records";
import { Button, buttonClassName } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import {
  issueSentence,
  RecordFieldInput,
  submittedValue,
  writeErrorSentence,
} from "./RecordFieldControl";

type SubmitResponse = {
  status?: "executed" | "queued";
  actionRequestId?: string;
  recordId?: string | null;
  policy?: string;
  error?: string;
  issues?: Array<{ field: string; message: string }>;
};

export function RecordForm({
  appId,
  objectApiName,
  objectLabel,
  operation,
  fields,
  initialRow,
  recordId,
}: {
  appId: string;
  objectApiName: string;
  objectLabel: string;
  operation: "create" | "update";
  fields: RecordViewColumn[];
  initialRow?: Record<string, unknown> | null;
  recordId?: string | null;
}) {
  const router = useRouter();
  const [submitting, setSubmitting] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [actionRequestId, setActionRequestId] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const base = `/a/${appId}/${objectApiName}`;

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSubmitting(true);
    setMessage(null);
    setActionRequestId(null);
    setFieldErrors({});
    const formData = new FormData(event.currentTarget);
    const values: Record<string, unknown> = {};
    const expected: Record<string, unknown> = {};
    for (const column of fields) {
      if (column.readOnly || column.kind === "readonly_formula") continue;
      const value = submittedValue(formData, column);
      if (value !== undefined || operation === "update") {
        values[column.apiName] = value ?? null;
      }
      if (operation === "update") {
        expected[column.apiName] = initialRow?.[column.columnName] ?? null;
      }
    }

    try {
      const response = await fetch(`/api/a/${appId}/${objectApiName}/submit`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          operation,
          ...(operation === "update" ? { id: recordId, expected } : {}),
          fields: values,
        }),
      });
      const payload = (await response.json()) as SubmitResponse;
      if (!response.ok) {
        const known = new Set(fields.map((column) => column.apiName));
        const byField: Record<string, string> = {};
        const general: string[] = [];
        for (const issue of payload.issues ?? []) {
          if (known.has(issue.field)) byField[issue.field] ??= issueSentence(issue.message);
          else general.push(issueSentence(issue.message));
        }
        setFieldErrors(byField);
        const fieldCount = Object.keys(byField).length;
        setMessage(
          general.join(" ") ||
            (fieldCount > 0
              ? `Fix the ${fieldCount === 1 ? "highlighted field" : `${fieldCount} highlighted fields`}, then submit again.`
              : writeErrorSentence(payload.error, "The record could not be submitted.")),
        );
        if (fieldCount > 0) {
          document.getElementById(`record-field-${Object.keys(byField)[0]}`)?.focus();
        }
        setActionRequestId(payload.actionRequestId ?? null);
        return;
      }
      if (payload.status === "queued") {
        setMessage(
          "The governed action is still queued. No success was assumed; you can inspect its status below.",
        );
        setActionRequestId(payload.actionRequestId ?? null);
        return;
      }
      const destinationId = payload.recordId ?? recordId;
      if (!destinationId) {
        setMessage(
          "The action executed, but the worker did not return a record ID.",
        );
        setActionRequestId(payload.actionRequestId ?? null);
        return;
      }
      router.push(`${base}/${encodeURIComponent(destinationId)}`);
      router.refresh();
    } catch {
      setMessage(
        "The records service could not be reached. The form was not reported as successful.",
      );
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form className="records-form-card" onSubmit={submit}>
      <div className="records-form-grid">
        {fields.map((column) => (
          <div
            className={`records-form-field${column.kind === "textarea" ? " is-wide" : ""}${fieldErrors[column.apiName] ? " is-invalid" : ""}`}
            key={column.apiName}
          >
            <label htmlFor={`record-field-${column.apiName}`}>
              {column.label}
              {column.required && <span aria-label="required"> *</span>}
            </label>
            <RecordFieldInput
              appId={appId}
              column={column}
              value={initialRow?.[column.columnName]}
              invalid={Boolean(fieldErrors[column.apiName])}
            />
            {fieldErrors[column.apiName] && (
              <span
                className="records-field-error"
                id={`record-field-${column.apiName}-error`}
              >
                {fieldErrors[column.apiName]}
              </span>
            )}
            {column.kind === "reference" && (
              <small>
                Search the permitted target object by name; the selected record
                ID is submitted.
              </small>
            )}
            {(column.readOnly || column.kind === "readonly_formula") && (
              <small>
                Calculated or source-managed value; shown for context only.
              </small>
            )}
            {column.kind === "multipicklist" && (
              <small>Use Ctrl or Command to select more than one value.</small>
            )}
          </div>
        ))}
      </div>
      {fields.length === 0 && (
        <div className="records-form-empty">
          This object has no editable fields.
        </div>
      )}
      {message && (
        <div className="records-form-message" role="status">
          <span>{message}</span>
          {actionRequestId && (
            <Link href={`/actions/${actionRequestId}`}>
              View action request
            </Link>
          )}
        </div>
      )}
      <footer className="records-form-footer">
        <p>
          <LockKeyhole aria-hidden="true" />
          Submitted through action policy, registry validation, and durable
          audit history.
        </p>
        <Link
          className={buttonClassName({
            size: "sm",
            className: "records-form-cancel",
          })}
          href={recordId ? `${base}/${encodeURIComponent(recordId)}` : base}
        >
          Cancel
        </Link>
        <Button
          className="records-primary-action"
          variant="primary"
          size="sm"
          type="submit"
          disabled={submitting || fields.length === 0}
        >
          {submitting ? (
            <Spinner className="records-spin" aria-hidden="true" />
          ) : (
            <CheckCircle2 aria-hidden="true" />
          )}
          {submitting
            ? "Submitting…"
            : `${operation === "create" ? "Create" : "Save"} ${objectLabel.toLowerCase()}`}
        </Button>
      </footer>
    </form>
  );
}
