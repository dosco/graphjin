import React, { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Code2, FileCode, Search } from "lucide-react";
import { graphqlRequest, parseJSON } from "../services/graphql";
import { DataErrorState, EmptyState, LoadingState, PageHeader, Panel, StatusPill, cx } from "../components/ui";

const codeQuery = `query CodeInsight {
  rows: gj_code(where: { kind: { in: ["file", "symbol", "reference", "db_reference"] } }, order_by: { path: asc }, limit: 160) {
    id kind name title summary path language hash symbol_kind qualified_name signature doc db_object_id database_name schema_name table_name column_name start_row start_col end_row end_col details_json updated_at
  }
}`;

const kinds = ["all", "file", "symbol", "reference", "db_reference"];

const CodeInsight = () => {
  const [kind, setKind] = useState("all");
  const [search, setSearch] = useState("");
  const { data, isLoading, error } = useQuery({
    queryKey: ["code-insight"],
    queryFn: () => graphqlRequest(codeQuery),
    staleTime: 30000,
  });
  const rows = data?.data?.rows || [];
  const visible = useMemo(() => {
    const term = search.trim().toLowerCase();
    return rows.filter((row) => {
      if (kind !== "all" && row.kind !== kind) {
        return false;
      }
      if (!term) {
        return true;
      }
      return [row.name, row.title, row.summary, row.path, row.qualified_name, row.signature, row.db_object_id]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(term));
    });
  }, [rows, kind, search]);

  return (
    <div className="page-stack">
      <PageHeader
        eyebrow="Source Insight"
        title="Code"
        description="Read-only files, symbols, references, and database links from gj_code."
      />

      <div className="toolbar">
        <div className="search-control">
          <Search size={16} aria-hidden="true" />
          <input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search files, symbols, references" />
        </div>
        <div className="segment-control" role="tablist" aria-label="Code kind">
          {kinds.map((value) => (
            <button key={value} className={cx(kind === value && "active")} onClick={() => setKind(value)} type="button">
              {value.replace("_", " ")}
            </button>
          ))}
        </div>
      </div>

      <Panel title="Code Source" description={`${visible.length} of ${rows.length} visible rows`}>
        {isLoading ? (
          <LoadingState label="Reading gj_code" />
        ) : error ? (
          <DataErrorState
            error={error}
            permissionMessage="gj_code is read-only source insight for admins/operators. Configure a code source and grant the operator role read access."
            unavailableMessage="The code root could not be reached from the current GraphJin service."
          />
        ) : visible.length ? (
          <div className="code-list">
            {visible.map((row) => (
              <CodeRow key={row.id} row={row} />
            ))}
          </div>
        ) : (
          <EmptyState title="No code rows" message="No code source rows match this filter." />
        )}
      </Panel>
    </div>
  );
};

const CodeRow = ({ row }) => {
  const details = parseJSON(row.details_json, {});
  const title = row.title || row.name || row.path || row.id;
  const location = row.path ? `${row.path}${row.start_row ? `:${row.start_row}` : ""}` : row.db_object_id;
  return (
    <article className="code-row">
      <div className="code-icon">
        {row.kind === "file" ? <FileCode size={18} aria-hidden="true" /> : <Code2 size={18} aria-hidden="true" />}
      </div>
      <div>
        <div className="code-title">
          <h3>{title}</h3>
          <StatusPill status={row.kind} />
        </div>
        {row.summary && <p>{row.summary}</p>}
        {row.signature && <pre className="mini-code inline"><code>{row.signature}</code></pre>}
        <div className="tag-row">
          {location && <span>{location}</span>}
          {row.language && <span>{row.language}</span>}
          {row.symbol_kind && <span>{row.symbol_kind}</span>}
          {row.database_name && <span>{[row.database_name, row.table_name, row.column_name].filter(Boolean).join(".")}</span>}
          {details?.kind && <span>{details.kind}</span>}
        </div>
      </div>
    </article>
  );
};

export default CodeInsight;
