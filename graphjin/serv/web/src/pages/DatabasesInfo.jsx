import React from "react";
import { useQuery } from "@tanstack/react-query";
import api from "../services/api";

const DatabasesInfo = () => {
  const { data, isLoading, error } = useQuery({
    queryKey: ["databases"],
    queryFn: api.getDatabases,
    refetchInterval: 5000, // Refresh every 5 seconds for live stats
  });

  if (isLoading) {
    return (
      <div className="gj-page">
        <div className="gj-page-header">
          <h1 className="gj-page-title">Databases</h1>
          <p className="gj-page-subtitle">Database connections and statistics</p>
        </div>
        <div className="gj-loading">Loading databases...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="gj-page">
        <div className="gj-page-header">
          <h1 className="gj-page-title">Databases</h1>
          <p className="gj-page-subtitle">Database connections and statistics</p>
        </div>
        <div className="gj-card">
          <div className="gj-empty">
            <h3 className="gj-empty-title">Failed to load databases</h3>
            <p className="gj-empty-text">{error.message}</p>
          </div>
        </div>
      </div>
    );
  }

  const databases = data?.databases || [];

  return (
    <div className="gj-page">
      <div className="gj-page-header">
        <h1 className="gj-page-title">Databases</h1>
        <p className="gj-page-subtitle">
          {databases.length} database{databases.length !== 1 ? "s" : ""} configured
        </p>
      </div>

      <div className="gj-databases-grid">
        {databases.map((db) => (
          <DatabaseCard key={db.name} database={db} />
        ))}
      </div>
    </div>
  );
};

const DatabaseCard = ({ database }) => {
  const { name, type, isDefault, tableCount, pool } = database;

  return (
    <div className={`gj-card gj-database-card ${isDefault ? "gj-database-default" : ""}`}>
      <div className="gj-database-header">
        <h3 className="gj-card-title">{name}</h3>
        <div className="gj-database-badges">
          <span className="gj-badge gj-badge-type">{type || "postgres"}</span>
          {isDefault && <span className="gj-badge gj-badge-default">Default</span>}
        </div>
      </div>

      <div className="gj-info-list">
        <div className="gj-info-row">
          <span className="gj-info-label">Tables</span>
          <span className="gj-info-value gj-info-number">{tableCount}</span>
        </div>
      </div>

      {pool && (
        <>
          <div className="gj-pool-stats gj-pool-stats-compact">
            <div className="gj-pool-stat">
              <span className="gj-pool-value">{pool.open}</span>
              <span className="gj-pool-label">Open</span>
            </div>
            <div className="gj-pool-stat">
              <span className="gj-pool-value gj-pool-active">{pool.inUse}</span>
              <span className="gj-pool-label">In Use</span>
            </div>
            <div className="gj-pool-stat">
              <span className="gj-pool-value gj-pool-idle">{pool.idle}</span>
              <span className="gj-pool-label">Idle</span>
            </div>
            <div className="gj-pool-stat">
              <span className="gj-pool-value">{pool.maxOpen}</span>
              <span className="gj-pool-label">Max</span>
            </div>
          </div>

          <div className="gj-pool-details gj-pool-details-compact">
            <div className="gj-info-row">
              <span className="gj-info-label">Wait Count</span>
              <span className="gj-info-value">{pool.waitCount}</span>
            </div>
            <div className="gj-info-row">
              <span className="gj-info-label">Wait Duration</span>
              <span className="gj-info-value">{pool.waitDuration}</span>
            </div>
          </div>
        </>
      )}
    </div>
  );
};

export default DatabasesInfo;
