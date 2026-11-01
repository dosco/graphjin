import { connection } from "next/server";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { graphjinAdminGet } from "@/lib/graphjin-admin";
import { AdminDenied } from "../../AdminShell";
import { Badge, type BadgeVariant } from "@/components/ui/badge";
import { Disclosure } from "@/components/ui/disclosure";
import { GraphjinShell, GraphjinUnavailable, JsonBlock, SettingsCard } from "../GraphjinAdmin";

type OpenApiParameter = { name?: string; in?: string; required?: boolean; description?: string; schema?: unknown };
type OpenApiOperation = {
  operationId?: string;
  summary?: string;
  description?: string;
  tags?: string[];
  parameters?: OpenApiParameter[];
  requestBody?: unknown;
  responses?: Record<string, unknown>;
};
type OpenApiSpec = {
  openapi?: string;
  info?: { title?: string; version?: string; description?: string };
  servers?: Array<{ url?: string }>;
  paths?: Record<string, Record<string, unknown>>;
};

const METHODS = ["get", "post", "put", "patch", "delete"] as const;
const METHOD_VARIANTS: Record<string, BadgeVariant> = {
  get: "success",
  post: "watch",
  put: "watch",
  patch: "watch",
  delete: "danger",
};

export default async function AdminGraphjinApiPage() {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;

  const result = await graphjinAdminGet<OpenApiSpec>(
    { orgId: await getOrgId(), userId: actor.userId },
    "openapi.json",
  );

  return (
    <GraphjinShell
      current="/admin/graphjin/api"
      title="API reference"
      subtitle="REST operations GraphJin generates from saved queries, from its OpenAPI document."
    >
      {!result.ok ? <GraphjinUnavailable title="API reference" error={result.error} /> : <ApiReference spec={result.data} />}
    </GraphjinShell>
  );
}

function ApiReference({ spec }: { spec: OpenApiSpec }) {
  const operations = Object.entries(spec.paths ?? {}).flatMap(([path, item]) =>
    METHODS.flatMap((method) => {
      const operation = item?.[method] as OpenApiOperation | undefined;
      return operation ? [{ path, method, operation }] : [];
    }),
  );

  return (
    <>
      <SettingsCard
        title={spec.info?.title ?? "GraphJin API"}
        copy={spec.info?.description}
        aside={spec.info?.version ? <Badge variant="outline">{spec.info.version}</Badge> : undefined}
      >
        <div className="flex flex-wrap gap-2">
          {spec.openapi ? <Badge variant="secondary">OpenAPI {spec.openapi}</Badge> : null}
          <Badge variant="secondary">{`${operations.length} operation${operations.length === 1 ? "" : "s"}`}</Badge>
          {(spec.servers ?? []).map((server) =>
            server.url ? (
              <Badge key={server.url} variant="outline">
                {server.url}
              </Badge>
            ) : null,
          )}
        </div>
      </SettingsCard>

      <SettingsCard title="Operations" copy="Saved queries exposed under /api/v1/rest.">
        {operations.length === 0 ? (
          <p className="text-ui-body-sm text-text2">No REST operations. Save a query in GraphJin to expose it here.</p>
        ) : (
          <div className="grid gap-2">
            {operations.map(({ path, method, operation }) => (
              <Disclosure
                key={`${method} ${path}`}
                title={
                  <span className="flex min-w-0 flex-wrap items-center gap-2">
                    <Badge variant={METHOD_VARIANTS[method] ?? "muted"}>{method.toUpperCase()}</Badge>
                    <span className="min-w-0 break-all font-mono text-xs">{path}</span>
                  </span>
                }
                meta={operation.summary ?? operation.operationId}
              >
                {operation.description ? (
                  <p className="mb-3 text-ui-body-sm text-text2">{operation.description}</p>
                ) : null}
                {operation.parameters?.length ? (
                  <dl className="mb-3 grid gap-x-4 gap-y-1.5 text-ui-body-sm sm:grid-cols-[12rem_minmax(0,1fr)]">
                    {operation.parameters.map((parameter, index) => (
                      <div key={`${parameter.in}:${parameter.name}:${index}`} className="contents">
                        <dt className="font-mono text-xs text-text">
                          {parameter.name}
                          {parameter.required ? " *" : ""}
                          <span className="ml-1 text-text3">({parameter.in})</span>
                        </dt>
                        <dd className="min-w-0 break-words text-text2">{parameter.description ?? "—"}</dd>
                      </div>
                    ))}
                  </dl>
                ) : null}
                {operation.requestBody ? <JsonBlock value={{ requestBody: operation.requestBody }} /> : null}
                {operation.responses ? (
                  <div className="mt-3">
                    <JsonBlock value={{ responses: operation.responses }} />
                  </div>
                ) : null}
              </Disclosure>
            ))}
          </div>
        )}
      </SettingsCard>

      <Disclosure title="OpenAPI document">
        <JsonBlock value={spec} />
      </Disclosure>
    </>
  );
}
