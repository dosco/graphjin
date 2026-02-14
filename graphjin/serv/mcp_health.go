package serv

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerHealthTools registers the check_health tool
func (ms *mcpServer) registerHealthTools() {
	if !ms.service.conf.MCP.AllowDevTools {
		return
	}

	ms.srv.AddTool(mcp.NewTool(
		"check_health",
		mcp.WithDescription("Check the health of the database connection. "+
			"Returns connection status, ping latency, connection pool statistics, "+
			"schema readiness, and table count. Use to diagnose connectivity issues."),
	), ms.handleCheckHealth)
}

// HealthResult represents the health check response
type HealthResult struct {
	Status       string          `json:"status"`
	DatabaseType string          `json:"database_type,omitempty"`
	PingLatency  string          `json:"ping_latency,omitempty"`
	SchemaReady  bool            `json:"schema_ready"`
	TableCount   int             `json:"table_count"`
	PoolStats    *PoolStatsInfo  `json:"pool_stats,omitempty"`
	Error        string          `json:"error,omitempty"`
}

// PoolStatsInfo represents database connection pool statistics
type PoolStatsInfo struct {
	MaxOpenConnections int `json:"max_open_connections"`
	OpenConnections    int `json:"open_connections"`
	InUse              int `json:"in_use"`
	Idle               int `json:"idle"`
	WaitCount          int64 `json:"wait_count"`
	WaitDuration       string `json:"wait_duration"`
}

func poolStatsFromDB(db *sql.DB) *PoolStatsInfo {
	stats := db.Stats()
	return &PoolStatsInfo{
		MaxOpenConnections: stats.MaxOpenConnections,
		OpenConnections:    stats.OpenConnections,
		InUse:              stats.InUse,
		Idle:               stats.Idle,
		WaitCount:          stats.WaitCount,
		WaitDuration:       stats.WaitDuration.String(),
	}
}

// handleCheckHealth checks database connection health
func (ms *mcpServer) handleCheckHealth(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	result := HealthResult{
		DatabaseType: ms.service.conf.DBType,
	}

	// Check if database is connected
	if ms.service.db == nil {
		result.Status = "disconnected"
		result.Error = "no database connection"
		data, _ := mcpMarshalJSON(result, true)
		return mcp.NewToolResultText(string(data)), nil
	}

	// Ping the database and measure latency
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	start := time.Now()
	err := ms.service.db.PingContext(pingCtx)
	latency := time.Since(start)

	if err != nil {
		result.Status = "unhealthy"
		result.PingLatency = latency.String()
		result.Error = fmt.Sprintf("ping failed: %v", err)
		result.PoolStats = poolStatsFromDB(ms.service.db)
		data, _ := mcpMarshalJSON(result, true)
		return mcp.NewToolResultText(string(data)), nil
	}

	result.Status = "healthy"
	result.PingLatency = latency.String()
	result.PoolStats = poolStatsFromDB(ms.service.db)

	// Check schema readiness
	if ms.service.gj != nil {
		result.SchemaReady = ms.service.gj.SchemaReady()
		if result.SchemaReady {
			result.TableCount = len(ms.service.gj.GetTables())
		}
	}

	data, err := mcpMarshalJSON(result, true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}
