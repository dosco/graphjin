"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { Plus, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { confirmDialog } from "@/components/ConfirmModal";
import { SearchInput } from "@/components/ui/search-input";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/cn";
import { matchesListSearch } from "@/lib/list-search";

type ThreadSummary = {
  id: string;
  title: string;
  createdAt: string;
  updatedAt: string;
  lastMessageAt: string;
};

export default function AskHistoryPanel({
  className,
  onNavigate,
}: {
  className?: string;
  onNavigate?: () => void;
}) {
  const router = useRouter();
  const pathname = usePathname() ?? "";
  const activeThreadId = useMemo(() => {
    const match = pathname.match(/^\/work\/([^/?#]+)/);
    return match?.[1] ?? null;
  }, [pathname]);
  const [threads, setThreads] = useState<ThreadSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [query, setQuery] = useState("");
  const visibleThreads = threads.filter((thread) =>
    matchesListSearch(query, displayThreadTitle(thread.title)),
  );

  useEffect(() => {
    let cancelled = false;
    fetch("/api/work/threads", { cache: "no-store" })
      .then(async (res) => {
        if (!res.ok) return null;
        return (await res.json()) as { threads: ThreadSummary[] };
      })
      .then((data) => {
        if (!cancelled && data) setThreads(data.threads ?? []);
      })
      .catch(() => {
        // The empty state remains useful if history is temporarily unavailable.
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  function createThread() {
    router.push("/work");
    onNavigate?.();
  }

  async function deleteThread(threadId: string) {
    const target = threads.find((t) => t.id === threadId);
    const ok = await confirmDialog({
      title: `Delete "${displayThreadTitle(target?.title ?? "")}"?`,
      description: "This also removes its run history.",
      confirmLabel: "Delete",
      destructive: true,
    });
    if (!ok) return;
    const res = await fetch(`/api/work/threads/${threadId}`, {
      method: "DELETE",
    });
    if (!res.ok) return;
    const remaining = threads.filter((t) => t.id !== threadId);
    setThreads(remaining);
    if (activeThreadId === threadId) {
      router.replace(remaining[0]?.id ? `/work/${remaining[0].id}` : "/work");
      onNavigate?.();
    }
  }

  return (
    <div className={cn("ask-history-panel", className)}>
      <div className="ask-history-head">
        <span className="ask-history-title">History</span>
        <span className="ask-history-count tabular-nums">{threads.length}</span>
        <Button
          variant="ghost"
          type="button"
          className="ask-history-new"
          onClick={createThread}
        >
          <Plus size={13} strokeWidth={2.25} aria-hidden="true" />
          <span>New work</span>
        </Button>
      </div>

      {threads.length > 0 ? (
        <div className="px-2.5 pt-2.5">
          <SearchInput
            label="Search work history"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search history"
            className="min-h-8 py-1.5 text-ui-body-sm"
          />
        </div>
      ) : null}

      <div className="ask-history-list">
        {loading ? (
          <div className="ask-history-empty">Loading threads...</div>
        ) : visibleThreads.length === 0 ? (
          <div className="ask-history-empty">
            {query
              ? "No threads match this search."
              : "Start a thread to see it here."}
          </div>
        ) : (
          visibleThreads.map((thread) => {
            const active = thread.id === activeThreadId;
            return (
              <div
                key={thread.id}
                className={cn("ask-history-row", active && "is-active")}
              >
                <Link
                  href={`/work/${thread.id}`}
                  className="ask-history-row-main"
                  title={displayThreadTitle(thread.title)}
                  prefetch={false}
                  onClick={onNavigate}
                >
                  <span className="ask-history-dot" aria-hidden="true" />
                  <span className="ask-history-row-copy">
                    <span className="ask-history-row-title">
                      {displayThreadTitle(thread.title)}
                    </span>
                    <span className="ask-history-row-time">
                      {formatDate(thread.lastMessageAt)}
                    </span>
                  </span>
                </Link>
                <Button
                  variant="ghost"
                  type="button"
                  className="ask-history-delete"
                  title="Delete thread"
                  aria-label="Delete thread"
                  onClick={(event) => {
                    event.preventDefault();
                    event.stopPropagation();
                    void deleteThread(thread.id);
                  }}
                >
                  <Trash2 size={13} strokeWidth={2} aria-hidden="true" />
                </Button>
              </div>
            );
          })
        )}
      </div>
    </div>
  );
}

function displayThreadTitle(value: string): string {
  const title = value.trim();
  return !title || /^untitled thread$/i.test(title) ? "No prompt yet" : title;
}

function formatDate(value: string): string {
  const d = new Date(value);
  const day = d.getDate();
  const mod100 = day % 100;
  const suffix =
    mod100 >= 11 && mod100 <= 13
      ? "th"
      : (({ 1: "st", 2: "nd", 3: "rd" } as Record<number, string>)[day % 10] ??
        "th");
  const month = d.toLocaleDateString(undefined, { month: "short" });
  const time = d
    .toLocaleTimeString(undefined, {
      hour: "numeric",
      minute: "2-digit",
      hour12: true,
    })
    .toLowerCase();
  return `${day}${suffix} ${month} ${time}`;
}
