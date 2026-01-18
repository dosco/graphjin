import React, { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import api from "../services/api";

const SchemaExplorer = () => {
  const [selectedTable, setSelectedTable] = useState(null);

  const { data: tablesData, isLoading, error } = useQuery({
    queryKey: ["tables"],
    queryFn: api.getTables,
  });

  const { data: schemaData, isLoading: schemaLoading } = useQuery({
    queryKey: ["tableSchema", selectedTable],
    queryFn: () => api.getTableSchema(selectedTable),
    enabled: !!selectedTable,
  });

  if (isLoading) {
    return (
      <div className="gj-page">
        <div className="gj-page-header">
          <h1 className="gj-page-title">Schema Explorer</h1>
          <p className="gj-page-subtitle">Browse database tables, columns, and relationships</p>
        </div>
        <div className="gj-loading">Loading tables...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="gj-page">
        <div className="gj-page-header">
          <h1 className="gj-page-title">Schema Explorer</h1>
          <p className="gj-page-subtitle">Browse database tables, columns, and relationships</p>
        </div>
        <div className="gj-card">
          <div className="gj-empty">
            <h3 className="gj-empty-title">Failed to load schema</h3>
            <p className="gj-empty-text">{error.message}</p>
          </div>
        </div>
      </div>
    );
  }

  const tables = tablesData?.tables || [];

  return (
    <div className="gj-page">
      <div className="gj-page-header">
        <h1 className="gj-page-title">Schema Explorer</h1>
        <p className="gj-page-subtitle">
          {tables.length} tables found
        </p>
      </div>

      <div className="gj-schema-layout">
        {/* Table List */}
        <div className="gj-schema-sidebar">
          <div className="gj-card">
            <h3 className="gj-card-title">Tables</h3>
            <div className="gj-table-list">
              {tables.map((table) => (
                <button
                  key={table.name}
                  className={`gj-table-item ${selectedTable === table.name ? "active" : ""}`}
                  onClick={() => setSelectedTable(table.name)}
                  aria-selected={selectedTable === table.name}
                >
                  <span className="gj-table-name">{table.name}</span>
                  <span className="gj-table-meta">{table.column_count} cols</span>
                </button>
              ))}
            </div>
          </div>
        </div>

        {/* Table Details */}
        <div className="gj-schema-content">
          {selectedTable ? (
            schemaLoading ? (
              <div className="gj-loading">Loading schema...</div>
            ) : schemaData ? (
              <div className="gj-card">
                <h3 className="gj-card-title">{schemaData.name}</h3>
                {schemaData.comment && (
                  <p className="gj-table-comment">{schemaData.comment}</p>
                )}

                {/* Columns */}
                <div className="gj-schema-section">
                  <h4 className="gj-section-title">Columns</h4>
                  <table className="gj-data-table">
                    <thead>
                      <tr>
                        <th>Name</th>
                        <th>Type</th>
                        <th>Nullable</th>
                        <th>Key</th>
                      </tr>
                    </thead>
                    <tbody>
                      {schemaData.columns?.map((col) => (
                        <tr key={col.name}>
                          <td className="gj-col-name">{col.name}</td>
                          <td className="gj-col-type">{col.type}{col.array ? "[]" : ""}</td>
                          <td>{col.nullable ? "Yes" : "No"}</td>
                          <td>
                            {col.primary_key && <span className="gj-badge gj-badge-pk">PK</span>}
                            {col.foreign_key && (
                              <span className="gj-badge gj-badge-fk" title={col.foreign_key}>FK</span>
                            )}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>

                {/* Relationships */}
                {(schemaData.relationships?.outgoing?.length > 0 ||
                  schemaData.relationships?.incoming?.length > 0) && (
                  <div className="gj-schema-section">
                    <h4 className="gj-section-title">Relationships</h4>

                    {schemaData.relationships?.outgoing?.length > 0 && (
                      <div className="gj-rel-group">
                        <h5 className="gj-rel-heading">Outgoing (references)</h5>
                        {schemaData.relationships.outgoing.map((rel) => (
                          <div key={`${rel.name}-${rel.table}-out`} className="gj-rel-item">
                            <span className="gj-rel-name">{rel.name}</span>
                            <span className="gj-rel-arrow">&rarr;</span>
                            <span className="gj-rel-table">{rel.table}</span>
                            <span className="gj-rel-type">({rel.type})</span>
                          </div>
                        ))}
                      </div>
                    )}

                    {schemaData.relationships?.incoming?.length > 0 && (
                      <div className="gj-rel-group">
                        <h5 className="gj-rel-heading">Incoming (referenced by)</h5>
                        {schemaData.relationships.incoming.map((rel) => (
                          <div key={`${rel.name}-${rel.table}-in`} className="gj-rel-item">
                            <span className="gj-rel-table">{rel.table}</span>
                            <span className="gj-rel-arrow">&rarr;</span>
                            <span className="gj-rel-name">{rel.name}</span>
                            <span className="gj-rel-type">({rel.type})</span>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                )}
              </div>
            ) : null
          ) : (
            <div className="gj-card">
              <div className="gj-empty">
                <div className="gj-empty-icon">
                  <svg viewBox="0 0 24 24" width="48" height="48" fill="none" stroke="currentColor" strokeWidth="1.5">
                    <ellipse cx="12" cy="5" rx="9" ry="3" />
                    <path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3" />
                    <path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5" />
                  </svg>
                </div>
                <h3 className="gj-empty-title">Select a table</h3>
                <p className="gj-empty-text">Choose a table from the list to view its schema</p>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

export default SchemaExplorer;
