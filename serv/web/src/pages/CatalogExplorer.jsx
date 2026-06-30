import React, { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Search } from "lucide-react";

import { graphqlRequest, parseJSON } from "../services/graphql";
import { DataErrorState, EmptyState, LoadingState, PageHeader, Panel, StatusPill } from "../components/ui";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";

const catalogQuery = `query CatalogExplorer {
  items: gj_catalog(where: { kind: { in: ["database", "table", "column", "relationship", "function", "query", "fragment", "capability", "system_capability", "workflow", "help"] } }, order_by: { kind: asc }, limit: 240) {
    id kind name title summary database_name schema_name table_name column_name source source_kind risk_level confidence sensitive sensitivity enabled capability_kind details_json evidence_json examples_json safety_json edges_json graphql_query graphql_mutation updated_at
  }
}`;

const kinds = ["all", "database", "table", "column", "relationship", "system_capability", "capability", "workflow", "help"];

const CatalogExplorer = () => {
  const [kind, setKind] = useState("all");
  const [search, setSearch] = useState("");
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
        title="Catalog"
        description="Schema, source capabilities, workflows, and GraphJin help from gj_catalog."
      />

      <div className="grid gap-3 rounded-lg border bg-card p-4 shadow-xs md:grid-cols-[minmax(0,1fr)_14rem] md:items-center">
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

      <Panel title="Catalog Rows" description={`${filtered.length} of ${items.length} visible rows`}>
        {isLoading ? (
          <LoadingState label="Reading gj_catalog" />
        ) : error ? (
          <DataErrorState
            error={error}
            permissionMessage="gj_catalog is the console discovery surface. Grant catalog.read to the operator role or inspect it from local development."
            unavailableMessage="The catalog root could not be reached from the current GraphJin service."
          />
        ) : filtered.length ? (
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            {filtered.map((item) => (
              <CatalogCard key={item.id} item={item} />
            ))}
          </div>
        ) : (
          <EmptyState title="No catalog rows" message="Try a different filter or grant this role catalog read access." />
        )}
      </Panel>
    </div>
  );
};

const CatalogCard = ({ item }) => {
  const details = parseJSON(item.details_json, {});
  const examples = parseJSON(item.examples_json, []);
  const location = [item.database_name, item.schema_name, item.table_name, item.column_name].filter(Boolean).join(".");
  return (
    <article className="grid min-h-52 gap-3 rounded-md border bg-background p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <Badge variant="outline" className="mb-2">{item.kind}</Badge>
          <h3 className="truncate text-sm font-semibold leading-5">{item.title || item.name || item.id}</h3>
        </div>
        {typeof item.enabled === "boolean" && <StatusPill status={item.enabled ? "enabled" : "disabled"} />}
      </div>
      {item.summary && <p className="line-clamp-3 text-sm leading-6 text-muted-foreground">{item.summary}</p>}
      {location && <span className="truncate text-xs text-muted-foreground">{location}</span>}
      <div className="flex flex-wrap gap-2">
        {item.source_kind && <Badge variant="secondary">{item.source_kind}</Badge>}
        {item.risk_level && <Badge variant="outline">{item.risk_level}</Badge>}
        {item.sensitivity && <Badge variant="outline">{item.sensitivity}</Badge>}
        {details?.root && <Badge variant="outline">{details.root}</Badge>}
      </div>
      {(item.graphql_query || item.graphql_mutation || examples?.length > 0) && (
        <pre className="max-h-32 overflow-auto rounded-md bg-muted p-3 text-xs leading-5">
          <code>{item.graphql_query || item.graphql_mutation || exampleText(examples[0])}</code>
        </pre>
      )}
    </article>
  );
};

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
