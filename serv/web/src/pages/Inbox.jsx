import React from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";

import { DataErrorState, EmptyState, LoadingState, PageHeader } from "../components/ui";
import { fetchApproval } from "../services/agent";
import { operatorIdentityKey, useOperatorIdentity } from "../services/identity";
import {
  listNotifications,
  markAllNotificationsSeen,
  markNotificationSeen,
  notificationCountKey,
  notificationsKey,
} from "../services/notifications";
import { ApprovalCard } from "./AgentChat";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

// Inbox lists what GraphJin needs you to know about: changes waiting for your
// approval, answers routines posted, runs that stopped. New ones arrive live
// (useNotificationStream refreshes this list).
const Inbox = () => {
  const identityKey = operatorIdentityKey(useOperatorIdentity());
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const query = useQuery({
    queryKey: [...notificationsKey, identityKey],
    queryFn: () => listNotifications({ limit: 100 }),
  });
  const refresh = () => Promise.all([
    queryClient.invalidateQueries({ queryKey: notificationsKey }),
    queryClient.invalidateQueries({ queryKey: notificationCountKey }),
  ]);
  const markAll = useMutation({ mutationFn: markAllNotificationsSeen, onSuccess: refresh });
  const markOne = useMutation({ mutationFn: ({ id, seen }) => markNotificationSeen(id, seen), onSuccess: refresh });

  const notifications = query.data?.notifications || [];
  const unseen = query.data?.unseen_count ?? notifications.filter((item) => !item.seen).length;

  function open(item) {
    if (!item.seen) {
      markOne.mutate({ id: item.id, seen: true });
    }
    if (item.link) {
      navigate(item.link);
    }
  }

  return (
    <div className="grid max-w-3xl gap-6">
      <PageHeader
        title="Inbox"
        description={unseen > 0 ? `${unseen} new` : "Nothing new."}
        actions={unseen > 0 && (
          <Button type="button" variant="ghost" size="sm" disabled={markAll.isPending} onClick={() => markAll.mutate()}>
            Mark all seen
          </Button>
        )}
      />
      {query.isLoading ? (
        <LoadingState />
      ) : query.error ? (
        <DataErrorState error={query.error} unavailableMessage="Couldn't load your inbox." />
      ) : notifications.length === 0 ? (
        <EmptyState title="Nothing here yet" message="Approvals, routine answers and stopped runs show up here." />
      ) : (
        <ol className="grid gap-7" aria-label="Notifications">
          {notifications.map((item) => (
            <NotificationItem key={item.id} item={item} onOpen={() => open(item)} onToggleSeen={() => markOne.mutate({ id: item.id, seen: !item.seen })} onDecided={refresh} />
          ))}
        </ol>
      )}
    </div>
  );
};

export function NotificationItem({ item, onOpen, onToggleSeen, onDecided }) {
  const waiting = item.kind === "approval_pending" && !item.seen;
  return (
    <li className="grid gap-2">
      <div className="flex items-baseline justify-between gap-4">
        <h3 className={cn("min-w-0 text-[0.9375rem]", item.seen ? "font-medium text-muted-foreground" : "font-semibold text-foreground")}>
          {item.title}
        </h3>
        <time className="shrink-0 font-mono text-[0.6875rem] text-muted-foreground" dateTime={item.created_at} title={item.created_at}>
          {relativeTime(item.created_at)}
        </time>
      </div>
      {item.body && <p className="line-clamp-3 whitespace-pre-wrap break-words text-sm leading-6 text-muted-foreground">{item.body}</p>}
      {waiting && <PendingApproval approvalID={item.ref_id} onDecided={onDecided} />}
      <div className="flex flex-wrap items-center gap-1 text-xs">
        {item.link && <Button type="button" variant="ghost" size="sm" onClick={onOpen}>Open</Button>}
        <Button type="button" variant="ghost" size="sm" onClick={onToggleSeen}>{item.seen ? "Mark unseen" : "Mark seen"}</Button>
        {item.delegated_by && <span className="text-muted-foreground">for {item.delegated_by}</span>}
      </div>
    </li>
  );
}

// PendingApproval lets a person decide a held change right from the inbox.
function PendingApproval({ approvalID, onDecided }) {
  const { data: row } = useQuery({
    queryKey: ["approval", approvalID],
    queryFn: () => fetchApproval(approvalID),
    enabled: Boolean(approvalID),
  });
  if (!row) {
    return null;
  }
  const approval = {
    id: row.id,
    hash: row.hash,
    summary: row.summary?.text || "",
    preview: row.preview || {},
    query: row.query,
    expires_at: row.expires_at,
    expect: row.expect?.text || "",
  };
  return <ApprovalCard approval={approval} onDecided={onDecided} />;
}

export function relativeTime(value, now = Date.now()) {
  const time = Date.parse(value);
  if (Number.isNaN(time)) {
    return "";
  }
  const seconds = Math.round((time - now) / 1000);
  const format = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
  const units = [["day", 86400], ["hour", 3600], ["minute", 60]];
  for (const [unit, size] of units) {
    if (Math.abs(seconds) >= size) {
      return format.format(Math.round(seconds / size), unit);
    }
  }
  return "just now";
}

export default Inbox;
