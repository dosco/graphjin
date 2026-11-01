"use client";

import { Suspense, useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { ArrowLeft, FileText, Trash2, Upload } from "lucide-react";
import type { browseLibrary, LibraryDocument } from "@neko/llm/work";
import { LIBRARY_UPLOAD_ACCEPT } from "@neko/llm/library/formats";
import { confirmDialog } from "@/components/ConfirmModal";
import PageHeading from "@/components/PageHeading";
import { ActionGroup } from "@/components/ui/action-group";
import { Button } from "@/components/ui/button";
import { MenuItem, OverflowMenu } from "@/components/ui/overflow-menu";
import { EmptyState } from "@/components/ui/empty";
import { Badge, type BadgeVariant } from "@/components/ui/badge";
import { SearchInput } from "@/components/ui/search-input";
import { Field, Input, NativeSelect } from "@/components/ui/field";
import { LocalDateTime } from "@/components/ui/local-date-time";
import { validateLibraryUploadBatch } from "@/lib/library-upload-contract";

type LibraryData = Awaited<ReturnType<typeof browseLibrary>>;

const STATUS: Record<string, { label: string; variant: BadgeVariant }> = {
  ready: { label: "Ready", variant: "success" },
  failed: { label: "Failed", variant: "danger" },
};

function StatusBadge({ status }: { status: string }) {
  const pill = STATUS[status] ?? { label: status, variant: "muted" as BadgeVariant };
  return <Badge variant={pill.variant}>{pill.label}</Badge>;
}

export default function LibraryPage() {
  return <Suspense fallback={<p role="status">Loading library…</p>}><LibraryBrowser /></Suspense>;
}

function LibraryBrowser() {
  const params = useSearchParams();
  const q = params.get("q") ?? "";
  const selectedId = params.get("id");
  const listParams = new URLSearchParams(params.toString());
  listParams.delete("id");
  const requestKey = listParams.toString();
  const [data, setData] = useState<LibraryData>({ documents: [], total: 0, page: 1, pageSize: 50 });
  const [loadedKey, setLoadedKey] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [queryDraft, setQueryDraft] = useState({ url: q, value: q });
  const query = queryDraft.url === q ? queryDraft.value : q;
  const [version, setVersion] = useState(0);
  const [detailState, setDetailState] = useState<{ key: string; document: LibraryDocument | null; error: string | null } | null>(null);
  const loadKey = `${requestKey}:${version}`;
  const detailKey = `${selectedId}:${version}`;
  const loading = loadedKey !== loadKey;
  const selectedDocument = detailState?.key === detailKey ? detailState.document : null;
  const detailError = detailState?.key === detailKey ? detailState.error : null;
  const [uploading, setUploading] = useState(false);
  const [filtersOpen, setFiltersOpen] = useState(false);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const queryTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const detailRef = useRef<HTMLElement | null>(null);

  const change = useCallback((changes: Record<string, string | null>, replace = false) => {
    setError(null);
    const next = new URLSearchParams(window.location.search);
    for (const [key, value] of Object.entries(changes)) {
      if (value) next.set(key, value); else next.delete(key);
    }
    const href = `/library?${next}`;
    if (replace) window.history.replaceState(null, "", href);
    else window.history.pushState(null, "", href);
  }, []);
  const refresh = useCallback(async () => { setError(null); setVersion(value => value + 1); }, []);

  useEffect(() => {
    return () => { if (queryTimer.current) clearTimeout(queryTimer.current); };
  }, [q]);

  useEffect(() => {
    const controller = new AbortController();
    void fetch(`/api/library?${requestKey}`, { cache: "no-store", signal: controller.signal })
      .then(async response => {
        const payload = await response.json();
        if (!response.ok) throw new Error(payload.error ?? "Library could not be loaded. Try again.");
        return payload as LibraryData;
      })
      .then(loaded => { if (!controller.signal.aborted) setData(loaded); })
      .catch(cause => { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : "Library could not be loaded. Try again."); })
      .finally(() => { if (!controller.signal.aborted) setLoadedKey(loadKey); });
    return () => controller.abort();
  }, [requestKey, loadKey]);

  useEffect(() => {
    if (!selectedId) return;
    const controller = new AbortController();
    void fetch(`/api/library/documents/${encodeURIComponent(selectedId)}`, { cache: "no-store", signal: controller.signal })
      .then(async response => {
        if (!response.ok) throw new Error(response.status === 404
          ? "This document is no longer available. Close it and refresh the list."
          : "Details could not be loaded. Try again.");
        return (await response.json() as { document: LibraryDocument }).document;
      })
      .then(loaded => { if (!controller.signal.aborted) { setDetailState({ key: detailKey, document: loaded, error: null }); detailRef.current?.focus({ preventScroll: true }); } })
      .catch(cause => { if (!controller.signal.aborted) setDetailState({ key: detailKey, document: null, error: cause instanceof Error ? cause.message : "Details could not be loaded. Try again." }); });
    return () => controller.abort();
  }, [selectedId, detailKey]);

  const upload = useCallback(
    async (files: FileList | null) => {
      if (!files || files.length === 0) return;
      const selected = Array.from(files);
      const validation = validateLibraryUploadBatch(selected);
      if (!validation.ok) {
        setError(validation.error);
        if (fileInputRef.current) fileInputRef.current.value = "";
        return;
      }
      setUploading(true);
      try {
        const body = new FormData();
        for (const file of selected) body.append("files", file);
        const response = await fetch("/api/library/upload", { method: "POST", body });
        if (!response.ok) {
          const payload = (await response.json().catch(() => null)) as { error?: string } | null;
          throw new Error(payload?.error ?? "The documents could not be imported.");
        }
        setError(null);
        await refresh();
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : "Upload failed.");
      } finally {
        setUploading(false);
        if (fileInputRef.current) fileInputRef.current.value = "";
      }
    },
    [refresh],
  );

  const removeDocument = useCallback(
    async (id: string) => {
      const ok = await confirmDialog({
        title: "Remove this document from your library?",
        description: "OpenNeko will no longer find it when you ask. Files attached in a conversation stay with that conversation.",
        confirmLabel: "Remove",
        destructive: true,
      });
      if (!ok) return;
      setBusyId(id);
      try {
        const response = await fetch(`/api/library/documents/${encodeURIComponent(id)}`, { method: "DELETE" });
        if (!response.ok) throw new Error("Document could not be removed.");
        if (id === selectedId) change({ id: null });
        await refresh();
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : "Document could not be removed.");
      } finally {
        setBusyId(null);
      }
    },
    [change, refresh, selectedId],
  );

  const filter = (key: string, value: string) => change({ [key]: value, page: null, id: null });
  const rowHref = (id: string) => {
    const next = new URLSearchParams(params.toString());
    next.set("id", id);
    return `/library?${next}`;
  };
  const closeDetail = () => change({ id: null });
  const pageCount = Math.max(1, Math.ceil(data.total / data.pageSize));
  const searching = loading || query.trim() !== q;

  return (
    <div className="library-page document-library">
      <PageHeading title="Library" description="Your documents and spreadsheets. OpenNeko searches and reads them when you ask."
        actions={<Button variant="primary" disabled={uploading} onClick={() => fileInputRef.current?.click()}>
          <Upload aria-hidden="true" />{uploading ? "Uploading…" : "Upload documents"}
        </Button>} />
      <Input ref={fileInputRef} type="file" multiple accept={LIBRARY_UPLOAD_ACCEPT} hidden onChange={event => void upload(event.target.files)} />
      <div className="library-browser">
        <div className="library-browser-toolbar">
          <Field label="Search" htmlFor="library-search" className="library-browser-search">
            <SearchInput id="library-search" label="Search library" value={query} maxLength={200}
              placeholder="Find a document by name"
              onChange={event => {
                const value = event.target.value;
                setQueryDraft({ url: q, value });
                if (queryTimer.current) clearTimeout(queryTimer.current);
                queryTimer.current = setTimeout(() => change({ q: value.trim() || null, page: null, id: null }, true), 350);
              }} />
          </Field>
          <Button className="library-browser-filter-toggle" aria-expanded={filtersOpen} aria-controls="library-filters" onClick={() => setFiltersOpen(value => !value)}>Filters</Button>
          <div id="library-filters" className="library-browser-filters" data-open={filtersOpen}>
            <Field label="Status" htmlFor="library-status">
              <NativeSelect id="library-status" value={params.get("status") ?? ""} onChange={event => filter("status", event.target.value)}>
                <option value="">All statuses</option>
                {Object.entries(STATUS).map(([status, { label }]) => <option key={status} value={status}>{label}</option>)}
              </NativeSelect>
            </Field>
            <Field label="Sort" htmlFor="library-sort">
              <NativeSelect id="library-sort" value={params.get("sort") ?? "recent"} onChange={event => filter("sort", event.target.value)}>
                <option value="recent">Most recent</option><option value="name">Name</option>
              </NativeSelect>
            </Field>
          </div>
        </div>
        {error && <div className="library-error" role="alert"><span>{error}</span><Button size="sm" onClick={() => void refresh()}>Retry</Button></div>}
        <div className="library-browser-workspace" data-detail-open={Boolean(selectedId)}>
          <section className="library-browser-results" aria-label="Documents" aria-busy={searching}>
            <header className="library-browser-results-head">
              <span role="status">{searching ? "Searching library…" : `${data.total.toLocaleString()} ${data.total === 1 ? "document" : "documents"}${q ? " found" : ""}`}</span>
              <span>Private to you</span>
            </header>
            {searching ? <p className="library-browser-message" role="status">Loading results…</p> : error ? null : data.total === 0 ?
              <EmptyState className="library-empty" title={q ? "No matches" : "No documents yet"}
                body={q ? "Try another name or clear the filters." : "Upload documents and spreadsheets, then ask OpenNeko about them."}
                action={q || params.get("status") ? <Button onClick={() => change({ q: null, status: null, page: null, sort: null })}>Clear filters</Button> : undefined} />
              : <ul className="library-browser-list">
                {data.documents.map(doc => <li key={doc.id} data-selected={doc.id === selectedId}>
                  <FileText aria-hidden="true" size={18} />
                  <div className="library-browser-row-copy">
                    <Link href={rowHref(doc.id)} prefetch={false} onNavigate={event => { event.preventDefault(); change({ id: doc.id }); }} aria-current={doc.id === selectedId ? "true" : undefined}>{doc.filename}</Link>
                    <span>{formatSize(doc.sizeBytes)} · <LocalDateTime value={doc.createdAt} /></span>
                  </div>
                  <StatusBadge status={doc.status} />
                  <OverflowMenu label={`Actions for ${doc.filename}`}>
                    <MenuItem danger disabled={busyId === doc.id} onClick={() => void removeDocument(doc.id)}><Trash2 aria-hidden="true" />Remove from library</MenuItem>
                  </OverflowMenu>
                </li>)}
              </ul>}
            <footer className="library-browser-pagination">
              <span>{data.total ? `${(data.page - 1) * data.pageSize + 1}–${Math.min(data.page * data.pageSize, data.total)} of ${data.total.toLocaleString()}` : "0 results"}</span>
              <ActionGroup><Button size="sm" disabled={searching || data.page <= 1} onClick={() => change({ page: String(data.page - 1), id: null })}>Previous</Button>
                <span className="tabular-nums">{data.page} / {pageCount}</span>
                <Button size="sm" disabled={searching || data.page >= pageCount} onClick={() => change({ page: String(data.page + 1), id: null })}>Next</Button></ActionGroup>
            </footer>
          </section>
          {selectedId && <section className="library-browser-detail" aria-label="Document details" ref={detailRef} tabIndex={-1}
            onKeyDown={event => { if (event.key === "Escape") { event.stopPropagation(); closeDetail(); } }}>
            <Button variant="ghost" size="sm" onClick={closeDetail}><ArrowLeft aria-hidden="true" />Back to list</Button>
            {detailError ? <div role="alert"><p>{detailError}</p><Button size="sm" onClick={() => void refresh()}>Retry details</Button></div>
              : !selectedDocument ? <p role="status">Loading details…</p>
              : <>
                <h2>{selectedDocument.filename}</h2>
                <div><StatusBadge status={selectedDocument.status} /></div>
                <p>{formatSize(selectedDocument.sizeBytes)} · <LocalDateTime value={selectedDocument.createdAt} /></p>
                <p className="text-ui-body-sm text-text2">{selectedDocument.status === "failed"
                  ? selectedDocument.error || "This document could not be stored. Remove it and upload it again."
                  : "Private to you. OpenNeko searches and reads this file when you ask about it."}</p>
                <ActionGroup align="start"><Button variant="danger" disabled={busyId === selectedDocument.id} onClick={() => void removeDocument(selectedDocument.id)}>Remove from library</Button></ActionGroup>
              </>}
          </section>}
        </div>
      </div>
    </div>
  );
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
