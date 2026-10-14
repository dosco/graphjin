"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { Trash2 } from "lucide-react";
import { confirmDialog } from "@/components/ConfirmModal";
import { MenuItem, OverflowMenu } from "@/components/ui/overflow-menu";

export function RecordDeleteMenu({
  appId,
  objectApiName,
  objectLabel,
  recordId,
  recordName,
}: {
  appId: string;
  objectApiName: string;
  objectLabel: string;
  recordId: string;
  recordName: string;
}) {
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [requestId, setRequestId] = useState<string | null>(null);

  async function remove() {
    const confirmed = await confirmDialog({
      title: `Delete ${recordName}?`,
      description: `This moves the ${objectLabel.toLowerCase()} to the recycle bin. You can restore it from there.`,
      confirmLabel: `Delete ${objectLabel.toLowerCase()}`,
      destructive: true,
    });
    if (!confirmed) return;
    setBusy(true);
    setMessage(null);
    setRequestId(null);
    try {
      const response = await fetch(
        `/api/a/${encodeURIComponent(appId)}/${encodeURIComponent(objectApiName)}/delete/${encodeURIComponent(recordId)}`,
        { method: "POST" },
      );
      const body = (await response.json()) as { error?: string; status?: "executed" | "queued"; actionRequestId?: string };
      if (!response.ok) {
        setMessage(body.error ?? "The record was not deleted. Try again.");
        setRequestId(body.actionRequestId ?? null);
        return;
      }
      if (body.status === "queued") {
        setMessage("The delete is still running. The record moves to the recycle bin when it finishes.");
        setRequestId(body.actionRequestId ?? null);
        return;
      }
      router.push(`/a/${appId}/${objectApiName}`);
      router.refresh();
    } catch {
      setMessage("The records service did not respond. The record was not deleted.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="records-delete-menu">
      {message && (
        <span role="status" className="records-delete-message">
          {message} {requestId && <Link href={`/actions/${requestId}`}>View request</Link>}
        </span>
      )}
      <OverflowMenu label="Record actions">
        <MenuItem danger disabled={busy} onSelect={() => void remove()}>
          <Trash2 aria-hidden="true" /> Delete {objectLabel.toLowerCase()}
        </MenuItem>
      </OverflowMenu>
    </div>
  );
}
