import React, { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { errorText, graphqlRequest, parseJSON } from "../services/graphql";
import { DataErrorState, EmptyState, LoadingState, PageHeader, Panel, StatusPill } from "../components/ui";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";

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
    <div className="grid max-w-6xl gap-6">
      <PageHeader
        title="Code"
      />

      <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_14rem] md:items-center">
        <Input aria-label="Search code" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search files and symbols" />
        <Select aria-label="Code row kind" value={kind} onChange={(event) => setKind(event.target.value)}>
          {kinds.map((value) => (
            <option key={value} value={value}>
              {formatKind(value)}
            </option>
          ))}
        </Select>
      </div>

      <Panel description={error ? undefined : `${visible.length} of ${rows.length}`}>
        {isLoading ? (
          <LoadingState />
        ) : error && /table not found/i.test(errorText(error, "")) ? (
          <EmptyState title="No code source" message="Add a code source to search files and symbols." />
        ) : error ? (
          <DataErrorState
            error={error}
            permissionMessage="Your role needs code read access, and a code source must be configured."
            unavailableMessage="Couldn't load code."
          />
        ) : visible.length ? (
          <div className="grid gap-6">
            {visible.map((row) => (
              <CodeRow key={row.id} row={row} />
            ))}
          </div>
        ) : (
          <EmptyState title="No matches" />
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
    <article className="min-w-0">
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <h3 className="min-w-0 truncate text-sm font-semibold">{title}</h3>
          <StatusPill status={row.kind} />
        </div>
        {row.summary && <p className="mt-2 text-sm leading-6 text-muted-foreground">{row.summary}</p>}
        {row.signature && (
          <pre className="mt-3 max-w-full overflow-auto rounded-md bg-accent p-3 text-xs leading-5">
            <code>{row.signature}</code>
          </pre>
        )}
        <p className="mt-1.5 font-mono text-[0.6875rem] text-muted-foreground">
          {[location, row.language, row.symbol_kind, row.database_name && [row.database_name, row.table_name, row.column_name].filter(Boolean).join("."), details?.kind].filter(Boolean).join(" · ")}
        </p>
      </div>
    </article>
  );
};

function formatKind(value) {
  return value
    .split("_")
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

export default CodeInsight;
