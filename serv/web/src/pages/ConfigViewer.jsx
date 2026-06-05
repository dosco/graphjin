import React from "react";
import { useQuery } from "@tanstack/react-query";
import { graphqlRequest, parseJSON } from "../services/graphql";
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
  const row = data?.data?.current;
  const redactedPaths = parseJSON(row?.redacted_paths, []);

  return (
    <div className="page-stack">
      <PageHeader
        eyebrow="Configuration"
        title="Config"
        description="Read-only effective configuration from gj_config with sensitive values redacted."
      />

      {isLoading ? (
        <LoadingState label="Reading gj_config" />
      ) : error ? (
        <DataErrorState
          error={error}
          permissionMessage="gj_config is read-only here and intended for admins/operators. Grant config.read to the agentic operator role or use local development."
          unavailableMessage="The config root could not be reached from the current GraphJin service."
        />
      ) : row ? (
        <>
          <div className="metric-grid">
            <Metric label="Sources Mode" value={row.sources_used ? "On" : "Off"} />
            <Metric label="Active DB" value={row.active_database || "none"} />
            <Metric label="Catalog" value={row.catalog_revision ? "Ready" : "Pending"} detail={row.catalog_revision || "no revision"} />
            <Metric label="Redactions" value={redactedPaths.length} detail="sensitive paths hidden" />
          </div>

          <Panel title="Config Location" description={row.config_path || "No config path reported"}>
            <div className="config-meta">
              <span>Updated {row.updated_at || "unknown"}</span>
              <span>{row.id}</span>
            </div>
          </Panel>

          <div className="config-grid">
            {sections.map(([title, key]) => (
              <ConfigSection key={key} title={title} value={row[key]} />
            ))}
          </div>

          <Panel title="Redacted Config JSON" description="Raw effective config shape for inspection.">
            <pre className="json-block"><code>{JSON.stringify(parseJSON(row.config_json, {}), null, 2)}</code></pre>
          </Panel>
        </>
      ) : (
        <EmptyState title="No config row" message="gj_config did not return the current configuration row." />
      )}
    </div>
  );
};

const ConfigSection = ({ title, value }) => {
  const parsed = parseJSON(value, null);
  const count = Array.isArray(parsed) ? parsed.length : parsed && typeof parsed === "object" ? Object.keys(parsed).length : 0;
  return (
    <Panel title={title} description={`${count} entries`}>
      {count ? (
        <pre className="json-block compact"><code>{JSON.stringify(parsed, null, 2)}</code></pre>
      ) : (
        <EmptyState title={`No ${title.toLowerCase()}`} message="No entries are visible in this section." />
      )}
    </Panel>
  );
};

export default ConfigViewer;
