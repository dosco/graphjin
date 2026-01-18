import React, { useState, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import api from "../services/api";

const SavedQueries = () => {
  const [selectedQuery, setSelectedQuery] = useState(null);
  const [filter, setFilter] = useState("");

  const { data: queriesData, isLoading, error } = useQuery({
    queryKey: ["queries"],
    queryFn: api.getQueries,
  });

  const { data: queryDetail, isLoading: detailLoading } = useQuery({
    queryKey: ["queryDetail", selectedQuery],
    queryFn: () => api.getQueryDetail(selectedQuery),
    enabled: !!selectedQuery,
  });

  const { data: fragmentsData, error: fragmentsError } = useQuery({
    queryKey: ["fragments"],
    queryFn: api.getFragments,
  });

  const queries = queriesData?.queries || [];
  const fragments = fragmentsData?.fragments || [];

  const filteredQueries = useMemo(
    () =>
      queries.filter(
        (q) =>
          q.name.toLowerCase().includes(filter.toLowerCase()) ||
          (q.namespace && q.namespace.toLowerCase().includes(filter.toLowerCase()))
      ),
    [queries, filter]
  );

  if (isLoading) {
    return (
      <div className="gj-page">
        <div className="gj-page-header">
          <h1 className="gj-page-title">Saved Queries</h1>
          <p className="gj-page-subtitle">Manage your GraphQL queries and mutations</p>
        </div>
        <div className="gj-loading">Loading queries...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="gj-page">
        <div className="gj-page-header">
          <h1 className="gj-page-title">Saved Queries</h1>
          <p className="gj-page-subtitle">Manage your GraphQL queries and mutations</p>
        </div>
        <div className="gj-card">
          <div className="gj-empty">
            <h3 className="gj-empty-title">Failed to load queries</h3>
            <p className="gj-empty-text">{error.message}</p>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="gj-page">
      <div className="gj-page-header">
        <h1 className="gj-page-title">Saved Queries</h1>
        <p className="gj-page-subtitle">
          {queries.length} queries, {fragments.length} fragments
        </p>
      </div>

      <div className="gj-queries-layout">
        {/* Query List */}
        <div className="gj-queries-sidebar">
          <div className="gj-card">
            <div className="gj-search-box">
              <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" role="img" aria-hidden="true">
                <circle cx="11" cy="11" r="8" />
                <path d="m21 21-4.35-4.35" />
              </svg>
              <input
                type="text"
                placeholder="Search queries..."
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                className="gj-search-input"
                aria-label="Search queries"
              />
            </div>

            <h3 className="gj-card-title">Queries</h3>
            <div className="gj-query-list">
              {filteredQueries.length > 0 ? (
                filteredQueries.map((query) => (
                  <button
                    key={`${query.namespace || ""}.${query.name}`}
                    className={`gj-query-item ${selectedQuery === query.name ? "active" : ""}`}
                    onClick={() => setSelectedQuery(query.name)}
                    aria-selected={selectedQuery === query.name}
                  >
                    <span className={`gj-query-type gj-query-${query.operation}`}>
                      {query.operation === "mutation" ? "M" : "Q"}
                    </span>
                    <div className="gj-query-info">
                      <span className="gj-query-name">{query.name}</span>
                      {query.namespace && (
                        <span className="gj-query-namespace">{query.namespace}</span>
                      )}
                    </div>
                  </button>
                ))
              ) : (
                <div className="gj-empty-small">
                  <p>{filter ? "No matching queries" : "No saved queries"}</p>
                </div>
              )}
            </div>

            {fragmentsError ? (
              <div className="gj-error-small">
                <p>Failed to load fragments</p>
              </div>
            ) : fragments.length > 0 ? (
              <>
                <h3 className="gj-card-title gj-mt">Fragments</h3>
                <div className="gj-fragment-list">
                  {fragments.map((fragment) => (
                    <div key={fragment.name} className="gj-fragment-item">
                      <span className="gj-fragment-icon">F</span>
                      <span className="gj-fragment-name">{fragment.name}</span>
                    </div>
                  ))}
                </div>
              </>
            ) : null}
          </div>
        </div>

        {/* Query Details */}
        <div className="gj-queries-content">
          {selectedQuery ? (
            detailLoading ? (
              <div className="gj-loading">Loading query...</div>
            ) : queryDetail ? (
              <div className="gj-card">
                <div className="gj-query-header">
                  <h3 className="gj-card-title">{queryDetail.name}</h3>
                  <span className={`gj-badge gj-badge-${queryDetail.operation}`}>
                    {queryDetail.operation}
                  </span>
                </div>
                {queryDetail.namespace && (
                  <p className="gj-query-meta">Namespace: {queryDetail.namespace}</p>
                )}

                <div className="gj-code-section">
                  <h4 className="gj-section-title">Query</h4>
                  <pre className="gj-code-block">
                    <code>{queryDetail.query}</code>
                  </pre>
                </div>

                {queryDetail.variables && Object.keys(queryDetail.variables).length > 0 && (
                  <div className="gj-code-section">
                    <h4 className="gj-section-title">Variables</h4>
                    <pre className="gj-code-block">
                      <code>{JSON.stringify(queryDetail.variables, null, 2)}</code>
                    </pre>
                  </div>
                )}
              </div>
            ) : null
          ) : (
            <div className="gj-card">
              <div className="gj-empty">
                <div className="gj-empty-icon">
                  <svg viewBox="0 0 24 24" width="48" height="48" fill="none" stroke="currentColor" strokeWidth="1.5">
                    <path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z" />
                    <polyline points="17 21 17 13 7 13 7 21" />
                    <polyline points="7 3 7 8 15 8" />
                  </svg>
                </div>
                <h3 className="gj-empty-title">Select a query</h3>
                <p className="gj-empty-text">Choose a query from the list to view its details</p>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

export default SavedQueries;
