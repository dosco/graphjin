import React, { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, Search } from "lucide-react";

import { graphqlRequest, parseJSON } from "../services/graphql";
import { DataErrorState, EmptyState, LoadingState, PageHeader, Panel, StatusPill } from "../components/ui";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";

const catalogQuery = `query CatalogExplorer {
  items: gj_catalog(where: { kind: { in: ["database", "table", "column", "relationship", "function", "query", "fragment", "capability", "system_capability", "workflow", "help"] } }, order_by: { kind: asc }, limit: 240) {
    id kind name title summary database_name schema_name table_name column_name source source_kind risk_level confidence sensitive sensitivity enabled capability_kind details_json evidence_json examples_json safety_json edges_json graphql_query graphql_mutation updated_at
  }
}`;

const kinds = ["all", "database", "table", "column", "relationship", "system_capability", "capability", "workflow", "help"];

const CatalogExplorer = ({ initialKind = "all", title = "Catalog", description = "Schema, source capabilities, workflows, and GraphJin help from gj_catalog." }) => {
  const [kind, setKind] = useState(initialKind);
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState(null);
  const { data, isLoading, error } = useQuery({
    queryKey: ["catalog-explorer"],
    queryFn: () => graphqlRequest(catalogQuery),
    staleTime: 60000,
  });
  const items = data?.data?.items || [];
  const filtered = useMemo(() => {
    const term = search.trim().toLowerCase();
    return items.filter((item) => {
      if (kind !== "all" && item.kind !== kind) {
        return false;
      }
      if (!term) {
        return true;
      }
      return [item.name, item.title, item.summary, item.database_name, item.table_name, item.column_name, item.source, item.source_kind]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(term));
    });
  }, [items, kind, search]);

  return (
    <div className="mx-auto grid max-w-7xl gap-6">
      <PageHeader
        eyebrow="Discovery"
        title={title}
        description={description}
      />

      <div className="grid gap-3 border-y bg-muted/20 px-3 py-3 md:grid-cols-[minmax(0,1fr)_14rem] md:items-center">
        <label className="relative block">
          <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
          <Input className="pl-9" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search catalog rows" />
        </label>
        <Select aria-label="Catalog row kind" value={kind} onChange={(event) => setKind(event.target.value)}>
          {kinds.map((value) => (
            <option key={value} value={value}>
              {formatKind(value)}
            </option>
          ))}
        </Select>
      </div>

      <Panel title="Catalog rows" description={`${filtered.length} of ${items.length} visible rows`} contentClassName="p-0 sm:p-0">
        {isLoading ? (
          <LoadingState label="Reading gj_catalog" />
        ) : error ? (
          <DataErrorState
            error={error}
            permissionMessage="gj_catalog is the console discovery surface. Grant catalog.read to the operator role or inspect it from local development."
            unavailableMessage="The catalog root could not be reached from the current GraphJin service."
          />
        ) : filtered.length ? (
          <div className="divide-y">
            <div className="hidden grid-cols-[8rem_minmax(0,1fr)_minmax(9rem,0.7fr)_7rem_1.5rem] gap-4 bg-muted/25 px-4 py-2 text-xs font-medium text-muted-foreground md:grid">
              <span>Kind</span><span>Name</span><span>Location</span><span>Status</span><span />
            </div>
            {filtered.map((item, index) => (
              <CatalogRow key={`${item.kind}:${item.id || item.name || item.title || "row"}:${index}`} item={item} onOpen={setSelected} />
            ))}
          </div>
        ) : (
          <EmptyState title="No catalog rows" message="Try a different filter or grant this role catalog read access." />
        )}
      </Panel>
      <Sheet open={Boolean(selected)} onOpenChange={(open) => !open && setSelected(null)}>
        <SheetContent side="right" className="grid w-[min(96vw,44rem)] grid-rows-[auto_minmax(0,1fr)] p-0 sm:w-[44rem]">
          {selected && <CatalogInspector item={selected} />}
        </SheetContent>
      </Sheet>
    </div>
  );
};

const CatalogRow = ({ item, onOpen }) => {
  const details = parseJSON(item.details_json, {});
  const location = [item.database_name, item.schema_name, item.table_name, item.column_name].filter(Boolean).join(".");
  return (
    <button type="button" onClick={() => onOpen(item)} className="grid w-full gap-2 px-4 py-3 text-left transition hover:bg-muted/45 md:grid-cols-[8rem_minmax(0,1fr)_minmax(9rem,0.7fr)_7rem_1.5rem] md:items-center md:gap-4">
      <Badge variant="outline" className="w-fit">{formatKind(item.kind)}</Badge>
      <div className="min-w-0">
        <p className="truncate text-sm font-semibold">{item.title || item.name || item.id}</p>
        {item.summary && <p className="mt-0.5 line-clamp-1 text-xs text-muted-foreground">{item.summary}</p>}
      </div>
      <span className="truncate text-xs text-muted-foreground">{location || item.source || details?.root || "—"}</span>
      <span>{typeof item.enabled === "boolean" ? <StatusPill status={item.enabled ? "enabled" : "disabled"} /> : item.risk_level ? <StatusPill severity={item.risk_level} /> : <span className="text-xs text-muted-foreground">—</span>}</span>
      <ChevronRight className="hidden size-4 text-muted-foreground md:block" aria-hidden="true" />
    </button>
  );
};

const CatalogInspector = ({ item }) => {
  const details = parseJSON(item.details_json, {});
  const evidence = parseJSON(item.evidence_json, null);
  const examples = parseJSON(item.examples_json, []);
  const safety = parseJSON(item.safety_json, null);
  const location = [item.database_name, item.schema_name, item.table_name, item.column_name].filter(Boolean).join(".");
  const query = item.graphql_query || item.graphql_mutation || exampleText(examples?.[0]);
  const metadata = [
    ["Kind", formatKind(item.kind)],
    ["Location", location],
    ["Source", item.source],
    ["Source kind", item.source_kind],
    ["Risk", item.risk_level],
    ["Sensitivity", item.sensitivity],
    ["Confidence", item.confidence],
    ["Updated", item.updated_at],
  ].filter(([, value]) => value != null && value !== "");
  return (
    <>
      <SheetHeader className="border-b px-5 py-5 pr-12 text-left">
        <div className="flex flex-wrap items-center gap-2"><Badge variant="outline">{formatKind(item.kind)}</Badge>{typeof item.enabled === "boolean" && <StatusPill status={item.enabled ? "enabled" : "disabled"} />}</div>
        <SheetTitle className="mt-2 text-xl">{item.title || item.name || item.id}</SheetTitle>
        <SheetDescription>{item.summary || "Catalog metadata and executable references from GraphJin."}</SheetDescription>
      </SheetHeader>
      <div className="min-h-0 overflow-y-auto px-5 py-5">
        {metadata.length > 0 && (
          <dl className="divide-y border-y text-sm">
            {metadata.map(([label, value]) => <div key={label} className="grid grid-cols-[8rem_minmax(0,1fr)] gap-4 py-2.5"><dt className="text-muted-foreground">{label}</dt><dd className="min-w-0 break-words font-medium">{String(value)}</dd></div>)}
          </dl>
        )}
        {query && <InspectorSection title="GraphQL"><pre className="overflow-auto whitespace-pre-wrap break-words rounded-lg bg-muted/55 p-4 text-xs leading-5"><code>{query}</code></pre></InspectorSection>}
        {Object.keys(details || {}).length > 0 && <InspectorSection title="Details"><JSONBlock value={details} /></InspectorSection>}
        {hasInspectorValue(evidence) && <InspectorSection title="Evidence"><JSONBlock value={evidence} /></InspectorSection>}
        {hasInspectorValue(safety) && <InspectorSection title="Safety"><JSONBlock value={safety} /></InspectorSection>}
      </div>
    </>
  );
};

const InspectorSection = ({ title, children }) => (
  <section className="mt-6 border-t pt-4">
    <h3 className="mb-3 text-sm font-semibold">{title}</h3>
    {children}
  </section>
);

const JSONBlock = ({ value }) => <pre className="overflow-auto whitespace-pre-wrap break-words rounded-lg bg-muted/55 p-4 text-xs leading-5"><code>{JSON.stringify(value, null, 2)}</code></pre>;

function hasInspectorValue(value) {
  if (Array.isArray(value)) {
    return value.length > 0;
  }
  if (value && typeof value === "object") {
    return Object.keys(value).length > 0;
  }
  return value != null && value !== "";
}

function exampleText(example) {
  if (!example) {
    return "";
  }
  if (typeof example === "string") {
    return example;
  }
  return example.query || example.name || JSON.stringify(example);
}

function formatKind(value) {
  return value
    .split("_")
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

export default CatalogExplorer;
