import React, { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Code2, FileCode, Search } from "lucide-react";

import { graphqlRequest, parseJSON } from "../services/graphql";
import { DataErrorState, EmptyState, LoadingState, PageHeader, Panel, StatusPill } from "../components/ui";
import { Badge } from "@/components/ui/badge";
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
    <div className="mx-auto grid max-w-7xl gap-6">
      <PageHeader
        eyebrow="Source Insight"
        title="Code"
        description="Read-only files, symbols, references, and database links from gj_code."
      />

      <div className="grid gap-3 rounded-lg border bg-card p-4 shadow-xs md:grid-cols-[minmax(0,1fr)_14rem] md:items-center">
        <label className="relative block">
          <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
          <Input className="pl-9" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search files, symbols, references" />
        </label>
        <Select aria-label="Code row kind" value={kind} onChange={(event) => setKind(event.target.value)}>
          {kinds.map((value) => (
            <option key={value} value={value}>
              {formatKind(value)}
            </option>
          ))}
        </Select>
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
          <div className="grid gap-3">
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
    <article className="grid gap-3 rounded-md border bg-background p-4 md:grid-cols-[2.5rem_minmax(0,1fr)]">
      <div className="flex size-10 items-center justify-center rounded-md border bg-muted text-muted-foreground">
        {row.kind === "file" ? <FileCode size={18} aria-hidden="true" /> : <Code2 size={18} aria-hidden="true" />}
      </div>
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <h3 className="min-w-0 truncate text-sm font-semibold">{title}</h3>
          <StatusPill status={row.kind} />
        </div>
        {row.summary && <p className="mt-2 text-sm leading-6 text-muted-foreground">{row.summary}</p>}
        {row.signature && (
          <pre className="mt-3 max-w-full overflow-auto rounded-md bg-muted p-3 text-xs leading-5">
            <code>{row.signature}</code>
          </pre>
        )}
        <div className="mt-3 flex flex-wrap gap-2">
          {location && <Badge variant="outline">{location}</Badge>}
          {row.language && <Badge variant="secondary">{row.language}</Badge>}
          {row.symbol_kind && <Badge variant="outline">{row.symbol_kind}</Badge>}
          {row.database_name && <Badge variant="outline">{[row.database_name, row.table_name, row.column_name].filter(Boolean).join(".")}</Badge>}
          {details?.kind && <Badge variant="outline">{details.kind}</Badge>}
        </div>
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
