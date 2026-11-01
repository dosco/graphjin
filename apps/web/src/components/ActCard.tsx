"use client";

import { useRouter } from "next/navigation";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/field";
import { Badge, type BadgeVariant } from "@/components/ui/badge";
import { cn } from "@/lib/cn";
import { formatSavedShort } from "@/lib/hours-saved";
import { ActionChanges } from "@/components/ActionChanges";
import { actionApprovalAttribution } from "@/lib/action-copy";

export type ActRowTone = "good" | "watch" | "action";

export type ActRowData = {
  id: string;
  tone: ActRowTone;
  headline: string;
  detail?: string | null;
  target?: string | null;
  rejectionReason?: string | null;
  approverPhrase?: string | null;
  status: string;
  kind?: string;
  payload?: unknown;
  /** Realized minutes saved — shown on fired receipts that carry an estimate. */
  minutesSaved?: number | null;
};

export type ActCardData = {
  runId: string | null;
  runAt: string;
  trigger?: string | null;
  state: "live" | "awaiting" | "rejected" | "failed" | "needs_check";
  workflowName?: string | null;
  rows: ActRowData[];
};

function formatTime(iso: string): string {
  return new Date(iso).toLocaleTimeString("en-IN", {
    hour: "numeric",
    minute: "2-digit",
    hour12: true,
  }).toUpperCase();
}

const STATE_LABEL: Record<ActCardData["state"], string> = {
  live: "Auto-response live",
  awaiting: "Waiting for you",
  rejected: "Rejected",
  failed: "Failed",
  needs_check: "Needs checking",
};

const STATE_PILL_VARIANT: Record<ActCardData["state"], BadgeVariant> = {
  live: "live",
  awaiting: "watch",
  rejected: "muted",
  failed: "danger",
  needs_check: "watch",
};

const TONE_DOT: Record<ActRowTone, string> = {
  good: "bg-success",
  watch: "bg-watch",
  action: "bg-danger",
};

export default function ActCard({
  data,
  index,
  focusedRowId,
  busyRowId,
  rejectingRowId,
  rejectReason,
  onRejectReasonChange,
  onCancelReject,
  onSubmitReject,
  onFocusRow,
  onApproveRow,
  onBeginRejectRow,
  rowRef,
}: {
  data: ActCardData;
  index: number;
  focusedRowId?: string | null;
  busyRowId?: string | null;
  rejectingRowId?: string | null;
  rejectReason?: string;
  onRejectReasonChange?: (v: string) => void;
  onCancelReject?: () => void;
  onSubmitReject?: () => void;
  onFocusRow?: (id: string) => void;
  onApproveRow?: (id: string) => void;
  onBeginRejectRow?: (id: string) => void;
  rowRef?: (id: string, el: HTMLLIElement | null) => void;
}) {
  const router = useRouter();
  return (
    <Card
      as="article"
      className={cn(
        "grid gap-3 act-card",
        data.state === "awaiting" && "border-watch/35",
      )}
      style={{ animation: `fadeUp 0.4s ease ${index * 0.04}s both` }}
    >
      <header className="flex items-center justify-between gap-3">
        <Badge variant={STATE_PILL_VARIANT[data.state]}>
          {STATE_LABEL[data.state]}
        </Badge>
        <span className="text-ui-caption tabular-nums text-text3">
          {formatTime(data.runAt)}
        </span>
      </header>

      {data.trigger && (
        <div className="px-3.5 py-3 border border-watch/30 rounded-xl bg-watch/10 text-text font-semibold text-sm leading-snug">
          {data.trigger}
        </div>
      )}

      <ul className="list-none m-0 p-0 grid gap-2">
        {data.rows.map((row) => {
          const isFocused = focusedRowId === row.id;
          const isBusy = busyRowId === row.id;
          const isRejecting = rejectingRowId === row.id;
          const isPending = data.state === "awaiting";

          return (
            <li
              key={row.id}
              ref={(el) => rowRef?.(row.id, el)}
              className={cn(
                "flex items-start gap-3 px-3.5 py-3 rounded-xl border border-border bg-card cursor-pointer",
                "transition-[border-color,box-shadow] duration-150",
                "hover:border-text3",
                "focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2",
                isFocused && "border-text3 bg-neutral-soft",
              )}
              onClick={() => {
                if (isPending) onFocusRow?.(row.id);
                else router.push(`/actions/${row.id}`);
              }}
              role="button"
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  if (isPending) onFocusRow?.(row.id);
                  else router.push(`/actions/${row.id}`);
                }
              }}
            >
              <span
                className={cn(
                  "w-2.5 h-2.5 mt-1.5 rounded-full flex-none",
                  TONE_DOT[row.tone],
                )}
              />
              <div className="flex-1 min-w-0 grid gap-1">
                <p className="m-0 text-ui-body font-semibold text-text leading-snug tracking-[-0.005em]">
                  {row.headline}
                </p>
                {row.target && (
                  <p className="m-0 font-mono text-xs text-text2 break-all">
                    {row.target}
                  </p>
                )}
                {row.detail && (
                  <p className="m-0 text-ui-body-sm leading-[1.55] text-text2">
                    {row.detail}
                  </p>
                )}
                {isPending ? (
                  <ActionChanges payload={row.payload} limit={3} className="mt-1.5" />
                ) : null}
                {row.rejectionReason && (
                  <p className="m-0 text-ui-body-sm leading-[1.55] text-text2 italic">
                    {row.rejectionReason}
                  </p>
                )}
                {data.state === "live" && (row.approverPhrase || (row.minutesSaved ?? 0) > 0) && (
                  <p className="mt-1 text-ui-caption text-text3 flex items-center gap-2 flex-wrap">
                    {row.approverPhrase && (
                      <span>
                        <span className="text-text2 font-medium">
                          {actionApprovalAttribution(row.approverPhrase)}
                        </span>
                      </span>
                    )}
                    {(row.minutesSaved ?? 0) > 0 && (
                      <span
                        className="tabular-nums text-success-ink bg-success-soft border border-success-mid/30 rounded-full px-1.5 py-px"
                        title="Estimated human time saved"
                      >
                        {formatSavedShort(row.minutesSaved as number)} saved
                      </span>
                    )}
                  </p>
                )}

                {isPending && isRejecting ? (
                  <div
                    className="flex flex-col gap-2 mt-2"
                    onClick={(e) => e.stopPropagation()}
                  >
                    <Textarea
                      className="min-h-[50px] text-ui-body-sm"
                      value={rejectReason ?? ""}
                      placeholder="Why are you rejecting this? (optional)"
                      onChange={(e) => onRejectReasonChange?.(e.target.value)}
                      autoFocus
                      rows={2}
                    />
                    <div className="flex gap-2">
                      <RowButton tone="destructive" disabled={isBusy} onClick={onSubmitReject}>
                        Confirm reject
                      </RowButton>
                      <RowButton disabled={isBusy} onClick={onCancelReject}>
                        Cancel
                      </RowButton>
                    </div>
                  </div>
                ) : isPending ? (
                  <div className="flex gap-2 mt-2">
                    <RowButton
                      tone="primary"
                      disabled={isBusy}
                      onClick={(e) => {
                        e.stopPropagation();
                        onApproveRow?.(row.id);
                      }}
                    >
                      Approve
                    </RowButton>
                    <RowButton
                      disabled={isBusy}
                      onClick={(e) => {
                        e.stopPropagation();
                        onBeginRejectRow?.(row.id);
                      }}
                    >
                      Reject
                    </RowButton>
                    <a
                      className="ml-auto self-center px-1.5 py-1 text-xs font-semibold text-text2 no-underline opacity-70 transition-opacity duration-150 hover:opacity-100 hover:text-text hover:underline underline-offset-2"
                      href={`/actions/${row.id}`}
                      onClick={(e) => e.stopPropagation()}
                      title="Open the full lineage: trigger, workflow, payload"
                    >
                      Why
                    </a>
                  </div>
                ) : null}
              </div>
            </li>
          );
        })}
      </ul>
    </Card>
  );
}

type RowButtonProps = {
  tone?: "primary" | "destructive";
  disabled?: boolean;
  onClick?: (e: React.MouseEvent<HTMLButtonElement>) => void;
  children: React.ReactNode;
};

function RowButton({ tone, disabled, onClick, children }: RowButtonProps) {
  return (
    <Button
      size="sm"
      variant={
        tone === "primary"
          ? "primary"
          : tone === "destructive"
            ? "danger"
            : "secondary"
      }
      disabled={disabled}
      onClick={onClick}
    >
      {children}
    </Button>
  );
}
