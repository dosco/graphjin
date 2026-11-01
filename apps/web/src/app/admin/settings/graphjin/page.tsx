import type { ReactNode } from "react";
import Link from "next/link";
import { connection } from "next/server";
import { asc, data_source, data_source_secret, db, desc, eq } from "@neko/db";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { getGraphjinConfigSettingsPayload } from "@/lib/graphjin-config-settings";
import { AdminDenied, AdminShell } from "../../AdminShell";
import { Button, buttonClassName } from "@/components/ui/button";
import { StatCard, StatGrid } from "@/components/ui/stat-card";
import GraphjinConfigControls from "./GraphjinConfigControls";
import SourceSecretsPanel from "./SourceSecretsPanel";

export default async function AdminGraphjinPage() {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;

  const orgId = await getOrgId();
  const [sources, graphjinConfig, secrets] = await Promise.all([
    db()
      .select({
        id: data_source.id,
        name: data_source.name,
        label: data_source.label,
        kind: data_source.kind,
        graphqlUrl: data_source.graphql_url,
        mcpUrl: data_source.mcp_url,
        subscriptionUrl: data_source.subscription_url,
        authMode: data_source.auth_mode,
        isDefault: data_source.is_default,
        enabled: data_source.enabled,
        updatedAt: data_source.updated_at,
      })
      .from(data_source)
      .where(eq(data_source.org_id, orgId))
      .orderBy(asc(data_source.name)),
    getGraphjinConfigSettingsPayload(orgId),
    db()
      .select({
        name: data_source_secret.name,
        description: data_source_secret.description,
        updatedAt: data_source_secret.updated_at,
      })
      .from(data_source_secret)
      .where(eq(data_source_secret.org_id, orgId))
      .orderBy(desc(data_source_secret.updated_at)),
  ]);

  const enabled = sources.filter((source) => source.enabled);
  const jwtCount = enabled.filter((source) => source.authMode === "jwt").length;
  const sourceConfigEnabled = graphjinConfig.settings.sourceConfigEnabled;

  return (
    <AdminShell
      title="GraphJin config"
      subtitle="Source-mode endpoints and RBAC token claims passed to GraphJin."
      back={{ href: "/admin/settings", label: "Settings" }}
    >
      <StatGrid label="GraphJin status">
        <StatCard label="Sources" value={sources.length} />
        <StatCard label="Enabled" value={enabled.length} />
        <StatCard
          label="JWT RBAC"
          value={enabled.length > 0 ? `${jwtCount}/${enabled.length}` : "0"}
          tone={enabled.length === 0 || jwtCount !== enabled.length ? "warn" : "ok"}
        />
        <StatCard
          label="MCP config"
          value={sourceConfigEnabled ? "On" : "Off"}
          tone={sourceConfigEnabled ? "ok" : "neutral"}
        />
      </StatGrid>

      <GraphjinConfigControls initial={graphjinConfig.settings} />

      <section className="settings-card">
        <div className="settings-card-head">
          <div>
            <h2 className="settings-card-title">Source graph</h2>
            <p className="settings-card-copy">
              Live GraphJin source-mode inspection and guarded config updates.
            </p>
          </div>
        </div>
        {!sourceConfigEnabled ? (
          <p className="mb-3 text-ui-body-sm text-text2">
            Turn on Ask-based config above to use these actions.
          </p>
        ) : null}
        <div className="flex flex-wrap gap-2">
          <ActionLink
            href={workSeedHref(INSPECT_SOURCE_GRAPH_SEED)}
            disabled={!sourceConfigEnabled}
          >
            Inspect live graph
          </ActionLink>
          <ActionLink
            href={workSeedHref(REGISTER_SOURCE_SEED)}
            disabled={!sourceConfigEnabled}
          >
            Register source
          </ActionLink>
          <ActionLink
            href={workSeedHref(UPDATE_ACCESS_SEED)}
            disabled={!sourceConfigEnabled}
          >
            Update access
          </ActionLink>
          <ActionLink
            href={workSeedHref(ENABLE_API_WRITES_SEED)}
            disabled={!sourceConfigEnabled}
          >
            Enable API writes
          </ActionLink>
          <ActionLink
            href={workSeedHref(ADD_ROLE_SEED)}
            disabled={!sourceConfigEnabled}
          >
            Add RBAC role
          </ActionLink>
        </div>
      </section>

      <SourceSecretsPanel
        initialSecrets={secrets.map((secret) => ({
          name: secret.name,
          description: secret.description,
          updatedAt: secret.updatedAt.toISOString(),
        }))}
      />

      <section className="settings-card">
        <div className="settings-card-head">
          <div>
            <h2 className="settings-card-title">Sources</h2>
            <p className="settings-card-copy">
              Data sources registered for GraphJin-backed reads and writes.
            </p>
          </div>
          <Link
            href="/admin/settings/data"
            className={buttonClassName({ size: "sm" })}
          >
            Edit sources
          </Link>
        </div>

        {sources.length === 0 ? (
          <p className="text-sm text-text2">No GraphJin data sources configured.</p>
        ) : (
          <div className="overflow-x-auto">
            <table
              className="w-full border-collapse text-left text-sm"
              style={{ minWidth: 1040 }}
            >
              <thead className="text-ui-caption text-text2">
                <tr>
                  <th className="whitespace-nowrap border-b border-border px-3 py-2 font-semibold">Source</th>
                  <th className="whitespace-nowrap border-b border-border px-3 py-2 font-semibold">Auth</th>
                  <th className="border-b border-border px-3 py-2 font-semibold">GraphQL</th>
                  <th className="border-b border-border px-3 py-2 font-semibold">MCP</th>
                  <th className="whitespace-nowrap border-b border-border px-3 py-2 font-semibold">Updated</th>
                  <th className="whitespace-nowrap border-b border-border px-3 py-2 font-semibold">Ask</th>
                </tr>
              </thead>
              <tbody>
                {sources.map((source) => (
                  <tr key={source.id} className="border-b border-border last:border-0">
                    <td className="whitespace-nowrap px-3 py-3 align-top">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-semibold text-text">{source.name}</span>
                        {source.isDefault ? <Badge tone="ok">default</Badge> : null}
                        {!source.enabled ? <Badge tone="warn">disabled</Badge> : null}
                      </div>
                      <div className="mt-1 text-xs text-text3">
                        {source.label ?? source.kind}
                      </div>
                    </td>
                    <td className="whitespace-nowrap px-3 py-3 align-top">
                      <Badge tone={source.authMode === "jwt" ? "ok" : "warn"}>
                        {source.authMode}
                      </Badge>
                    </td>
                    <td
                      className="max-w-[300px] px-3 py-3 align-top font-mono text-xs text-text2"
                      style={{ minWidth: 300 }}
                    >
                      <span className="break-all">{source.graphqlUrl}</span>
                    </td>
                    <td
                      className="max-w-[280px] px-3 py-3 align-top font-mono text-xs text-text2"
                      style={{ minWidth: 280 }}
                    >
                      <span className="break-all">{source.mcpUrl ?? "Not set"}</span>
                    </td>
                    <td className="whitespace-nowrap px-3 py-3 align-top text-text2">
                      {formatDate(source.updatedAt)}
                    </td>
                    <td className="whitespace-nowrap px-3 py-3 align-top">
                      <div className="flex gap-2">
                        <ActionLink
                          href={workSeedHref(inspectSourceSeed(source.name))}
                          disabled={!sourceConfigEnabled}
                        >
                          Inspect
                        </ActionLink>
                        <ActionLink
                          href={workSeedHref(updateSourceAccessSeed(source.name))}
                          disabled={!sourceConfigEnabled}
                        >
                          Access
                        </ActionLink>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <section className="settings-card">
        <div className="settings-card-head">
          <div>
            <h2 className="settings-card-title">RBAC claims</h2>
            <p className="settings-card-copy">
              Claims minted for source-mode GraphJin requests.
            </p>
          </div>
        </div>
        <div className="grid gap-2 md:grid-cols-2">
          <Claim name="sub" value="actor user id or service" />
          <Claim name="role" value="admin, member, or service" />
          <Claim name="roles" value="array containing role" />
          <Claim name="org_id" value="OpenNeko organization id" />
          <Claim name="account_id" value="OpenNeko organization id" />
        </div>
      </section>
    </AdminShell>
  );
}

const INSPECT_SOURCE_GRAPH_SEED =
  "Inspect the live GraphJin source graph. Use describe_source_graph first, then summarize the current databases, capabilities, namespaces, source-mode readiness, and RBAC posture.";

const REGISTER_SOURCE_SEED =
  "Help me register a new GraphJin source. Present Database, API, and Files as the source kinds, collect only the fields relevant to the selected kind, and create a source_config_admin register_source proposal for admin approval. Use list_source_secret_names when Database credentials need a stored secretRef name.";

const UPDATE_ACCESS_SEED =
  "Review GraphJin source access. Use describe_source_graph first, ask me to confirm the source name and desired read/write/delete access, then create a source_config_admin set_source_access proposal.";

const ENABLE_API_WRITES_SEED =
  "Enable governed writes on a GraphJin API source. Use describe_source_graph and list_openapi_specs first, then confirm with me the source name, the spec key, the operation id, the roles allowed to call it, and whether write access is authenticated or admin. Create one source_config_admin enable_api_writes proposal for admin approval. Database sources stay read-only.";

const ADD_ROLE_SEED =
  "Help me add a GraphJin RBAC role. Inspect the live source graph if useful, then propose a source_config_admin add_role change for admin approval with the role name and JWT match expression.";

function inspectSourceSeed(sourceName: string): string {
  return `Inspect the live GraphJin source graph for the OpenNeko data source "${sourceName}". Use describe_source_graph first and summarize its current databases, capabilities, namespaces, and RBAC posture.`;
}

function updateSourceAccessSeed(sourceName: string): string {
  return `Review GraphJin access for the OpenNeko data source "${sourceName}". Use describe_source_graph first, map the relevant GraphJin source name, ask me to confirm the desired read/write/delete access, then create a source_config_admin set_source_access proposal.`;
}

function workSeedHref(seed: string): string {
  return `/work?seed=${encodeURIComponent(seed)}`;
}

function ActionLink({
  href,
  disabled,
  children,
}: {
  href: string;
  disabled: boolean;
  children: ReactNode;
}) {
  if (disabled) {
    return (
      <Button size="sm" disabled>
        {children}
      </Button>
    );
  }
  return (
    <Link href={href} className={buttonClassName({ size: "sm" })}>
      {children}
    </Link>
  );
}

function Badge({
  children,
  tone,
}: {
  children: ReactNode;
  tone: "ok" | "warn";
}) {
  return (
    <span
      className={`whitespace-nowrap rounded-md border px-2 py-0.5 text-ui-label font-semibold ${
        tone === "ok"
          ? "border-success-soft bg-bg text-text2"
          : "border-danger-soft bg-bg text-danger"
      }`}
    >
      {children}
    </span>
  );
}

function Claim({ name, value }: { name: string; value: string }) {
  return (
    <div className="rounded-[12px] border border-border bg-bg px-3 py-3">
      <div className="font-mono text-xs font-semibold text-text">{name}</div>
      <div className="mt-1 text-sm text-text2">{value}</div>
    </div>
  );
}

function formatDate(value: Date | null): string {
  if (!value) return "Never";
  return value.toLocaleString("en-US", {
    year: "numeric",
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}
