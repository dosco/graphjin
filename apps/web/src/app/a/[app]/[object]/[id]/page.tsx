import Link from "next/link";
import { notFound } from "next/navigation";
import { ArrowLeft, Pencil } from "lucide-react";
import { getOrgId } from "@/lib/db";
import { readRecordDetail, RecordAppRouteError } from "@/lib/records";
import { RecordDetailCard } from "@/components/records/RecordDetailCard";
import { RecordsUnavailable } from "@/components/records/RecordsNotice";
import {
  RecordChangeTimeline,
  RecordRelatedLists,
} from "@/components/records/RecordDetailActivity";
import { PendingChangeMarker } from "@/components/records/PendingChangeMarker";
import { RecordDeleteMenu } from "@/components/records/RecordDeleteMenu";
import { buttonClassName } from "@/components/ui/button";

export const dynamic = "force-dynamic";

export default async function RecordDetailPage({
  params,
}: {
  params: Promise<{ app: string; object: string; id: string }>;
}) {
  const [{ app, object, id }, orgId] = await Promise.all([params, getOrgId()]);
  let result: Awaited<ReturnType<typeof readRecordDetail>> | null = null;
  let routeError: RecordAppRouteError | null = null;
  try {
    result = await readRecordDetail({
      orgId,
      appId: app,
      objectApiName: object,
      recordId: id,
    });
  } catch (error) {
    if (error instanceof RecordAppRouteError) {
      routeError = error;
    } else {
      throw error;
    }
  }
  if (routeError?.status === 404) notFound();
  if (routeError) return <RecordsUnavailable message={routeError.message} />;
  if (!result?.row) notFound();
  const base = `/a/${result.app.appId}/${result.view.object.apiName}`;
  const name = String(result.row[result.view.object.nameField] ?? result.row.id ?? "Record");
  return (
    <main className="records-root records-detail-root">
      <header className="records-detail-header">
        <Link className="records-back" href={base} aria-label={`Back to ${result.view.object.pluralLabel}`}>
          <ArrowLeft aria-hidden="true" />
        </Link>
        <div>
          <span className="records-breadcrumb">
            {result.app.label} / {result.view.object.pluralLabel}
          </span>
          <h1>{name}</h1>
          <PendingChangeMarker actions={result.pendingActions} />
        </div>
        {result.view.permission.canUpdate && (
          <Link
            className={buttonClassName({
              size: "sm",
              className: "records-secondary-action",
            })}
            href={`${base}/${encodeURIComponent(String(result.row.id))}/edit`}
          >
            <Pencil aria-hidden="true" /> Edit all
          </Link>
        )}
        {result.view.permission.canDelete && (
          <RecordDeleteMenu
            appId={result.app.appId}
            objectApiName={result.view.object.apiName}
            objectLabel={result.view.object.label}
            recordId={String(result.row.id)}
            recordName={name}
          />
        )}
      </header>
      <RecordDetailCard
        appId={result.app.appId}
        view={result.view}
        row={result.row}
        owners={result.owners}
        references={result.references}
        layout={result.layout}
        canUpdate={result.view.permission.canUpdate}
      />
      <RecordRelatedLists appId={result.app.appId} lists={result.relatedLists} />
      <RecordChangeTimeline history={result.history} view={result.view} />
    </main>
  );
}
