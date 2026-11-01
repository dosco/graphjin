"use client";

import { useCallback, useEffect, useState } from "react";
import { Archive, Pin } from "lucide-react";
import { confirmDialog } from "@/components/ConfirmModal";
import PageHeading from "@/components/PageHeading";
import { Button, IconButton } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty";
import { SearchInput } from "@/components/ui/search-input";
import { matchesListSearch } from "@/lib/list-search";

type MemoryRow = {
  id: string;
  kind: string;
  scope: string;
  scopeId: string | null;
  text: string;
  pinned: boolean;
  confidence: number;
  useCount?: number;
  lastUsedAt?: string | null;
  archivedAt?: string | null;
  createdAt?: string;
};

type PendingRow = {
  id: string;
  draftText: string;
  draftKind: string;
  draftScope: string;
  confidence: number;
  reasoning: string | null;
};

async function fetchMemoryData(signal?: AbortSignal) {
  const [memoryResponse, pendingResponse] = await Promise.all([
    fetch("/api/work/memories", { cache: "no-store", signal }),
    fetch("/api/work/memories/pending", { cache: "no-store", signal }),
  ]);
  if (!memoryResponse.ok || !pendingResponse.ok) {
    throw new Error("Memory could not be loaded.");
  }
  const [memoryData, pendingData] = await Promise.all([
    memoryResponse.json() as Promise<{ memories?: MemoryRow[] }>,
    pendingResponse.json() as Promise<{ pending?: PendingRow[] }>,
  ]);
  return {
    memories: memoryData.memories ?? [],
    pending: pendingData.pending ?? [],
  };
}

export default function MemoryPage() {
  const [memories, setMemories] = useState<MemoryRow[]>([]);
  const [pending, setPending] = useState<PendingRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [query, setQuery] = useState("");

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const data = await fetchMemoryData();
      setMemories(data.memories);
      setPending(data.pending);
      setError(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Memory could not be loaded.");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void fetchMemoryData(controller.signal)
      .then((data) => {
        setMemories(data.memories);
        setPending(data.pending);
        setError(null);
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted) return;
        setError(
          cause instanceof Error ? cause.message : "Memory could not be loaded.",
        );
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, []);

  const archive = useCallback(
    async (id: string) => {
      const ok = await confirmDialog({
        title: "Archive this memory?",
        description: "It will stop being added to future agent runs.",
        confirmLabel: "Archive",
        destructive: true,
      });
      if (!ok) return;
      setBusyId(id);
      try {
        const response = await fetch(
          `/api/work/memories/${encodeURIComponent(id)}`,
          {
            method: "DELETE",
            headers: { "content-type": "application/json" },
            body: JSON.stringify({ reason: "manual archive from /memory" }),
          },
        );
        if (!response.ok) throw new Error("Memory could not be archived.");
        await refresh();
      } catch (cause) {
        setError(
          cause instanceof Error ? cause.message : "Memory could not be archived.",
        );
      } finally {
        setBusyId(null);
      }
    },
    [refresh],
  );

  const decide = useCallback(
    async (id: string, action: "accept" | "decline") => {
      setBusyId(id);
      try {
        const response = await fetch("/api/work/memories/decide", {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({
            id,
            action,
            ...(action === "accept" ? { scope: "global" } : {}),
          }),
        });
        if (!response.ok) throw new Error("Memory review could not be saved.");
        await refresh();
      } catch (cause) {
        setError(
          cause instanceof Error
            ? cause.message
            : "Memory review could not be saved.",
        );
      } finally {
        setBusyId(null);
      }
    },
    [refresh],
  );

  const active = memories.filter((memory) => !memory.archivedAt);
  const visibleActive = active.filter((memory) =>
    matchesListSearch(
      query,
      memory.kind,
      memory.scope,
      memory.scopeId,
      memory.text,
    ),
  );
  const visiblePending = pending.filter((item) =>
    matchesListSearch(
      query,
      item.draftKind,
      item.draftScope,
      item.draftText,
      item.reasoning,
    ),
  );

  return (
    <div className="library-page memory-library">
      <PageHeading
        title="Memory"
        description="Facts OpenNeko saved about your business and uses in briefings, answers, and runs."
        actions={
          <div className="library-head-stats" aria-label="Memory status">
            <div>
              <strong>{active.length}</strong>
              <span>active</span>
            </div>
            <div data-state={pending.length > 0 ? "attention" : "clear"}>
              <strong>{pending.length}</strong>
              <span>pending</span>
            </div>
          </div>
        }
      />

      <main className="library-main">
        <SearchInput
          label="Search memory"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Search saved memories"
        />

        {error ? (
          <div className="library-error" role="alert">
            <div>
              <strong>Memory unavailable</strong>
              <span>{error}</span>
            </div>
            <Button
              variant="danger"
              size="sm"
              className="shrink-0"
              onClick={() => void refresh()}
            >
              Retry
            </Button>
          </div>
        ) : null}

        {visiblePending.length > 0 ? (
          <section className="library-section memory-review">
            <header className="library-section-head">
              <div>
                <span>Needs review</span>
                <h2>Pending suggestions</h2>
              </div>
              <strong>{visiblePending.length}</strong>
            </header>
            <ol className="memory-review-list">
              {visiblePending.map((item, index) => (
                <li key={item.id} className="memory-review-row">
                  <span className="library-index">
                    {index + 1}
                  </span>
                  <div className="memory-review-copy">
                    <div className="memory-review-meta">
                      <span>{humanize(item.draftKind)}</span>
                      <span>{item.draftScope}</span>
                      <span>{Math.round(item.confidence * 100)}% confidence</span>
                    </div>
                    <p>{item.draftText}</p>
                    {item.reasoning ? <small>{item.reasoning}</small> : null}
                  </div>
                  <div className="memory-review-actions">
                    <Button
                      variant="primary"
                      size="sm"
                      disabled={busyId === item.id}
                      onClick={() => void decide(item.id, "accept")}
                    >
                      Save globally
                    </Button>
                    <Button
                      size="sm"
                      disabled={busyId === item.id}
                      onClick={() => void decide(item.id, "decline")}
                    >
                      Decline
                    </Button>
                  </div>
                </li>
              ))}
            </ol>
          </section>
        ) : null}

        <section className="library-section memory-saved">
          <header className="library-section-head">
            <div>
              <span>Agent context</span>
              <h2>Saved memories</h2>
            </div>
          </header>

          {loading ? (
            <div className="library-loading" role="status" aria-label="Loading memories">
              <span />
              <span />
              <span />
            </div>
          ) : visibleActive.length === 0 ? (
            <EmptyState
              className="library-empty"
              title={query ? "No matching memories" : "No saved memory"}
              body={
                query
                  ? "Try another fact, kind, or scope."
                  : "Stable facts and instructions appear here after you ask OpenNeko to remember them."
              }
            />
          ) : (
            <>
              <div className="memory-table-head" aria-hidden="true">
                <span />
                <span>Kind</span>
                <span>Memory</span>
                <span>Scope</span>
                <span>Usage</span>
                <span />
              </div>
              <ol className="memory-index">
                {visibleActive.map((memory, index) => (
                  <li key={memory.id} className="memory-index-row">
                    <span className="library-index">
                      {index + 1}
                    </span>
                    <div className="memory-kind">
                      <span>{humanize(memory.kind)}</span>
                      {memory.pinned ? (
                        <small>
                          <Pin aria-hidden="true" strokeWidth={2} />
                          pinned
                        </small>
                      ) : null}
                    </div>
                    <p>{memory.text}</p>
                    <div className="memory-scope">
                      <span>{memory.scope}</span>
                      {memory.scopeId ? <small>{memory.scopeId.slice(0, 8)}</small> : null}
                    </div>
                    <div className="memory-usage">
                      <span>{memory.useCount ?? 0} uses</span>
                      <small>
                        {memory.lastUsedAt
                          ? `last ${formatDate(memory.lastUsedAt)}`
                          : memory.createdAt
                            ? `saved ${formatDate(memory.createdAt)}`
                            : "not used"}
                      </small>
                    </div>
                    <IconButton
                      label="Archive memory"
                      variant="ghost"
                      className="memory-archive-control"
                      disabled={busyId === memory.id}
                      onClick={() => void archive(memory.id)}
                    >
                      <Archive aria-hidden="true" strokeWidth={1.9} />
                    </IconButton>
                  </li>
                ))}
              </ol>
            </>
          )}
        </section>
      </main>
    </div>
  );
}

function humanize(value: string): string {
  return value.replace(/_/g, " ");
}

function formatDate(value: string): string {
  return new Date(value).toLocaleDateString("en-IN", {
    month: "short",
    day: "numeric",
  });
}
