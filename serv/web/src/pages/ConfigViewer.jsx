import React, { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import api from "../services/api";

const ConfigViewer = () => {
  const { data, isLoading, error } = useQuery({
    queryKey: ["config"],
    queryFn: api.getConfig,
  });

  if (isLoading) {
    return (
      <div className="gj-page">
        <div className="gj-page-header">
          <h1 className="gj-page-title">Configuration</h1>
          <p className="gj-page-subtitle">View current GraphJin configuration settings</p>
        </div>
        <div className="gj-loading">Loading configuration...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="gj-page">
        <div className="gj-page-header">
          <h1 className="gj-page-title">Configuration</h1>
          <p className="gj-page-subtitle">View current GraphJin configuration settings</p>
        </div>
        <div className="gj-card">
          <div className="gj-empty">
            <h3 className="gj-empty-title">Failed to load configuration</h3>
            <p className="gj-empty-text">{error.message}</p>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="gj-page">
      <div className="gj-page-header">
        <h1 className="gj-page-title">Configuration</h1>
        <p className="gj-page-subtitle">View current GraphJin configuration settings</p>
      </div>

      <div className="gj-config-sections">
        {data?.sections?.map((section) => (
          <ConfigSection key={section.name} section={section} />
        ))}
      </div>
    </div>
  );
};

// ConfigSection component renders a collapsible section with fields
const ConfigSection = ({ section }) => {
  // Default expansion: general, server, and database are expanded
  const defaultExpanded = ["general", "server", "database"].includes(section.name);
  const [expanded, setExpanded] = useState(defaultExpanded);

  // Don't render empty sections (except for certain ones that might have meaningful empty state)
  const hasContent = section.fields && section.fields.length > 0;
  const alwaysShowSections = ["general", "server", "database", "auth", "mcp", "rateLimiter", "compiler", "security", "cors"];

  if (!hasContent && !alwaysShowSections.includes(section.name)) {
    return null;
  }

  // Count for display in title
  const countableSections = ["roles", "tables", "functions", "resolvers"];
  const showCount = countableSections.includes(section.name);

  return (
    <div className="gj-card gj-config-section">
      <button
        className="gj-config-header"
        onClick={() => setExpanded(!expanded)}
        aria-expanded={expanded}
      >
        <h3 className="gj-card-title">
          {section.title}
          {showCount && ` (${section.fields?.length || 0})`}
        </h3>
        <span className={`gj-config-toggle ${expanded ? "open" : ""}`}>
          <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="2">
            <polyline points="6 9 12 15 18 9" />
          </svg>
        </span>
      </button>
      {expanded && (
        <div className="gj-config-content">
          {hasContent ? (
            <SectionContent section={section} />
          ) : (
            <p className="gj-config-empty">No configuration set</p>
          )}
        </div>
      )}
    </div>
  );
};

// SectionContent renders the appropriate content based on section type
const SectionContent = ({ section }) => {
  // Special rendering for complex sections
  switch (section.name) {
    case "roles":
      return <RolesContent fields={section.fields} />;
    case "tables":
      return <TablesContent fields={section.fields} />;
    case "functions":
      return <FunctionsContent fields={section.fields} />;
    case "resolvers":
      return <ResolversContent fields={section.fields} />;
    default:
      // Standard field list rendering
      return (
        <div className="gj-info-list">
          {section.fields.map((field) => (
            <ConfigField key={field.key} field={field} />
          ))}
        </div>
      );
  }
};

// ConfigField renders a single field based on its type
const ConfigField = ({ field }) => {
  const renderValue = () => {
    if (field.sensitive) {
      return <span className="gj-info-value gj-sensitive">****</span>;
    }

    switch (field.type) {
      case "bool":
        return (
          <span className={`gj-info-value ${field.value ? "gj-info-highlight" : ""}`}>
            {field.value ? "Enabled" : "Disabled"}
          </span>
        );
      case "int":
      case "float":
        return <span className="gj-info-value gj-info-number">{field.value ?? 0}</span>;
      case "duration":
        return <span className="gj-info-value">{field.value || "0s"}</span>;
      case "array":
        return field.value?.length > 0 ? (
          <div className="gj-tag-list">
            {field.value.map((v, i) => (
              <span key={i} className="gj-tag">{v}</span>
            ))}
          </div>
        ) : (
          <span className="gj-info-value gj-info-muted">None</span>
        );
      default:
        return <span className="gj-info-value">{field.value || "-"}</span>;
    }
  };

  return (
    <div className="gj-info-row">
      <span className="gj-info-label">{field.label}</span>
      {renderValue()}
    </div>
  );
};

// RolesContent renders the roles section with expandable role details
const RolesContent = ({ fields }) => {
  const [expandedRoles, setExpandedRoles] = useState({});

  const toggleRole = (roleName) => {
    setExpandedRoles((prev) => ({
      ...prev,
      [roleName]: !prev[roleName],
    }));
  };

  if (!fields || fields.length === 0) {
    return <p className="gj-config-empty">No roles configured</p>;
  }

  return (
    <div className="gj-roles-list">
      {fields.map((field) => {
        const role = field.value;
        const isExpanded = expandedRoles[role.name];

        return (
          <div key={role.name} className="gj-role-item">
            <button
              className="gj-role-header"
              onClick={() => toggleRole(role.name)}
              aria-expanded={isExpanded}
            >
              <div className="gj-role-info">
                <span className="gj-role-name">{role.name}</span>
                {role.comment && <span className="gj-role-comment">{role.comment}</span>}
                <span className="gj-role-meta">{role.tableCount} table{role.tableCount !== 1 ? "s" : ""}</span>
              </div>
              <span className={`gj-config-toggle ${isExpanded ? "open" : ""}`}>
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2">
                  <polyline points="6 9 12 15 18 9" />
                </svg>
              </span>
            </button>
            {isExpanded && role.tables && role.tables.length > 0 && (
              <div className="gj-role-tables">
                {role.match && (
                  <div className="gj-role-match">
                    <span className="gj-info-label">Match:</span>
                    <span className="gj-info-value">{role.match}</span>
                  </div>
                )}
                <table className="gj-data-table gj-data-table-compact">
                  <thead>
                    <tr>
                      <th>Table</th>
                      <th>Permissions</th>
                    </tr>
                  </thead>
                  <tbody>
                    {role.tables.map((t) => (
                      <tr key={t.name}>
                        <td>{t.schema ? `${t.schema}.${t.name}` : t.name}</td>
                        <td>
                          <div className="gj-tag-list">
                            {t.permissions?.map((p) => (
                              <span key={p} className="gj-tag gj-tag-small">{p}</span>
                            ))}
                            {t.readOnly && <span className="gj-tag gj-tag-small gj-tag-muted">read-only</span>}
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
};

// TablesContent renders the tables section
const TablesContent = ({ fields }) => {
  if (!fields || fields.length === 0) {
    return <p className="gj-config-empty">No custom table configurations</p>;
  }

  return (
    <table className="gj-data-table">
      <thead>
        <tr>
          <th>Name</th>
          <th>Schema</th>
          <th>Type</th>
          <th>Database</th>
          <th>Blocklist</th>
          <th>Columns</th>
        </tr>
      </thead>
      <tbody>
        {fields.map((field) => {
          const t = field.value;
          return (
            <tr key={`${t.schema || "default"}.${t.name}`}>
              <td>{t.name}</td>
              <td>{t.schema || "-"}</td>
              <td>{t.type || "-"}</td>
              <td>{t.database || "-"}</td>
              <td>
                {t.blocklist?.length > 0 ? (
                  <div className="gj-tag-list">
                    {t.blocklist.map((b) => (
                      <span key={b} className="gj-tag gj-tag-small">{b}</span>
                    ))}
                  </div>
                ) : (
                  "-"
                )}
              </td>
              <td>{t.columnCount || 0}</td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
};

// FunctionsContent renders the functions section
const FunctionsContent = ({ fields }) => {
  if (!fields || fields.length === 0) {
    return <p className="gj-config-empty">No functions configured</p>;
  }

  return (
    <table className="gj-data-table">
      <thead>
        <tr>
          <th>Name</th>
          <th>Schema</th>
          <th>Return Type</th>
        </tr>
      </thead>
      <tbody>
        {fields.map((field) => {
          const f = field.value;
          return (
            <tr key={f.name}>
              <td>{f.name}</td>
              <td>{f.schema || "-"}</td>
              <td>{f.returnType || "-"}</td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
};

// ResolversContent renders the resolvers section
const ResolversContent = ({ fields }) => {
  if (!fields || fields.length === 0) {
    return <p className="gj-config-empty">No resolvers configured</p>;
  }

  return (
    <table className="gj-data-table">
      <thead>
        <tr>
          <th>Name</th>
          <th>Type</th>
          <th>Table</th>
          <th>Column</th>
        </tr>
      </thead>
      <tbody>
        {fields.map((field) => {
          const r = field.value;
          return (
            <tr key={r.name}>
              <td>{r.name}</td>
              <td>{r.type || "-"}</td>
              <td>{r.table || "-"}</td>
              <td>{r.column || "-"}</td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
};

export default ConfigViewer;
