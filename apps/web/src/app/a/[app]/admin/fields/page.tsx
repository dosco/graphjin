import Link from "next/link";
import { notFound } from "next/navigation";
import { ArrowLeft } from "lucide-react";
import { getOrgId } from "@/lib/db";
import { RecordAdminPermissionError, RecordAppRouteError } from "@/lib/records";
import { getRecordSchemaEditorModel } from "@/lib/records-schema-edit";
import { RecordAdminNav } from "@/components/records/RecordAdminNav";
import { RecordSchemaEditor } from "@/components/records/RecordSchemaEditor";
import { RecordsUnavailable } from "@/components/records/RecordsNotice";

export const dynamic = "force-dynamic";

export default async function RecordFieldsPage({
  params,
  searchParams,
}: {
  params: Promise<{ app: string }>;
  searchParams: Promise<{ object?: string }>;
}) {
  const [{ app }, query, orgId] = await Promise.all([params, searchParams, getOrgId()]);
  let model: Awaited<ReturnType<typeof getRecordSchemaEditorModel>> | null = null;
  let routeError: RecordAppRouteError | RecordAdminPermissionError | null = null;
  try {
    model = await getRecordSchemaEditorModel({ orgId, appId: app });
  } catch (error) {
    if (error instanceof RecordAppRouteError || error instanceof RecordAdminPermissionError) routeError = error;
    else throw error;
  }
  if (routeError instanceof RecordAppRouteError && routeError.status === 404) notFound();
  if (routeError) return <RecordsUnavailable message={routeError.message} />;
  if (!model) throw new Error("Fields admin page did not resolve.");
  const back = model.objects[0] ? `/a/${model.appId}/${model.objects[0].apiName}` : `/a/${model.appId}`;
  return (
    <main className="records-root records-detail-root">
      <header className="records-detail-header">
        <Link className="records-back" href={back} aria-label={`Back to ${model.appLabel}`}>
          <ArrowLeft aria-hidden="true" />
        </Link>
        <div>
          <span className="records-breadcrumb">{model.appLabel} / Admin</span>
          <h1>Fields and layouts</h1>
        </div>
        <RecordAdminNav appId={model.appId} active="fields" />
      </header>
      <RecordSchemaEditor model={model} initialObject={query.object} />
    </main>
  );
}
