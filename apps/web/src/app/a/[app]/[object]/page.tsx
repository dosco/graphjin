import Link from "next/link";
import { notFound } from "next/navigation";
import { Plus, Trash2, Upload } from "lucide-react";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import {
  getRecordSubstrateStatus,
  listRecordSavedViews,
  loadRecordAppShell,
  readRecordList,
  RecordAppRouteError,
} from "@/lib/records";
import { RecordTable } from "@/components/records/RecordTable";
import { RecordViewBar } from "@/components/records/RecordViewBar";
import { RecordsUnavailable } from "@/components/records/RecordsNotice";
import { SubstrateStrip } from "@/components/records/SubstrateStrip";
import { buttonClassName } from "@/components/ui/button";
import { RecordListSearch } from "@/components/records/RecordListSearch";
import { RECORD_PAGE_SIZES } from "@/lib/records-filter";
import {
  normalizeRecordSavedViewDefinition,
  type RecordFilterExpression,
} from "@neko/records";
import {
  loadRecordsVisualListFixture,
  type VisualListFixture,
} from "@/lib/records-visual-fixtures";

export const dynamic = "force-dynamic";

type SearchParams = Record<string, string | string[] | undefined>;

function value(params: SearchParams, key: string): string | undefined {
  const candidate = params[key];
  return typeof candidate === "string" ? candidate : undefined;
}

function positivePage(raw: string | undefined): number {
  const parsed = Number.parseInt(raw ?? "1", 10);
  return Number.isSafeInteger(parsed) && parsed > 0 && parsed < 10_000
    ? parsed
    : 1;
}

function pageSize(raw: string | undefined): number {
  const parsed = Number.parseInt(raw ?? "", 10);
  return (RECORD_PAGE_SIZES as readonly number[]).includes(parsed) ? parsed : 50;
}

function has(params: SearchParams, key: string): boolean {
  return Object.hasOwn(params, key);
}

function filterExpression(
  raw: string | undefined,
): RecordFilterExpression | null {
  if (!raw) return null;
  try {
    return normalizeRecordSavedViewDefinition({
      version: 1,
      filter: JSON.parse(raw),
      sort: null,
      search: null,
      myRecords: false,
      columns: [],
    }).filter;
  } catch {
    return null;
  }
}

async function loadPage(input: {
  orgId: string;
  app: string;
  object: string;
  queryParams: SearchParams;
}) {
  const shell = await loadRecordAppShell(input.orgId, input.app);
  if (shell.availability === "degraded") {
    return {
      kind: "degraded" as const,
      message: shell.degradedReason ?? "The app is degraded.",
    };
  }
  const [views, substrate, actor] = await Promise.all([
    listRecordSavedViews({
      orgId: input.orgId,
      appId: input.app,
      objectApiName: input.object,
    }),
    getRecordSubstrateStatus({ orgId: input.orgId, appId: input.app }),
    getCurrentActor(),
  ]);
  const selectedView = views.find(
    (candidate) => candidate.id === value(input.queryParams, "view"),
  );
  const selected = selectedView?.definition;
  const direction = has(input.queryParams, "direction")
    ? value(input.queryParams, "direction") === "desc"
      ? "desc"
      : "asc"
    : (selected?.sort?.direction ?? "asc");
  const sortField = has(input.queryParams, "sort")
    ? value(input.queryParams, "sort")
    : selected?.sort?.field;
  const explicitSearch = has(input.queryParams, "q");
  const search = explicitSearch
    ? (value(input.queryParams, "q") ?? null)
    : (selected?.search ?? null);
  const explicitMine = has(input.queryParams, "mine");
  const mine = explicitMine
    ? value(input.queryParams, "mine") === "true"
    : (selected?.myRecords ?? false);
  const explicitFilter = has(input.queryParams, "filter");
  const filter = explicitFilter
    ? filterExpression(value(input.queryParams, "filter"))
    : (selected?.filter ?? null);
  const page = positivePage(value(input.queryParams, "page"));
  const size = pageSize(value(input.queryParams, "size"));
  const result = await readRecordList({
    orgId: input.orgId,
    appId: input.app,
    objectApiName: input.object,
    first: size,
    after: value(input.queryParams, "after"),
    search,
    sort: sortField ? { field: sortField, direction } : undefined,
    filter,
    myRecords: mine,
    columns: selected?.columns.length ? selected.columns : undefined,
  });
  const object = shell.snapshot.objects.find(
    (candidate) => candidate.apiName === result.view.object.apiName,
  );
  const columnOrder = new Map(
    result.view.columns.map((column, index) => [column.apiName, index]),
  );
  const filterFields = shell.snapshot.fields
    .filter(
      (field) => field.objectId === object?.id && field.archivedAt === null,
    )
    .sort(
      (a, b) =>
        (columnOrder.get(String(a.apiName)) ?? Number.MAX_SAFE_INTEGER) -
        (columnOrder.get(String(b.apiName)) ?? Number.MAX_SAFE_INTEGER),
    )
    .map((field) => ({
      apiName: String(field.apiName),
      label: field.label,
      kind: field.kind,
      picklistValues: field.picklistValues,
      referenceTargets: field.referenceTargets,
    }));
  const nameLabel =
    result.view.columns.find((column) => column.columnName === result.view.object.nameField)?.label ?? "name";
  const serializedFilter = filter ? JSON.stringify(filter) : undefined;
  return {
    kind: "active" as const,
    result,
    substrate,
    actor,
    views,
    selectedView,
    filterFields,
    nameLabel,
    definition: {
      version: 1 as const,
      filter,
      sort: sortField ? { field: sortField, direction } : null,
      search,
      myRecords: mine,
      columns: result.view.columns
        .filter(
          (column) =>
            column.kind !== "owner" && column.kind !== "system_datetime",
        )
        .map((column) => column.apiName),
    },
    page,
    size,
    base: `/a/${result.app.appId}/${result.view.object.apiName}`,
    query: {
      q: explicitSearch
        ? (value(input.queryParams, "q") ?? "")
        : (search ?? undefined),
      sort: sortField,
      direction,
      mine: explicitMine
        ? mine
          ? "true"
          : "false"
        : mine
          ? "true"
          : undefined,
      filter: explicitFilter ? (serializedFilter ?? "null") : serializedFilter,
      view: selectedView?.id,
      after: value(input.queryParams, "after"),
      page: value(input.queryParams, "page"),
      back: value(input.queryParams, "back"),
      size: value(input.queryParams, "size"),
    },
  };
}

export default async function RecordObjectPage({
  params,
  searchParams,
}: {
  params: Promise<{ app: string; object: string }>;
  searchParams: Promise<SearchParams>;
}) {
  const [{ app, object }, queryParams] = await Promise.all([
    params,
    searchParams,
  ]);
  let loaded: Awaited<ReturnType<typeof loadPage>> | VisualListFixture | null =
    loadRecordsVisualListFixture(app, object);
  let routeError: RecordAppRouteError | null = null;
  if (!loaded) {
    const orgId = await getOrgId();
    try {
      loaded = await loadPage({ orgId, app, object, queryParams });
    } catch (error) {
      if (error instanceof RecordAppRouteError) {
        routeError = error;
      } else {
        throw error;
      }
    }
  }

  if (routeError?.status === 404) notFound();
  if (routeError) return <RecordsUnavailable message={routeError.message} />;
  if (!loaded) throw new Error("Record page did not resolve.");
  if (loaded.kind === "degraded")
    return <RecordsUnavailable message={loaded.message} />;

  const {
    result,
    substrate,
    actor,
    views,
    selectedView,
    filterFields,
    nameLabel,
    definition,
    page,
    size,
    base,
    query,
  } = loaded;
  return (
    <main className="records-root">
      <header className="records-object-header">
        <div className="records-object-title">
          <span className="records-breadcrumb">{result.app.label}</span>
          <span aria-hidden="true">/</span>
          <h1>{result.view.object.pluralLabel}</h1>
          <span className="records-total">
            {result.total.toLocaleString("en")} records
          </span>
        </div>
        <RecordListSearch
          key={query.view ?? "all"}
          base={base}
          query={query}
          pluralLabel={result.view.object.pluralLabel}
          nameLabel={nameLabel}
        />
        {result.view.permission.canCreate && (
          <Link
            className={buttonClassName({
              variant: "primary",
              className: "records-primary-action",
            })}
            href={`${base}/new`}
          >
            <Plus aria-hidden="true" /> New{" "}
            {result.view.object.label.toLowerCase()}
          </Link>
        )}
        {actor.role === "admin" && (
          <Link
            className={buttonClassName({
              className: "records-secondary-action",
            })}
            href={`/a/${result.app.appId}/admin?object=${encodeURIComponent(result.view.object.apiName)}`}
          >
            <Upload aria-hidden="true" /> Import
          </Link>
        )}
        <Link
          className={buttonClassName({
            className: "records-secondary-action",
          })}
          href={`${base}/recycle`}
        >
          <Trash2 aria-hidden="true" /> Recycle bin
        </Link>
      </header>
      <div className="records-list-workspace">
        <section className="records-list-main">
          <RecordViewBar
            base={base}
            objectLabel={result.view.object.pluralLabel}
            query={query}
            ownerScoped={result.view.object.visibility === "owner"}
            appId={result.app.appId}
            objectApiName={result.view.object.apiName}
            views={views}
            selectedView={selectedView ?? null}
            definition={definition}
            fields={filterFields}
            canShare={actor.role === "admin"}
          />
          <RecordTable
            appId={result.app.appId}
            view={result.view}
            rows={result.rows}
            owners={result.owners}
            references={result.references}
            pendingActions={result.pendingActions}
            total={result.total}
            cursor={result.cursor}
            page={page}
            pageSize={size}
            query={query}
          />
          <SubstrateStrip status={substrate} />
        </section>
      </div>
    </main>
  );
}
