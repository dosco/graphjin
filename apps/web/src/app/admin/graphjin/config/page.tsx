import Link from "next/link";
import { connection } from "next/server";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { graphjinAdminQuery, parseGraphjinJson } from "@/lib/graphjin-admin";
import { AdminDenied } from "../../AdminShell";
import { Badge } from "@/components/ui/badge";
import { buttonClassName } from "@/components/ui/button";
import { Disclosure } from "@/components/ui/disclosure";
import { StatCard, StatGrid } from "@/components/ui/stat-card";
import { GraphjinShell, GraphjinUnavailable, JsonBlock, SettingsCard } from "../GraphjinAdmin";

const CONFIG_QUERY = `query AdminGraphjinConfig {
  current: gj_config(id: "current") {
    id sources_used config_path active_database sources databases relationships tables roles blocklist functions resolvers mcp config_json redacted_paths updated_at catalog_revision
  }
}`;

const SECTIONS = [
  ["Sources", "sources"],
  ["Databases", "databases"],
  ["Roles", "roles"],
  ["Tables", "tables"],
  ["Relationships", "relationships"],
  ["Functions", "functions"],
  ["Resolvers", "resolvers"],
  ["MCP", "mcp"],
] as const;

type ConfigRow = Record<(typeof SECTIONS)[number][1], unknown> & {
  id: string;
  sources_used: boolean | null;
  config_path: string | null;
  active_database: string | null;
  config_json: unknown;
  redacted_paths: unknown;
  updated_at: string | null;
  catalog_revision: string | null;
};

export default async function AdminGraphjinConfigPage() {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;

  const result = await graphjinAdminQuery<{ current: ConfigRow | null }>(
    { orgId: await getOrgId(), userId: actor.userId },
    CONFIG_QUERY,
  );
  const row = result.ok ? result.data.current : null;

  return (
    <GraphjinShell
      current="/admin/graphjin/config"
      title="Config"
      subtitle="GraphJin's effective configuration, read-only, with secrets redacted."
    >
      {!result.ok ? (
        <GraphjinUnavailable title="Config" error={result.error} />
      ) : !row ? (
        <SettingsCard title="Config" copy="GraphJin did not return its current configuration." />
      ) : (
        <ConfigView row={row} />
      )}
    </GraphjinShell>
  );
}

function ConfigView({ row }: { row: ConfigRow }) {
  const redacted = parseGraphjinJson<unknown[]>(row.redacted_paths, []);
  return (
    <>
      <StatGrid label="Config summary">
        <StatCard label="Sources mode" value={row.sources_used ? "On" : "Off"} />
        <StatCard label="Active database" value={row.active_database || "None"} />
        <StatCard
          label="Catalog"
          value={row.catalog_revision ? "Ready" : "Pending"}
          tone={row.catalog_revision ? "ok" : "warn"}
        />
        <StatCard label="Redactions" value={Array.isArray(redacted) ? redacted.length : 0} detail="Sensitive paths hidden" />
      </StatGrid>

      <SettingsCard
        title="Config location"
        copy={row.config_path || "GraphJin did not report a config path."}
        aside={
          <Link href="/admin/settings/graphjin" className={buttonClassName({ size: "sm" })}>
            Change settings
          </Link>
        }
      >
        <div className="flex flex-wrap gap-2">
          <Badge variant="outline">{row.id}</Badge>
          {row.catalog_revision ? <Badge variant="secondary">{row.catalog_revision}</Badge> : null}
          {row.updated_at ? <Badge variant="secondary">{`Updated ${row.updated_at}`}</Badge> : null}
        </div>
      </SettingsCard>

      <div className="grid gap-2">
        {SECTIONS.map(([title, key]) => {
          const value = parseGraphjinJson<unknown>(row[key], null);
          const count = Array.isArray(value)
            ? value.length
            : value && typeof value === "object"
              ? Object.keys(value).length
              : 0;
          return (
            <Disclosure key={key} title={title} meta={`${count} ${count === 1 ? "entry" : "entries"}`}>
              {count ? <JsonBlock value={value} /> : <p className="text-ui-body-sm text-text2">No entries.</p>}
            </Disclosure>
          );
        })}
        <Disclosure title="Redacted config JSON" meta="Full effective shape">
          <JsonBlock value={parseGraphjinJson<unknown>(row.config_json, {})} />
        </Disclosure>
      </div>
    </>
  );
}
