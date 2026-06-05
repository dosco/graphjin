import React, { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Search } from "lucide-react";
import { graphqlRequest, parseJSON } from "../services/graphql";
import { DataErrorState, EmptyState, LoadingState, PageHeader, Panel, StatusPill, cx } from "../components/ui";

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
    <div className="page-stack">
      <PageHeader
        eyebrow="Discovery"
        title="Catalog"
        description="Schema, source capabilities, workflows, and GraphJin help from gj_catalog."
      />

      <div className="toolbar">
        <div className="search-control">
          <Search size={16} aria-hidden="true" />
          <input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search catalog rows" />
        </div>
        <div className="segment-control" role="tablist" aria-label="Catalog kind">
          {kinds.map((value) => (
            <button key={value} className={cx(kind === value && "active")} onClick={() => setKind(value)} type="button">
              {value.replace("_", " ")}
            </button>
          ))}
        </div>
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
          <div className="catalog-grid">
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
    <article className="catalog-card">
      <div className="catalog-card-header">
        <div>
          <span className="kind-label">{item.kind}</span>
          <h3>{item.title || item.name || item.id}</h3>
        </div>
        {typeof item.enabled === "boolean" && <StatusPill status={item.enabled ? "enabled" : "disabled"} />}
      </div>
      {item.summary && <p>{item.summary}</p>}
      {location && <span className="muted-line">{location}</span>}
      <div className="tag-row">
        {item.source_kind && <span>{item.source_kind}</span>}
        {item.risk_level && <span>{item.risk_level}</span>}
        {item.sensitivity && <span>{item.sensitivity}</span>}
        {details?.root && <span>{details.root}</span>}
      </div>
      {(item.graphql_query || item.graphql_mutation || examples?.length > 0) && (
        <pre className="mini-code">
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

export default CatalogExplorer;
