import React from "react";
import { useQuery } from "@tanstack/react-query";

import { graphqlRequest, parseJSON, relativeTime } from "../services/graphql";
import { DataErrorState, EmptyState, LoadingState, Metric, PageHeader, Panel } from "../components/ui";

const configQuery = `query ConfigViewer {
  current: gj_config(id: "current") {
    id sources_used config_path active_database sources databases relationships tables roles blocklist functions resolvers mcp config_json redacted_paths updated_at catalog_revision
  }
}`;

const sections = [
  ["Sources", "sources"],
  ["Databases", "databases"],
  ["Roles", "roles"],
  ["Tables", "tables"],
  ["Relationships", "relationships"],
  ["Functions", "functions"],
  ["Resolvers", "resolvers"],
  ["MCP", "mcp"],
];

const ConfigViewer = () => {
  const { data, isLoading, error } = useQuery({
    queryKey: ["config-viewer"],
    queryFn: () => graphqlRequest(configQuery),
    staleTime: 30000,
  });
  // gj_config(id:) comes back as a one-row list.
  const current = data?.data?.current;
  const row = Array.isArray(current) ? current[0] : current;
  const redactedPaths = parseJSON(row?.redacted_paths, []);
  const filled = sections.filter(([, key]) => entryCount(row?.[key]) > 0);
  const empty = sections.filter(([, key]) => entryCount(row?.[key]) === 0);

  return (
    <div className="grid max-w-6xl gap-6">
      <PageHeader
        title="Config"
        description={row?.config_path ? `${row.config_path} · secrets redacted` : "Secrets are redacted."}
      />

      {isLoading ? (
        <LoadingState />
      ) : error ? (
        <DataErrorState
          error={error}
          permissionMessage="Your role needs config.read to see this."
          unavailableMessage="Couldn't load the config."
        />
      ) : row ? (
        <>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <Metric label="Active DB" value={row.active_database || "none"} />
            <Metric label="Sources" value={row.sources_used ? "On" : "Off"} />
            <Metric label="Catalog" value={row.catalog_revision ? "Ready" : "Pending"} detail={row.catalog_revision ? row.catalog_revision.slice(0, 12) : undefined} />
            <Metric label="Redacted" value={redactedPaths.length} detail={row.updated_at ? `updated ${relativeTime(row.updated_at)}` : undefined} />
          </div>

          <div className="grid gap-x-12 gap-y-6 xl:grid-cols-2">
            {filled.map(([title, key]) => (
              <ConfigSection key={key} title={title} value={row[key]} />
            ))}
          </div>
          {empty.length > 0 && (
            <p className="text-sm text-muted-foreground">
              Not set: {empty.map(([title]) => title.toLowerCase()).join(", ")}.
            </p>
          )}

          <Panel title="JSON">
            <JSONBlock value={parseJSON(row.config_json, {})} />
          </Panel>
        </>
      ) : (
        <EmptyState title="No config found" />
      )}
    </div>
  );
};

function entryCount(value) {
  const parsed = parseJSON(value, null);
  return Array.isArray(parsed) ? parsed.length : parsed && typeof parsed === "object" ? Object.keys(parsed).length : 0;
}

const ConfigSection = ({ title, value }) => (
  <Panel title={`${title} · ${entryCount(value)}`}>
    <JSONBlock value={parseJSON(value, null)} compact />
  </Panel>
);

function JSONBlock({ value, compact = false }) {
  return (
    <pre className={`max-h-[520px] overflow-auto rounded-md bg-accent p-4 text-xs leading-5 ${compact ? "max-h-80" : ""}`}>
      <code>{JSON.stringify(value, null, 2)}</code>
    </pre>
  );
}

export default ConfigViewer;
