import { connection } from "next/server";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { graphjinAdminQuery, parseGraphjinJson } from "@/lib/graphjin-admin";
import { AdminDenied } from "../../AdminShell";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Disclosure } from "@/components/ui/disclosure";
import { Field, NativeSelect } from "@/components/ui/field";
import { SearchInput } from "@/components/ui/search-input";
import {
  GraphjinShell,
  GraphjinUnavailable,
  JsonBlock,
  SettingsCard,
  StatusBadge,
  firstParam,
  formatKind,
} from "../GraphjinAdmin";

const CATALOG_QUERY = `query AdminGraphjinCatalog {
  items: gj_catalog(where: { kind: { in: ["database", "table", "column", "relationship", "function", "query", "fragment", "capability", "system_capability", "workflow", "help"] } }, order_by: { kind: asc }, limit: 240) {
    id kind name title summary database_name schema_name table_name column_name source source_kind risk_level confidence sensitivity enabled details_json evidence_json examples_json safety_json graphql_query graphql_mutation updated_at
  }
}`;

const KINDS = ["all", "database", "table", "column", "relationship", "function", "query", "system_capability", "capability", "workflow", "help"];

type CatalogItem = {
  id: string;
  kind: string;
  name: string | null;
  title: string | null;
  summary: string | null;
  database_name: string | null;
  schema_name: string | null;
  table_name: string | null;
  column_name: string | null;
  source: string | null;
  source_kind: string | null;
  risk_level: string | null;
  confidence: number | string | null;
  sensitivity: string | null;
  enabled: boolean | null;
  details_json: unknown;
  evidence_json: unknown;
  examples_json: unknown;
  safety_json: unknown;
  graphql_query: string | null;
  graphql_mutation: string | null;
  updated_at: string | null;
};

export default async function AdminGraphjinCatalogPage({
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
  const result = await graphjinAdminQuery<{ items: CatalogItem[] }>(
    { orgId: await getOrgId(), userId: actor.userId },
    CATALOG_QUERY,
  );

  const items = result.ok ? result.data.items ?? [] : [];
  const visible = items.filter((item) => {
    if (kind !== "all" && item.kind !== kind) return false;
    if (!search) return true;
    return [item.name, item.title, item.summary, item.database_name, item.table_name, item.column_name, item.source, item.source_kind]
      .some((value) => value != null && String(value).toLowerCase().includes(search));
  });

  return (
    <GraphjinShell
      current="/admin/graphjin/catalog"
      title="Catalog"
      subtitle="Schema, source capabilities, workflows, and help that GraphJin discovered."
    >
      <form method="get" className="mb-4 grid gap-3 md:grid-cols-[minmax(0,1fr)_14rem_auto] md:items-end">
        <SearchInput label="Search catalog" name="q" defaultValue={firstParam(params.q)} placeholder="Search catalog rows" />
        <Field label="Kind" htmlFor="catalog-kind">
          <NativeSelect id="catalog-kind" name="kind" defaultValue={kind}>
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
        <GraphjinUnavailable title="Catalog" error={result.error} />
      ) : (
        <SettingsCard title="Catalog rows" copy={`${visible.length} of ${items.length} rows`}>
          {visible.length === 0 ? (
            <p className="text-ui-body-sm text-text2">No catalog rows match this filter.</p>
          ) : (
            <div className="grid gap-2">
              {visible.map((item, index) => (
                <CatalogRow key={`${item.kind}:${item.id}:${index}`} item={item} />
              ))}
            </div>
          )}
        </SettingsCard>
      )}
    </GraphjinShell>
  );
}

function CatalogRow({ item }: { item: CatalogItem }) {
  const location = [item.database_name, item.schema_name, item.table_name, item.column_name].filter(Boolean).join(".");
  const details = parseGraphjinJson<Record<string, unknown>>(item.details_json, {});
  const evidence = parseGraphjinJson<unknown>(item.evidence_json, null);
  const safety = parseGraphjinJson<unknown>(item.safety_json, null);
  const examples = parseGraphjinJson<unknown[]>(item.examples_json, []);
  const graphql = item.graphql_query || item.graphql_mutation || exampleText(examples?.[0]);
  const metadata: Array<[string, unknown]> = [
    ["Location", location],
    ["Source", item.source],
    ["Source kind", item.source_kind],
    ["Risk", item.risk_level],
    ["Sensitivity", item.sensitivity],
    ["Confidence", item.confidence],
    ["Updated", item.updated_at],
  ];

  return (
    <Disclosure
      title={
        <span className="flex min-w-0 flex-wrap items-center gap-2">
          <Badge variant="outline">{formatKind(item.kind)}</Badge>
          <span className="min-w-0 truncate">{item.title || item.name || item.id}</span>
          {typeof item.enabled === "boolean" ? <StatusBadge status={item.enabled ? "enabled" : "disabled"} /> : null}
        </span>
      }
      meta={location || item.source || undefined}
    >
      {item.summary ? <p className="mb-3 text-ui-body-sm text-text2">{item.summary}</p> : null}
      <dl className="grid gap-x-4 gap-y-1.5 text-ui-body-sm sm:grid-cols-[8rem_minmax(0,1fr)]">
        {metadata
          .filter(([, value]) => value != null && value !== "")
          .map(([label, value]) => (
            <div key={label} className="contents">
              <dt className="text-text3">{label}</dt>
              <dd className="min-w-0 break-words text-text">{String(value)}</dd>
            </div>
          ))}
      </dl>
      {graphql ? (
        <div className="mt-3">
          <JsonBlock value={graphql} />
        </div>
      ) : null}
      {Object.keys(details).length > 0 ? (
        <div className="mt-3">
          <JsonBlock value={details} />
        </div>
      ) : null}
      {hasValue(evidence) ? (
        <div className="mt-3">
          <JsonBlock value={{ evidence }} />
        </div>
      ) : null}
      {hasValue(safety) ? (
        <div className="mt-3">
          <JsonBlock value={{ safety }} />
        </div>
      ) : null}
    </Disclosure>
  );
}

function hasValue(value: unknown): boolean {
  if (Array.isArray(value)) return value.length > 0;
  if (value && typeof value === "object") return Object.keys(value).length > 0;
  return value != null && value !== "";
}

function exampleText(example: unknown): string {
  if (!example) return "";
  if (typeof example === "string") return example;
  const record = example as { query?: unknown; name?: unknown };
  if (typeof record.query === "string") return record.query;
  return typeof record.name === "string" ? record.name : JSON.stringify(example);
}
