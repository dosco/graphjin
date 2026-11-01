"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import {
  OUTCOME_LABEL,
  actionOutcome,
  describeOutcome,
  systemForActionKind,
} from "@/lib/action-outcome";

export const UNDO_WINDOW_MS = 5000;
const FOLLOW_INTERVAL_MS = 2000;
const FOLLOW_LIMIT_MS = 120_000;
const TERMINAL = new Set(["executed", "failed", "rejected"]);

type ActionDetail = {
  actionRequest: { status: string; kind: string };
  executions?: Array<{ status: string; result: unknown; error: string | null }>;
};

function resultStatus(result: unknown): string | undefined {
  if (!result || typeof result !== "object") return undefined;
  const status = (result as { status?: unknown }).status;
  return typeof status === "string" ? status : undefined;
}

// After an approval is sent, the worker runs the change. Follow the
// request until it ends and tell the operator how it ended, so a failure
// never hides in a tab they are not looking at.
async function followOutcome(id: string, label: string, open: (href: string) => void) {
  const view = { label: "View", onClick: () => open(`/actions/${id}`) };
  const toastId = toast.loading("Applying the change", { description: label });
  const started = Date.now();
  while (Date.now() - started < FOLLOW_LIMIT_MS) {
    await new Promise((resolve) => window.setTimeout(resolve, FOLLOW_INTERVAL_MS));
    let detail: ActionDetail;
    try {
      const res = await fetch(`/api/action-requests/${id}`, { cache: "no-store" });
      if (!res.ok) continue;
      detail = (await res.json()) as ActionDetail;
    } catch {
      continue;
    }
    const { status, kind } = detail.actionRequest;
    if (!TERMINAL.has(status)) continue;
    const execution = detail.executions?.[0];
    const outcome = actionOutcome(status, resultStatus(execution?.result) ?? execution?.status);
    const sentence = describeOutcome(outcome, execution?.error, systemForActionKind(kind));
    if (outcome === "completed" || outcome === "approved") {
      toast.success("Change applied", { id: toastId, description: label, action: view, duration: 6000 });
    } else if (outcome === "failed") {
      toast.error("Change failed", { id: toastId, description: sentence ?? label, action: view, duration: Infinity });
    } else {
      toast.warning(OUTCOME_LABEL[outcome], { id: toastId, description: sentence ?? label, action: view, duration: Infinity });
    }
    return;
  }
  toast("Still applying", {
    id: toastId,
    description: "OpenNeko is still working on this change. Approvals shows the result when it ends.",
    action: view,
    duration: 10000,
  });
}

type Decision = "approve" | "reject";
type Pending = { decision: Decision; label: string; reason?: string; timer: number };

function send(id: string, decision: Decision, reason?: string, keepalive = false) {
  return fetch(`/api/action-requests/${id}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ decision, reason }),
    keepalive,
  });
}

// Approve and reject take effect in the list at once. The request waits
// for the undo window, so a mistaken click costs nothing.
export function useApprovalDecisions(onSettled: () => void | Promise<void>) {
  const [hiddenIds, setHiddenIds] = useState<ReadonlySet<string>>(new Set());
  const [rejectingId, setRejectingId] = useState<string | null>(null);
  const [rejectReason, setRejectReason] = useState("");
  const pending = useRef(new Map<string, Pending>());
  const settledRef = useRef(onSettled);
  const router = useRouter();
  const openRef = useRef((href: string) => router.push(href));

  useEffect(() => {
    openRef.current = (href: string) => router.push(href);
  }, [router]);

  useEffect(() => {
    settledRef.current = onSettled;
  }, [onSettled]);

  useEffect(() => {
    const queue = pending.current;
    const flush = (follow: boolean) => {
      for (const [id, p] of queue) {
        window.clearTimeout(p.timer);
        const sent = send(id, p.decision, p.reason, true);
        if (follow && p.decision === "approve") {
          void sent.then((res) => {
            if (res.ok) void followOutcome(id, p.label, (href) => openRef.current(href));
          });
        }
      }
      queue.clear();
    };
    const onPageHide = () => flush(false);
    window.addEventListener("pagehide", onPageHide);
    return () => {
      window.removeEventListener("pagehide", onPageHide);
      flush(true);
    };
  }, []);

  const setHidden = useCallback((id: string, hidden: boolean) => {
    setHiddenIds((current) => {
      const next = new Set(current);
      if (hidden) next.add(id);
      else next.delete(id);
      return next;
    });
  }, []);

  const decide = useCallback(
    (id: string, decision: Decision, label: string, reason?: string) => {
      setHidden(id, true);
      const timer = window.setTimeout(async () => {
        pending.current.delete(id);
        try {
          const res = await send(id, decision, reason);
          if (!res.ok) throw new Error(`HTTP ${res.status}`);
          if (decision === "approve") void followOutcome(id, label, (href) => openRef.current(href));
        } catch {
          toast.error(`OpenNeko could not record the ${decision === "approve" ? "approval" : "rejection"}. Try again.`);
        }
        await settledRef.current();
        setHidden(id, false);
      }, UNDO_WINDOW_MS);
      pending.current.set(id, { decision, label, reason, timer });
      toast(decision === "approve" ? "Approved" : "Rejected", {
        description: label,
        duration: UNDO_WINDOW_MS,
        action: {
          label: "Undo",
          onClick: () => {
            const p = pending.current.get(id);
            if (!p) return;
            window.clearTimeout(p.timer);
            pending.current.delete(id);
            setHidden(id, false);
          },
        },
      });
    },
    [setHidden],
  );

  const approve = useCallback((id: string, label: string) => decide(id, "approve", label), [decide]);

  const beginReject = useCallback((id: string) => {
    setRejectingId(id);
    setRejectReason("");
  }, []);

  const cancelReject = useCallback(() => {
    setRejectingId(null);
    setRejectReason("");
  }, []);

  const submitReject = useCallback(
    (label: string) => {
      if (!rejectingId) return;
      const reason = rejectReason.trim() || undefined;
      const id = rejectingId;
      setRejectingId(null);
      setRejectReason("");
      decide(id, "reject", label, reason);
    },
    [decide, rejectReason, rejectingId],
  );

  return {
    hiddenIds,
    rejectingId,
    rejectReason,
    setRejectReason,
    approve,
    beginReject,
    cancelReject,
    submitReject,
  };
}
