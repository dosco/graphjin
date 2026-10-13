"use client";

import { Check, Loader2, ShieldCheck, X } from "lucide-react";
import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/field";
import { ToolDetail } from "./work-tool-detail";
import type { ApprovalItem, ToolItem } from "./work-types";

type ActionRequestSnapshot = {
  status: string;
  result: ApprovalItem["result"];
};

async function fetchActionRequestSnapshot(
  actionRequestId: string,
  actionKind: string,
): Promise<ActionRequestSnapshot | null> {
  const res = await fetch(`/api/action-requests/${actionRequestId}`, {
    cache: "no-store",
  });
  if (!res.ok) return null;
  const body = (await res.json()) as {
    actionRequest?: {
      status?: string;
      rejectionReason?: string | null;
      failureReason?: string | null;
    };
    executions?: Array<{
      status?: string;
      result?: Record<string, unknown> | null;
      externalRef?: string | null;
      commandOrOperation?: string | null;
      error?: string | null;
    }>;
  };
  const status = body.actionRequest?.status ?? "pending_approval";
  const execution = body.executions?.[0];
  let result: ApprovalItem["result"] = null;
  if (status === "executed") {
    result = {
      type: "action_request_result",
      action_request_id: actionRequestId,
      kind: actionKind,
      status: execution?.status === "partially_applied" || execution?.status === "reconcile_required"
        ? execution.status
        : "succeeded",
      outcome: {
        result: execution?.result ?? null,
        externalRef: execution?.externalRef ?? null,
        commandOrOperation: execution?.commandOrOperation ?? null,
      },
    };
  } else if (status === "failed") {
    result = {
      type: "action_request_result",
      action_request_id: actionRequestId,
      kind: actionKind,
      status: "failed",
      error: body.actionRequest?.failureReason ?? execution?.error ?? "Action execution failed.",
    };
  } else if (status === "rejected") {
    result = {
      type: "action_request_result",
      action_request_id: actionRequestId,
      kind: actionKind,
      status: "rejected",
      ...(body.actionRequest?.rejectionReason
        ? { rejection_reason: body.actionRequest.rejectionReason }
        : {}),
    };
  }
  return { status, result };
}

export function ActionApprovalRow({
  threadId,
  runId,
  tool,
}: {
  threadId: string;
  runId: string;
  tool: ToolItem;
}) {
  const approval = tool.approval!;
  const [busy, setBusy] = useState(false);
  const [open, setOpen] = useState(false);
  const [rejectMode, setRejectMode] = useState(false);
  const [rejectReason, setRejectReason] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);
  const [localDecision, setLocalDecision] = useState<
    "approve" | "reject" | null
  >(null);
  const [localStatus, setLocalStatus] = useState<string | null>(null);
  const [localResult, setLocalResult] =
    useState<ApprovalItem["result"]>(null);

  useEffect(() => {
    if (approval.result) return;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;
    const sync = async () => {
      const snapshot = await fetchActionRequestSnapshot(
        approval.actionRequestId,
        approval.actionKind,
      );
      if (cancelled || !snapshot) return;
      setLocalStatus(snapshot.status);
      setLocalResult(snapshot.result);
      if (snapshot.status === "approved") {
        timer = setTimeout(() => void sync(), 1_000);
      }
    };
    void sync();
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [
    approval.actionKind,
    approval.actionRequestId,
    approval.result,
    localDecision,
  ]);

  const headline =
    approval.intent ??
    approval.summary ??
    `Agent wants to run "${approval.actionKind}".`;
  const effectiveResult = approval.result ?? localResult;
  const pending =
    approval.decision === "pending_approval" &&
    !effectiveResult &&
    !localDecision &&
    (localStatus === null || localStatus === "pending_approval");
  const processing =
    !effectiveResult &&
    (approval.decision === "auto_approved" ||
      localDecision === "approve" ||
      localStatus === "approved");
  const failed = effectiveResult?.status === "failed";
  const partlyApplied = effectiveResult?.status === "partially_applied";
  const needsReconciliation = effectiveResult?.status === "reconcile_required";
  const rejected = effectiveResult?.status === "rejected";
  const succeeded = effectiveResult?.status === "succeeded";
  const visualStatus = failed || partlyApplied || needsReconciliation
    ? "failed"
    : rejected
      ? "rejected"
      : succeeded
        ? "done"
        : processing
          ? "running"
          : "approval";
  const stateLabel = partlyApplied
    ? "Partly applied"
    : needsReconciliation
      ? "Needs checking"
      : failed
        ? "Failed"
        : rejected
          ? "Rejected"
          : succeeded
            ? "Done"
            : processing
              ? "Running"
              : "Approval required";
  const summary = partlyApplied
    ? "Some changes were applied. Review the action details before retrying."
    : needsReconciliation
      ? "The outcome is uncertain. Review the action details before retrying."
      : failed
        ? (effectiveResult?.error ?? "The action failed.")
        : rejected
          ? (effectiveResult?.rejection_reason ?? "The request was rejected.")
          : headline;
  const hasDetail =
    tool.input !== undefined ||
    tool.deltas.length > 0 ||
    tool.end?.result !== undefined ||
    tool.end?.error !== undefined;

  async function decide(decision: "approve" | "reject") {
    if (!runId) {
      setLocalError("Run not ready yet — try again in a moment.");
      return;
    }
    setBusy(true);
    setLocalError(null);
    try {
      const res = await fetch(
        `/api/work/threads/${threadId}/runs/${runId}/approve-action`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            actionRequestId: approval.actionRequestId,
            decision,
            ...(decision === "reject" && rejectReason
              ? { rejectionReason: rejectReason }
              : {}),
          }),
        },
      );
      if (!res.ok) {
        if (res.status === 409) {
          const snapshot = await fetchActionRequestSnapshot(
            approval.actionRequestId,
            approval.actionKind,
          );
          if (snapshot) {
            setLocalStatus(snapshot.status);
            setLocalResult(snapshot.result);
            setLocalDecision(decision);
            return;
          }
        }
        const body = (await res.json().catch(() => ({}))) as { error?: string };
        setLocalError(
          body.error ?? `Request failed (${res.status} ${res.statusText})`,
        );
        return;
      }
      const body = (await res.json().catch(() => ({}))) as {
        status?: string;
        decision?: string;
      };
      setLocalStatus(
        body.status ??
          (decision === "approve" ? "approved" : "rejected"),
      );
      setLocalDecision(decision);
      setRejectMode(false);
    } catch (e) {
      setLocalError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div
      className={`work-tool-row work-action-row work-action-row-${visualStatus}`}
    >
      <div className="work-tool-row-head work-action-row-head">
        <span
          className={`work-tool-row-icon work-tool-row-icon-${visualStatus}`}
          aria-hidden="true"
        >
          {processing ? (
            <Loader2 className="work-status-spin" size={12} />
          ) : failed || partlyApplied || needsReconciliation || rejected ? (
            <X size={12} />
          ) : succeeded ? (
            <Check size={12} />
          ) : (
            <ShieldCheck size={12} />
          )}
        </span>
        <span className="work-action-row-copy">
          <span className="work-tool-row-name">
            {approvalActionLabel(approval.actionKind)}
          </span>
          <span className="work-tool-row-subtitle">{summary}</span>
        </span>
        <span className={`work-action-row-state is-${visualStatus}`}>
          {stateLabel}
        </span>
        {pending && !rejectMode ? (
          <span className="work-action-row-actions">
            <Button
              size="sm"
              variant="primary"
              disabled={busy}
              onClick={() => decide("approve")}
              className="work-action-button is-primary"
            >
              {busy ? "Approving…" : "Approve"}
            </Button>
            <Button
              size="sm"
              disabled={busy}
              onClick={() => setRejectMode(true)}
              className="work-action-button"
            >
              Reject
            </Button>
          </span>
        ) : null}
        {hasDetail ? (
          <Button
            size="sm"
            variant="ghost"
            className="work-action-row-detail-toggle"
            onClick={() => setOpen((value) => !value)}
            aria-expanded={open}
          >
            {open ? "Hide details" : "Details"}
          </Button>
        ) : null}
      </div>

      {pending && rejectMode ? (
        <div className="work-action-row-decision">
          <Input
            type="text"
            placeholder="Reason (optional, shown to the agent)"
            value={rejectReason}
            onChange={(event) => setRejectReason(event.target.value)}
            disabled={busy}
          />
          <div className="work-action-row-actions">
            <Button
              size="sm"
              variant="danger"
              disabled={busy}
              onClick={() => decide("reject")}
              className="work-action-button is-danger"
            >
              {busy ? "Rejecting…" : "Confirm reject"}
            </Button>
            <Button
              size="sm"
              disabled={busy}
              onClick={() => {
                setRejectMode(false);
                setRejectReason("");
              }}
              className="work-action-button"
            >
              Cancel
            </Button>
          </div>
        </div>
      ) : null}

      {localError ? (
        <div className="work-action-row-error">{localError}</div>
      ) : null}

      {open ? <ToolDetail tool={tool} /> : null}
    </div>
  );
}

function approvalActionLabel(kind: string): string {
  const known: Record<string, string> = {
    plugin_install: "Install integration",
    plugin_uninstall: "Remove integration",
    web_fetch: "Fetch from web",
    web_search: "Search the web",
    send_slack_message: "Send Slack message",
  };
  if (known[kind]) return known[kind];
  const words = kind
    .replace(/^mcp[_:.-]*/i, "")
    .split(/[_:.-]+/)
    .filter(Boolean);
  if (words.length === 0) return "Run action";
  return words
    .map((word, index) =>
      index === 0
        ? `${word.charAt(0).toUpperCase()}${word.slice(1).toLowerCase()}`
        : word.toLowerCase(),
    )
    .join(" ");
}
