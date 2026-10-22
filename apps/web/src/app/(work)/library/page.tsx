"use client";

import { Suspense, useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { ArrowLeft, FileText, Share2, Trash2, Upload } from "lucide-react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { toast } from "sonner";
import type { browseLibrary, LibraryConcept, LibraryDocument } from "@neko/llm/work";
import { LIBRARY_UPLOAD_ACCEPT } from "@neko/llm/library/formats";
import { confirmDialog } from "@/components/ConfirmModal";
import PageHeading from "@/components/PageHeading";
import { ActionGroup } from "@/components/ui/action-group";
import { Button } from "@/components/ui/button";
import { MenuItem, OverflowMenu } from "@/components/ui/overflow-menu";
import { EmptyState } from "@/components/ui/empty";
import { Badge, type BadgeVariant } from "@/components/ui/badge";
import { Checkbox, CheckboxControl } from "@/components/ui/checkbox";
import { SearchInput } from "@/components/ui/search-input";
import { Field, Input, NativeSelect, Textarea } from "@/components/ui/field";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Tabs, Tab } from "@/components/ui/tabs";
import { LocalDateTime } from "@/components/ui/local-date-time";
import { validateLibraryUploadBatch } from "@/lib/library-upload-contract";

type LibraryData = Awaited<ReturnType<typeof browseLibrary>>;
type PackRow = { id: string; title: string; description: string; concepts: number };
type Detail = { concept?: LibraryConcept; document?: LibraryDocument };
type Sharing = LibraryData["concepts"][number]["sharing"];

const SHARING_PILL: Record<NonNullable<Sharing>, { label: string; variant: BadgeVariant }> = {
  shared: { label: "Shared", variant: "success" },
  awaiting_approval: { label: "Awaiting approval", variant: "watch" },
  retired: { label: "Retired", variant: "muted" },
};

function SharingPill({ sharing }: { sharing: Sharing }) {
  if (!sharing) return null;
  const pill = SHARING_PILL[sharing];
  return <Badge variant={pill.variant}>{pill.label}</Badge>;
}

function statusVariant(status: string): BadgeVariant {
  if (["cataloged", "stable", "approved", "ready"].includes(status)) return "success";
  if (["failed", "declined", "deprecated"].includes(status)) return "danger";
  if (["draft", "pending", "uploaded", "processing", "review", "extracting", "extracted", "distilling"].includes(status)) return "watch";
  return "muted";
}

export default function LibraryPage() {
  return <Suspense fallback={<p role="status">Loading library…</p>}><LibraryBrowser /></Suspense>;
}

function LibraryBrowser() {
  const params = useSearchParams();
  const view = params.get("view") ?? "documents";
  const q = params.get("q") ?? "";
  const selectedId = params.get("id");
  const listParams = new URLSearchParams(params.toString());
  listParams.delete("id");
  const requestKey = listParams.toString();
  const [data, setData] = useState<LibraryData>({
    isAdmin: false, documents: [], concepts: [], counts: { documents: 0, concepts: 0, review: 0 },
    total: 0, page: 1, pageSize: 50, types: [], searchMode: "browse",
  });
  const [loadedKey, setLoadedKey] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [pickedState, setPickedState] = useState<{ key: string; ids: string[] }>({ key: "", ids: [] });
  const picked = pickedState.key === requestKey ? pickedState.ids : [];
  const setPicked = (ids: string[]) => setPickedState({ key: requestKey, ids });
  const [queryDraft, setQueryDraft] = useState({ url: q, value: q });
  const query = queryDraft.url === q ? queryDraft.value : q;
  const [version, setVersion] = useState(0);
  const [detailState, setDetailState] = useState<{ key: string; data: Detail | null; error: string | null } | null>(null);
  const loadKey = `${requestKey}:${version}`;
  const detailKey = `${view}:${selectedId}:${version}`;
  const loading = loadedKey !== loadKey;
  const detail = detailState?.key === detailKey ? detailState.data : null;
  const detailError = detailState?.key === detailKey ? detailState.error : null;
  const [uploading, setUploading] = useState(false);
  const [packs, setPacks] = useState<PackRow[]>([]);
  const [toolsOpen, setToolsOpen] = useState(false);
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [editingConcept, setEditingConcept] = useState<LibraryConcept | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const importInputRef = useRef<HTMLInputElement | null>(null);
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

  const hasActiveDocuments = data.documents.some(document =>
    ["uploaded", "extracting", "extracted", "distilling"].includes(document.status));
  useEffect(() => {
    if (!hasActiveDocuments || view !== "documents") return;
    const timer = setInterval(() => setVersion(value => value + 1), 3_000);
    return () => clearInterval(timer);
  }, [hasActiveDocuments, view]);

  useEffect(() => {
    if (!selectedId) return;
    const controller = new AbortController();
    void fetch(`/api/library/${view === "documents" ? "documents" : "concepts"}/${encodeURIComponent(selectedId)}`,
      { cache: "no-store", signal: controller.signal })
      .then(async response => {
        if (!response.ok) throw new Error(response.status === 404
          ? "This item is no longer available. Close it and refresh the list."
          : "Details could not be loaded. Try again.");
        return response.json() as Promise<Detail>;
      })
      .then(loaded => { if (!controller.signal.aborted) { setDetailState({ key: detailKey, data: loaded, error: null }); detailRef.current?.focus({ preventScroll: true }); } })
      .catch(cause => { if (!controller.signal.aborted) setDetailState({ key: detailKey, data: null, error: cause instanceof Error ? cause.message : "Details could not be loaded. Try again." }); });
    return () => controller.abort();
  }, [selectedId, view, detailKey]);

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
        for (const file of selected) {
          body.append("files", file);
        }
        const response = await fetch("/api/library/upload", {
          method: "POST",
          body,
        });
        if (!response.ok) {
          const payload = (await response.json().catch(() => null)) as {
            error?: string;
          } | null;
          throw new Error(
            payload?.error ?? "The documents could not be imported.",
          );
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

  const act = useCallback(
    async (id: string, run: () => Promise<Response>, failure: string) => {
      setBusyId(id);
      try {
        const response = await run();
        if (!response.ok) throw new Error(failure);
        setError(null);
        await refresh();
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : failure);
      } finally {
        setBusyId(null);
      }
    },
    [refresh],
  );

  const share = useCallback(
    (id: string) =>
      act(
        id,
        () =>
          fetch(`/api/library/concepts/${encodeURIComponent(id)}/share`, {
            method: "POST",
          }),
        "Concept could not be shared.",
      ),
    [act],
  );

  const decide = useCallback(
    (id: string, action: "approve" | "decline" | "deprecate") =>
      act(
        id,
        () =>
          fetch(`/api/library/concepts/${encodeURIComponent(id)}/decide`, {
            method: "POST",
            headers: { "content-type": "application/json" },
            body: JSON.stringify({ action }),
          }),
        "Library review could not be saved.",
      ),
    [act],
  );

  const runBulk = useCallback(
    async (ids: string[], path: string, body: object | null, label: string) => {
      setBusyId("bulk");
      let failed = 0;
      for (const id of ids) {
        const response = await fetch(`/api/library/concepts/${encodeURIComponent(id)}/${path}`, {
          method: "POST",
          ...(body ? { headers: { "content-type": "application/json" }, body: JSON.stringify(body) } : {}),
        }).catch(() => null);
        if (!response?.ok) failed += 1;
      }
      setPickedState({ key: "", ids: [] });
      setError(failed ? `${failed} of ${ids.length} concepts could not be ${label}.` : null);
      if (!failed) toast.success(`${ids.length} ${ids.length === 1 ? "concept" : "concepts"} ${label}.`);
      setBusyId(null);
      await refresh();
    },
    [refresh],
  );

  const removeDocument = useCallback(
    async (id: string) => {
      const ok = await confirmDialog({
        title: "Remove this document from your library?",
        description:
          "OpenNeko will no longer find it when you ask. Files attached in a conversation stay with that conversation.",
        confirmLabel: "Remove",
        destructive: true,
      });
      if (!ok) return;
      await act(
        id,
        () =>
          fetch(`/api/library/documents/${encodeURIComponent(id)}`, {
            method: "DELETE",
          }),
        "Document could not be removed.",
      );
    },
    [act],
  );

  const archiveConcept = useCallback(
    async (id: string) => {
      const ok = await confirmDialog({
        title: "Archive this concept?",
        description: "It will stop appearing in your library search results.",
        confirmLabel: "Archive",
        destructive: true,
      });
      if (!ok) return;
      await act(
        id,
        () =>
          fetch(`/api/library/concepts/${encodeURIComponent(id)}`, {
            method: "DELETE",
          }),
        "Concept could not be archived.",
      );
    },
    [act],
  );

  useEffect(() => {
    if (!data.isAdmin) return;
    const controller = new AbortController();
    fetch("/api/library/packs", {
      cache: "no-store",
      signal: controller.signal,
    })
      .then(async (res) => (res.ok ? res.json() : { packs: [] }))
      .then((payload: { packs?: PackRow[] }) => setPacks(payload.packs ?? []))
      .catch(() => {});
    return () => controller.abort();
  }, [data.isAdmin]);

  const exportBundle = useCallback(async () => {
    try {
      const res = await fetch("/api/library/export", { cache: "no-store" });
      if (!res.ok) throw new Error("Export failed.");
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = "library-okf-bundle.json";
      anchor.click();
      URL.revokeObjectURL(url);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Export failed.");
    }
  }, []);

  const importBundle = useCallback(
    async (files: FileList | null) => {
      const file = files?.[0];
      if (!file) return;
      try {
        const parsed = JSON.parse(await file.text()) as unknown;
        const res = await fetch("/api/library/import", {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify(parsed),
        });
        if (!res.ok) {
          const payload = (await res.json().catch(() => null)) as {
            error?: string;
          } | null;
          throw new Error(payload?.error ?? "Import failed.");
        }
        setError(null);
        await refresh();
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : "Import failed.");
      } finally {
        if (importInputRef.current) importInputRef.current.value = "";
      }
    },
    [refresh],
  );

  const installPack = useCallback(
    (id: string) =>
      act(
        id,
        () =>
          fetch(`/api/library/packs/${encodeURIComponent(id)}/install`, {
            method: "POST",
          }),
        "Pack could not be installed.",
      ),
    [act],
  );


  const filter = (key: string, value: string) => change({ [key]: value, page: null, id: null });
  const rowHref = (id: string) => {
    const next = new URLSearchParams(params.toString());
    next.set("id", id);
    return `/library?${next}`;
  };
  const closeDetail = () => change({ id: null });
  const selectedConcept = detail?.concept;
  const selectedDocument = detail?.document;
  const isDocuments = view === "documents";
  const pageCount = Math.max(1, Math.ceil(data.total / data.pageSize));
  const searching = loading || query.trim() !== q;
  const pickable = (concept: { status: string; layer: "personal" | "team" }) =>
    concept.layer === "personal" || (data.isAdmin && concept.status === "draft");
  const pickableIds = isDocuments ? [] : data.concepts.filter(pickable).map(concept => concept.id);
  const pickedConcepts = data.concepts.filter(concept => picked.includes(concept.id));
  const pickedPersonal = pickedConcepts.filter(concept => concept.layer === "personal").map(concept => concept.id);
  const pickedDrafts = pickedConcepts.filter(concept => concept.layer === "team" && concept.status === "draft").map(concept => concept.id);
  const conceptActions = (concept: { id: string; title: string; status: string; layer: "personal" | "team" }) =>
    <OverflowMenu label={`Actions for ${concept.title}`}>
      {concept.layer === "personal" ? <>
        <MenuItem disabled={busyId === concept.id} onClick={() => void share(concept.id)}><Share2 aria-hidden="true" />Share with team</MenuItem>
        <MenuItem danger disabled={busyId === concept.id} onClick={() => void archiveConcept(concept.id)}><Trash2 aria-hidden="true" />Archive concept</MenuItem>
      </> : data.isAdmin ? concept.status === "draft" ? <>
        <MenuItem disabled={busyId === concept.id} onClick={() => void decide(concept.id, "approve")}>Approve for team</MenuItem>
        <MenuItem danger disabled={busyId === concept.id} onClick={() => void decide(concept.id, "decline")}>Decline concept</MenuItem>
      </> : concept.status === "stable" ? <MenuItem danger disabled={busyId === concept.id} onClick={() => void decide(concept.id, "deprecate")}>Deprecate concept</MenuItem> : null : null}
    </OverflowMenu>;

  return (
    <div className="library-page document-library">
      <PageHeading title="Library" description="Your documents and spreadsheets. OpenNeko searches and reads them when you ask."
        actions={<ActionGroup>
          {data.isAdmin && <OverflowMenu label="Library tools">
            <MenuItem onClick={() => setToolsOpen(value => !value)}>Starter packs</MenuItem>
            <MenuItem onClick={() => void exportBundle()}>Export library</MenuItem>
            <MenuItem onClick={() => importInputRef.current?.click()}>Import library</MenuItem>
          </OverflowMenu>}
          <Button variant="primary" disabled={uploading} onClick={() => fileInputRef.current?.click()}>
            <Upload aria-hidden="true" />{uploading ? "Uploading…" : "Upload documents"}
          </Button>
        </ActionGroup>} />
      <Input ref={fileInputRef} type="file" multiple accept={LIBRARY_UPLOAD_ACCEPT} hidden onChange={event => void upload(event.target.files)} />
      <Input ref={importInputRef} type="file" accept=".json,application/json" hidden onChange={event => void importBundle(event.target.files)} />
      <div className="library-browser">
        <Tabs aria-label="Library views">
          {(["documents", "concepts", ...(data.isAdmin ? ["review"] : [])] as const).map(tab =>
            <Tab key={tab} selected={view === tab} aria-label={tab === "review" ? "Needs review" : humanize(tab)}
              onClick={() => change({ view: tab, page: null, id: null, type: null, status: null, layer: null, documentId: null })}>
              {tab === "review" ? "Needs review" : tab === "documents" ? "Documents" : "Concepts"}
              <span className="tabular-nums">{data.counts[tab as keyof typeof data.counts].toLocaleString()}</span>
            </Tab>)}
        </Tabs>
        <div className="library-browser-toolbar">
          <Field label="Search" htmlFor="library-search" className="library-browser-search">
            <SearchInput id="library-search" label="Search library" value={query} maxLength={200}
              placeholder={isDocuments ? "Find a document or describe what you need" : "Find a concept or describe what you need"}
              onChange={event => {
                const value = event.target.value;
                setQueryDraft({ url: q, value });
                if (queryTimer.current) clearTimeout(queryTimer.current);
                queryTimer.current = setTimeout(() => change({ q: value.trim() || null, page: null, id: null, sort: value.trim() ? "relevance" : null }, true), 350);
              }} />
          </Field>
          <Button className="library-browser-filter-toggle" aria-expanded={filtersOpen} aria-controls="library-filters" onClick={() => setFiltersOpen(value => !value)}>Filters</Button>
          <div id="library-filters" className="library-browser-filters" data-open={filtersOpen}>
          {view === "concepts" && <Field label="Visibility" htmlFor="library-layer">
            <NativeSelect id="library-layer" value={params.get("layer") ?? "all"} onChange={event => filter("layer", event.target.value)}>
              <option value="all">All visible</option><option value="personal">Personal</option><option value="team">Team</option>
            </NativeSelect>
          </Field>}
          {!isDocuments && <Field label="Category" htmlFor="library-type">
            <NativeSelect id="library-type" value={params.get("type") ?? ""} onChange={event => filter("type", event.target.value)}>
              <option value="">All categories</option>{data.types.map(type => <option key={type} value={type}>{humanize(type)}</option>)}
            </NativeSelect>
          </Field>}
          {isDocuments && <Field label="Status" htmlFor="library-status">
            <NativeSelect id="library-status" value={params.get("status") ?? ""} onChange={event => filter("status", event.target.value)}>
              <option value="">All statuses</option>{["ready", "uploaded", "extracting", "extracted", "distilling", "cataloged", "skipped", "failed"].map(status =>
                <option key={status} value={status}>{humanize(status)}</option>)}
            </NativeSelect>
          </Field>}
          <Field label="Sort" htmlFor="library-sort">
            <NativeSelect id="library-sort" value={params.get("sort") ?? (q ? "relevance" : "recent")} onChange={event => filter("sort", event.target.value)}>
              {q && <option value="relevance">Most relevant</option>}<option value="recent">Most recent</option><option value="name">Name</option>
            </NativeSelect>
          </Field>
          </div>
        </div>
        {data.isAdmin && view !== "review" && data.counts.review > 0 && <div className="flex flex-wrap items-center gap-2" role="status">
          <span className="text-ui-body-sm text-text2">{data.counts.review === 1 ? "1 shared concept waits" : `${data.counts.review.toLocaleString()} shared concepts wait`} for your approval before the team can use {data.counts.review === 1 ? "it" : "them"}.</span>
          <Button size="sm" onClick={() => change({ view: "review", page: null, id: null, type: null, status: null, layer: null, documentId: null })}>Review now</Button>
        </div>}
        {params.get("documentId") && <div className="flex flex-wrap items-center gap-2"><span className="text-ui-body-sm text-text2">Concepts from the selected document</span><Button size="sm" variant="ghost" onClick={() => filter("documentId", "")}>Clear document filter</Button></div>}
        {error && <div className="library-error" role="alert"><span>{error}</span><Button size="sm" onClick={() => void refresh()}>Retry</Button></div>}
        {q && data.searchMode === "keyword" && !loading && <p role="status" className="text-ui-body-sm text-text2">Meaning-based search is unavailable. Showing keyword matches.</p>}
        <div className="library-browser-workspace" data-detail-open={Boolean(selectedId)}>
          <section className="library-browser-results" aria-label={isDocuments ? "Documents" : "Concepts"} aria-busy={searching}>
            <header className="library-browser-results-head" data-picking={picked.length > 0}>
              {pickableIds.length > 0 && !searching && <Checkbox label={picked.length ? `${picked.length} selected` : "Select all"}
                checked={picked.length === 0 ? false : picked.length === pickableIds.length ? true : "indeterminate"}
                onCheckedChange={checked => setPicked(checked === true ? pickableIds : [])} />}
              {picked.length > 0 ? <ActionGroup>
                {pickedPersonal.length > 0 && <Button size="sm" disabled={busyId === "bulk"} onClick={() => void runBulk(pickedPersonal, "share", null, "shared with the team")}><Share2 aria-hidden="true" />Share with team</Button>}
                {pickedDrafts.length > 0 && <Button size="sm" variant="primary" disabled={busyId === "bulk"} onClick={() => void runBulk(pickedDrafts, "decide", { action: "approve" }, "approved")}>Approve for team</Button>}
                {pickedDrafts.length > 0 && <Button size="sm" variant="danger" disabled={busyId === "bulk"} onClick={() => void runBulk(pickedDrafts, "decide", { action: "decline" }, "declined")}>Decline</Button>}
                <Button size="sm" variant="ghost" disabled={busyId === "bulk"} onClick={() => setPicked([])}>Clear</Button>
              </ActionGroup> : <>
                <span role="status">{searching ? "Searching library…" : `${data.total.toLocaleString()} ${isDocuments ? "documents" : "concepts"}${q ? " found" : ""}`}</span>
                <span>{isDocuments ? "Private to you" : view === "review" ? "Shared concepts waiting for your approval" : "Personal knowledge and approved team concepts"}</span>
              </>}
            </header>
            {searching ? <p className="library-browser-message" role="status">Loading results…</p> : error ? null : data.total === 0 ?
              <EmptyState className="library-empty" title={q ? "No matches" : isDocuments ? "No documents yet" : view === "review" ? "Nothing to review" : "No concepts yet"}
                body={q ? "Try another phrase or clear the filters." : isDocuments ? "Upload a document to start building your library." : "Concepts distilled from your documents will appear here."}
                action={q || params.get("type") || params.get("status") || params.get("layer") ? <Button onClick={() => change({ q: null, type: null, status: null, layer: null, page: null, sort: null })}>Clear filters</Button> : undefined} />
              : <ul className="library-browser-list">
                {isDocuments ? data.documents.map(doc => <li key={doc.id} data-selected={doc.id === selectedId}>
                  <FileText aria-hidden="true" size={18} />
                  <div className="library-browser-row-copy">
                    <Link href={rowHref(doc.id)} prefetch={false} onNavigate={event => { event.preventDefault(); change({ id: doc.id }); }} aria-current={doc.id === selectedId ? "true" : undefined}>{doc.filename}</Link>
                    <span>{formatSize(doc.sizeBytes)} · <LocalDateTime value={doc.createdAt} /></span>
                  </div>
                  <Badge variant={statusVariant(doc.status)}>{humanize(doc.status)}</Badge>
                  <OverflowMenu label={`Actions for ${doc.filename}`}>
                    <MenuItem danger disabled={busyId === doc.id} onClick={() => void removeDocument(doc.id)}><Trash2 aria-hidden="true" />Remove from library</MenuItem>
                  </OverflowMenu>
                </li>) : data.concepts.map(concept => <li key={concept.id} data-selected={concept.id === selectedId || picked.includes(concept.id)}>
                  {pickableIds.length > 0 && (pickable(concept)
                    ? <CheckboxControl aria-label={`Select ${concept.title}`} checked={picked.includes(concept.id)}
                      onCheckedChange={checked => setPicked(checked === true ? [...picked, concept.id] : picked.filter(id => id !== concept.id))} />
                    : <span className="size-4 shrink-0" aria-hidden="true" />)}
                  <div className="library-browser-row-copy">
                    <Link href={rowHref(concept.id)} prefetch={false} onNavigate={event => { event.preventDefault(); change({ id: concept.id }); }} aria-current={concept.id === selectedId ? "true" : undefined}>{concept.title}</Link>
                    <span>{concept.description || concept.path}</span>
                    <small>{humanize(concept.type)} · {concept.layer === "personal" ? "Personal" : "Team"} · <LocalDateTime value={concept.updatedAt} /></small>
                  </div>
                  <SharingPill sharing={concept.sharing} />
                  {(concept.layer === "personal" || data.isAdmin) && conceptActions(concept)}
                </li>)}
              </ul>}
            <footer className="library-browser-pagination">
              <span>{data.total ? `${(data.page - 1) * data.pageSize + 1}–${Math.min(data.page * data.pageSize, data.total)} of ${data.total.toLocaleString()}` : "0 results"}</span>
              <ActionGroup><Button size="sm" disabled={searching || data.page <= 1} onClick={() => change({ page: String(data.page - 1), id: null })}>Previous</Button>
                <span className="tabular-nums">{data.page} / {pageCount}</span>
                <Button size="sm" disabled={searching || data.page >= pageCount} onClick={() => change({ page: String(data.page + 1), id: null })}>Next</Button></ActionGroup>
            </footer>
          </section>
          {selectedId && <section className="library-browser-detail" aria-label="Library details" ref={detailRef} tabIndex={-1}
            onKeyDown={event => { if (event.key === "Escape") { event.stopPropagation(); closeDetail(); } }}>
            <Button variant="ghost" size="sm" onClick={closeDetail}><ArrowLeft aria-hidden="true" />Back to list</Button>
            {detailError ? <div role="alert"><p>{detailError}</p><Button size="sm" onClick={() => void refresh()}>Retry details</Button></div>
              : !detail ? <p role="status">Loading details…</p>
              : selectedConcept ? <>
                <div className="flex flex-wrap items-center gap-2"><SharingPill sharing={data.concepts.find(concept => concept.id === selectedConcept.id)?.sharing
                  ?? (selectedConcept.userId === null ? selectedConcept.status === "stable" ? "shared" : selectedConcept.status === "draft" ? "awaiting_approval" : "retired" : null)} /><span className="text-ui-body-sm text-text2">{humanize(selectedConcept.type)}</span></div>
                <h2>{selectedConcept.title}</h2>
                {(selectedConcept.userId !== null || data.isAdmin) && <Button onClick={() => setEditingConcept(selectedConcept)}>Edit concept</Button>}
                <p className="text-ui-body-sm text-text2">{selectedConcept.description}</p>
                <article className="library-markdown"><ReactMarkdown remarkPlugins={[remarkGfm]} components={{ h1: "h3", h2: "h4", h3: "h5" }}>{selectedConcept.body}</ReactMarkdown></article>
                {selectedConcept.sources.length > 0 && <div><h3>Sources</h3><ul>{selectedConcept.sources.map((source, index) =>
                  <li key={index}>{source.resource}</li>)}</ul></div>}
                {selectedConcept.verified.length > 0 && <p className="text-ui-body-sm text-text2">Verified by {selectedConcept.verified.at(-1)!.by} · <LocalDateTime value={selectedConcept.verified.at(-1)!.at} /></p>}
                {(selectedConcept.userId !== null || data.isAdmin) && conceptActions({ ...selectedConcept, layer: selectedConcept.userId === null ? "team" : "personal" })}
              </> : selectedDocument ? <>
                <h2>{selectedDocument.filename}</h2>
                <Badge variant={statusVariant(selectedDocument.status)}>{humanize(selectedDocument.status)}</Badge>
                <p>{formatSize(selectedDocument.sizeBytes)} · <LocalDateTime value={selectedDocument.createdAt} /></p>
                <p className="text-ui-body-sm text-text2">{selectedDocument.status === "ready" ? "Private to you. OpenNeko searches and reads this file when you ask about it."
                  : selectedDocument.status === "failed" ? selectedDocument.error || "This document could not be stored. Remove it and upload it again."
                  : selectedDocument.skipReason || "OpenNeko built concepts from this document before the Library read files directly."}</p>
                {selectedDocument.status === "cataloged" && <ActionGroup align="start"><Button asChild><Link href={`/library?view=concepts&documentId=${selectedDocument.id}`} scroll={false}>View concepts</Link></Button></ActionGroup>}
              </> : null}
          </section>}
        </div>
        {toolsOpen && data.isAdmin && <section className="library-section">
          <header className="library-section-head"><h2>Starter packs</h2><Button variant="ghost" size="sm" onClick={() => setToolsOpen(false)}>Close</Button></header>
          <ul className="library-browser-list">{packs.map(pack => <li key={pack.id}><div className="library-browser-row-copy"><strong>{pack.title}</strong><span>{pack.description}</span><small>{pack.concepts} concepts</small></div>
            <Button size="sm" disabled={busyId === pack.id} onClick={() => void installPack(pack.id)}>Install</Button></li>)}</ul>
        </section>}
      </div>
      {editingConcept && <ConceptEditor concept={editingConcept} categories={data.types} onClose={() => setEditingConcept(null)} onSaved={() => { setEditingConcept(null); void refresh(); }} />}
    </div>
  );
}

function ConceptEditor({ concept, categories, onClose, onSaved }: { concept: LibraryConcept; categories: string[]; onClose: () => void; onSaved: () => void }) {
  const categoryOptions = [...new Map(["Policy", "Contract", "SOP", "Report", "Notes", "Metric definition", "Playbook", ...categories, concept.type]
    .map(value => [value.toLowerCase(), value])).values()].sort((a, b) => a.localeCompare(b));
  const [title, setTitle] = useState(concept.title);
  const [description, setDescription] = useState(concept.description ?? "");
  const [type, setType] = useState(concept.type);
  const [body, setBody] = useState(concept.body);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const dirty = title !== concept.title || description !== (concept.description ?? "") || type !== concept.type || body !== concept.body;
  useEffect(() => {
    if (!dirty) return;
    const warn = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ""; };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);
  const close = async () => {
    if (saving) return;
    if (dirty && !(await confirmDialog({ title: "Discard unsaved changes?", description: "Your changes to this concept have not been saved.", confirmLabel: "Discard changes", destructive: true }))) return;
    onClose();
  };
  const save = async (event: React.FormEvent) => {
    event.preventDefault();
    setSaving(true);
    setError(null);
    try {
      const response = await fetch(`/api/library/concepts/${concept.id}`, {
        method: "PATCH", headers: { "content-type": "application/json" },
        body: JSON.stringify({ title, description, type, body, updatedAt: concept.updatedAt }),
      });
      const result = await response.json();
      if (!response.ok) throw new Error(result.error ?? "Changes could not be saved. Try again.");
      toast.success("Concept updated", { description: result.searchIndexed === false ? "Keyword search is ready. Meaning-based search is temporarily unavailable for this revision." : undefined });
      onSaved();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Changes could not be saved. Try again.");
    } finally { setSaving(false); }
  };
  return <Sheet open onOpenChange={open => { if (!open) void close(); }}>
    <SheetContent className="w-full overflow-y-auto sm:max-w-xl">
      <SheetHeader><SheetTitle>Edit concept</SheetTitle><SheetDescription>
        {concept.userId !== null ? "Update your personal knowledge. Sharing with the team is a separate action."
          : concept.status === "draft" ? "This shared concept still needs approval after saving."
            : "Changes to this shared concept reach the team after saving."}
      </SheetDescription></SheetHeader>
      <form onSubmit={event => void save(event)} className="grid gap-5 px-4 pb-6">
        <Field label="Title" htmlFor="concept-title"><Input autoFocus id="concept-title" value={title} onChange={event => setTitle(event.target.value)} required maxLength={240} disabled={saving} /></Field>
        <Field label="Category" htmlFor="concept-type"><NativeSelect id="concept-type" value={type} onChange={event => setType(event.target.value)} disabled={saving}>
          {categoryOptions.map(category => <option key={category} value={category}>{category}</option>)}
        </NativeSelect></Field>
        <Field label="Description" htmlFor="concept-description"><Textarea id="concept-description" value={description} onChange={event => setDescription(event.target.value)} maxLength={2000} disabled={saving} rows={3} /></Field>
        <Field label="Content" htmlFor="concept-body" hint="Markdown is supported. Sources and document links are preserved."><Textarea id="concept-body" value={body} onChange={event => setBody(event.target.value)} required maxLength={200_000} disabled={saving} rows={16} /></Field>
        {error && <p role="alert" className="text-ui-body-sm text-danger">{error}</p>}
        <ActionGroup><Button disabled={saving} onClick={() => void close()}>Cancel</Button><Button type="submit" variant="primary" disabled={saving || !dirty}>{saving ? "Saving…" : "Save concept"}</Button></ActionGroup>
      </form>
    </SheetContent>
  </Sheet>;
}

function humanize(value: string): string { return value.replace(/_/g, " "); }
function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
