import { connection } from "next/server";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { graphjinAdminQuery } from "@/lib/graphjin-admin";
import { AdminDenied } from "../../AdminShell";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Field, NativeSelect } from "@/components/ui/field";
import { SearchInput } from "@/components/ui/search-input";
import {
  GraphjinShell,
  GraphjinUnavailable,
  JsonBlock,
  SettingsCard,
  firstParam,
  formatKind,
} from "../GraphjinAdmin";

const CODE_QUERY = `query AdminGraphjinCode {
  rows: gj_code(where: { kind: { in: ["file", "symbol", "reference", "db_reference"] } }, order_by: { path: asc }, limit: 160) {
    id kind name title summary path language symbol_kind qualified_name signature db_object_id database_name table_name column_name start_row
  }
}`;

const KINDS = ["all", "file", "symbol", "reference", "db_reference"];

type CodeRow = {
  id: string;
  kind: string;
  name: string | null;
  title: string | null;
  summary: string | null;
  path: string | null;
  language: string | null;
  symbol_kind: string | null;
  qualified_name: string | null;
  signature: string | null;
  db_object_id: string | null;
  database_name: string | null;
  table_name: string | null;
  column_name: string | null;
  start_row: number | null;
};

export default async function AdminGraphjinCodePage({
  searchParams,
}: {
  searchParams: Promise<{ q?: string | string[]; kind?: string | string[] }>;
}) {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;

  const params = await searchParams;
  const search = firstParam(params.q).toLowerCase();
  const kind = KINDS.includes(firstParam(params.kind)) ? firstParam(params.kind) : "all";
  const result = await graphjinAdminQuery<{ rows: CodeRow[] }>(
    { orgId: await getOrgId(), userId: actor.userId },
    CODE_QUERY,
  );

  const rows = result.ok ? result.data.rows ?? [] : [];
  const visible = rows.filter((row) => {
    if (kind !== "all" && row.kind !== kind) return false;
    if (!search) return true;
    return [row.name, row.title, row.summary, row.path, row.qualified_name, row.signature, row.db_object_id]
      .some((value) => value != null && value.toLowerCase().includes(search));
  });

  return (
    <GraphjinShell
      current="/admin/graphjin/code"
      title="Code"
      subtitle="Read-only files, symbols, references, and database links from GraphJin code sources."
    >
      <form method="get" className="mb-4 grid gap-3 md:grid-cols-[minmax(0,1fr)_14rem_auto] md:items-end">
        <SearchInput label="Search code" name="q" defaultValue={firstParam(params.q)} placeholder="Search files, symbols, references" />
        <Field label="Kind" htmlFor="code-kind">
          <NativeSelect id="code-kind" name="kind" defaultValue={kind}>
            {KINDS.map((value) => (
              <option key={value} value={value}>
                {formatKind(value)}
              </option>
            ))}
          </NativeSelect>
        </Field>
        <Button type="submit">Filter</Button>
      </form>

      {!result.ok ? (
        <GraphjinUnavailable title="Code" error={result.error} />
      ) : (
        <SettingsCard title="Code source" copy={`${visible.length} of ${rows.length} rows`}>
          {visible.length === 0 ? (
            <p className="text-ui-body-sm text-text2">No code rows match this filter. Configure a code source to populate this view.</p>
          ) : (
            <ul className="divide-y divide-border">
              {visible.map((row) => {
                const location = row.path ? `${row.path}${row.start_row ? `:${row.start_row}` : ""}` : row.db_object_id;
                const dbObject = [row.database_name, row.table_name, row.column_name].filter(Boolean).join(".");
                return (
                  <li key={row.id} className="grid gap-2 py-3 first:pt-0 last:pb-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="min-w-0 truncate font-semibold text-text">
                        {row.title || row.name || row.path || row.id}
                      </span>
                      <Badge variant="outline">{formatKind(row.kind)}</Badge>
                    </div>
                    {row.summary ? <p className="text-ui-body-sm text-text2">{row.summary}</p> : null}
                    {row.signature ? <JsonBlock value={row.signature} /> : null}
                    <div className="flex flex-wrap gap-2">
                      {location ? <Badge variant="secondary">{location}</Badge> : null}
                      {row.language ? <Badge variant="secondary">{row.language}</Badge> : null}
                      {row.symbol_kind ? <Badge variant="outline">{row.symbol_kind}</Badge> : null}
                      {dbObject ? <Badge variant="outline">{dbObject}</Badge> : null}
                    </div>
                  </li>
                );
              })}
            </ul>
          )}
        </SettingsCard>
      )}
    </GraphjinShell>
  );
}
