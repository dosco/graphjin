"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type FormEvent, type ReactNode } from "react";
import { Pencil } from "lucide-react";
import type { RecordViewColumn } from "@neko/records";
import { Button, IconButton } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { issueSentence, RecordFieldInput, submittedValue, writeErrorSentence } from "./RecordFieldControl";

type SubmitResponse = {
  status?: "executed" | "queued";
  actionRequestId?: string;
  error?: string;
  issues?: Array<{ field: string; message: string }>;
};

export function RecordDetailField({
  appId,
  objectApiName,
  recordId,
  column,
  value,
  editable,
  children,
}: {
  appId: string;
  objectApiName: string;
  recordId: string;
  column: RecordViewColumn;
  value: unknown;
  editable: boolean;
  children: ReactNode;
}) {
  const router = useRouter();
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [queued, setQueued] = useState<string | null>(null);

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSaving(true);
    setError(null);
    const next = submittedValue(new FormData(event.currentTarget), column);
    try {
      const response = await fetch(
        `/api/a/${encodeURIComponent(appId)}/${encodeURIComponent(objectApiName)}/submit`,
        {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({
            operation: "update",
            id: recordId,
            fields: { [column.apiName]: next ?? null },
            expected: { [column.apiName]: value ?? null },
          }),
        },
      );
      const payload = (await response.json()) as SubmitResponse;
      if (!response.ok) {
        const issue = payload.issues?.find((candidate) => candidate.field === column.apiName) ?? payload.issues?.[0];
        setError(issue ? issueSentence(issue.message) : writeErrorSentence(payload.error, "The change was not saved. Try again."));
        return;
      }
      setEditing(false);
      if (payload.status === "queued") setQueued(payload.actionRequestId ?? "");
      router.refresh();
    } catch {
      setError("The records service did not respond. The change was not saved.");
    } finally {
      setSaving(false);
    }
  }

  const errorId = `record-field-${column.apiName}-error`;
  return (
    <div className={`records-detail-field${editing ? " is-editing" : ""}`}>
      <dt>
        {editing ? <label htmlFor={`record-field-${column.apiName}`}>{column.label}</label> : column.label}
      </dt>
      <dd>
        {editing ? (
          <form
            className="records-inline-edit"
            onSubmit={save}
            onKeyDown={(event) => {
              if (event.key === "Escape" && !saving) {
                setEditing(false);
                setError(null);
              }
            }}
          >
            <RecordFieldInput appId={appId} column={column} value={value} invalid={Boolean(error)} />
            {error && (
              <span className="records-field-error" id={errorId} role="alert">
                {error}
              </span>
            )}
            <div className="records-inline-edit-actions">
              <Button size="sm" variant="primary" type="submit" disabled={saving}>
                {saving && <Spinner className="records-spin" aria-hidden="true" />}
                {saving ? "Saving…" : "Save"}
              </Button>
              <Button
                size="sm"
                variant="ghost"
                disabled={saving}
                onClick={() => {
                  setEditing(false);
                  setError(null);
                }}
              >
                Cancel
              </Button>
            </div>
          </form>
        ) : (
          <div className="records-detail-value">
            <div className="records-detail-value-content">{children}</div>
            {editable && (
              <IconButton
                size="icon-sm"
                variant="ghost"
                label={`Edit ${column.label}`}
                className="records-detail-edit"
                onClick={() => {
                  setQueued(null);
                  setEditing(true);
                }}
              >
                <Pencil aria-hidden="true" />
              </IconButton>
            )}
          </div>
        )}
        {queued !== null && !editing && (
          <span className="records-detail-queued" role="status">
            The change is still running. It shows here when it finishes.{" "}
            {queued && <Link href={`/actions/${queued}`}>View request</Link>}
          </span>
        )}
      </dd>
    </div>
  );
}
