package serv

import (
	core "github.com/dosco/graphjin/core/v3"
)

type TableIndexEntry struct {
	Name              string          `json:"name"`
	Schema            string          `json:"schema,omitempty"`
	Database          string          `json:"database,omitempty"`
	Type              string          `json:"type,omitempty"`
	Comment           string          `json:"comment,omitempty"`
	ColumnCount       int             `json:"column_count,omitempty"`
	RowCountApprox    int64           `json:"row_count_approx"`
	PrimaryKeys       []string        `json:"primary_keys,omitempty"`
	ForeignKeys       []ForeignKeyRef `json:"foreign_keys,omitempty"`
	KeyColumns        []string        `json:"key_columns,omitempty"`
	ColumnTypeSummary map[string]int  `json:"column_type_summary,omitempty"`
	JoinTargets       []string        `json:"join_targets,omitempty"`
	DuplicateIn       []string        `json:"duplicate_in,omitempty"`
}

type ForeignKeyRef struct {
	Column   string `json:"column"`
	Target   string `json:"target"`
	Database string `json:"database,omitempty"`
}

type TableProfile struct {
	RowCountApprox int64                   `json:"row_count_approx"`
	DateRanges     map[string]DateRange    `json:"date_ranges,omitempty"`
	EnumValues     map[string]EnumProfile  `json:"enum_values,omitempty"`
	NumericStats   map[string]NumericStats `json:"numeric_stats,omitempty"`
	SampleRows     []map[string]any        `json:"sample_rows,omitempty"`
}

type EnumProfile struct {
	Values        []EnumValue `json:"values,omitempty"`
	DistinctCount int64       `json:"distinct_count"`
	Truncated     bool        `json:"truncated,omitempty"`
}

type EnumValue struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

type DateRange struct {
	Min string `json:"min,omitempty"`
	Max string `json:"max,omitempty"`
}

type NumericStats struct {
	Min   string `json:"min,omitempty"`
	Max   string `json:"max,omitempty"`
	Avg   string `json:"avg,omitempty"`
	Sum   string `json:"sum,omitempty"`
	Count int64  `json:"count"`
}

type DatabaseOverview struct {
	Database            string              `json:"database"`
	AnalyticsMode       bool                `json:"analytics_mode"`
	TotalTables         int                 `json:"total_tables"`
	TotalColumns        int                 `json:"total_columns"`
	ApproxRowTotal      int64               `json:"approx_row_total,omitempty"`
	Schemas             []SchemaStats       `json:"schemas,omitempty"`
	TopNamespacesByRows []NamespaceRollup   `json:"top_namespaces_by_rows,omitempty"`
	OverallDateRange    *DateRange          `json:"overall_date_range,omitempty"`
	Functions           []core.FunctionInfo `json:"functions,omitempty"`
}

type NamespaceRollup struct {
	Database          string `json:"database"`
	Schema            string `json:"schema,omitempty"`
	TableCount        int    `json:"table_count"`
	ApproxRowTotal    int64  `json:"approx_row_total"`
	RowCountAvailable bool   `json:"row_count_available"`
}

type ListNamespacesResult struct {
	Database   string            `json:"database,omitempty"`
	Namespaces []NamespaceRollup `json:"namespaces"`
	Count      int               `json:"count"`
}

type SchemaStats struct {
	Name           string `json:"name"`
	TableCount     int    `json:"table_count"`
	ApproxRowTotal int64  `json:"approx_row_total"`
}

type TableRef struct {
	Name           string `json:"name"`
	Schema         string `json:"schema,omitempty"`
	RowCountApprox int64  `json:"row_count_approx"`
}

type SchemaInsights struct {
	Database          string             `json:"database"`
	DatabaseOverview  *DatabaseOverview  `json:"database_overview,omitempty"`
	HubTables         []HubTable         `json:"hub_tables,omitempty"`
	RelationshipPaths []RelationshipPath `json:"relationship_paths,omitempty"`
	NamespaceRouting  []NamespaceRoute   `json:"namespace_routing,omitempty"`
	QueryTemplates    []QueryTemplate    `json:"query_templates,omitempty"`
	DuplicateFlags    []DuplicateFlag    `json:"duplicate_flags,omitempty"`
	DataQualityFlags  []DataQualityFlag  `json:"data_quality_flags,omitempty"`
}

type HubTable struct {
	Name    string `json:"name"`
	FKCount int    `json:"fk_count"`
}

type RelationshipPath struct {
	From  string          `json:"from"`
	To    string          `json:"to"`
	Steps []core.PathStep `json:"steps,omitempty"`
}

type NamespaceRoute struct {
	Database   string `json:"database"`
	TableCount int    `json:"table_count"`
	Default    bool   `json:"default,omitempty"`
}

type QueryTemplate struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Table       string `json:"table"`
	Description string `json:"description,omitempty"`
	Query       string `json:"query"`
}

type DuplicateFlag struct {
	Table          string   `json:"table"`
	Schemas        []string `json:"schemas"`
	FKSchema       string   `json:"fk_schema,omitempty"`
	Recommendation string   `json:"recommendation,omitempty"`
}

type DataQualityFlag struct {
	Table   string `json:"table"`
	Column  string `json:"column,omitempty"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type DiscoveryPayload struct {
	Database         string            `json:"database"`
	Tables           []TableIndexEntry `json:"tables"`
	Insights         SchemaInsights    `json:"insights"`
	DatabaseOverview DatabaseOverview  `json:"database_overview"`
}

type DiscoveryFullPayload struct {
	Database         string             `json:"database"`
	Tables           []TableDetailEntry `json:"tables"`
	DatabaseOverview DatabaseOverview   `json:"database_overview"`
}

type TableDetailEntry struct {
	TableIndexEntry
	Schema  *core.TableSchema `json:"schema,omitempty"`
	Profile *TableProfile     `json:"profile,omitempty"`
}

type TableListOptions struct {
	Search string
	Schema string
	Limit  int
	Cursor string
}

type ListTablesResult struct {
	Database        string            `json:"database,omitempty"`
	Databases       []string          `json:"databases,omitempty"`
	Schema          string            `json:"schema,omitempty"`
	Tables          []TableIndexEntry `json:"tables"`
	Count           int               `json:"count"`
	Total           int               `json:"total"`
	TopTablesByRows []TableRef        `json:"top_tables_by_rows,omitempty"`
	NextCursor      string            `json:"next_cursor,omitempty"`
	HasMore         bool              `json:"has_more,omitempty"`
}

type DiscoveryCost struct {
	UsesLiveQueries bool   `json:"uses_live_queries"`
	Scope           string `json:"scope,omitempty"`
	Cache           string `json:"cache,omitempty"`
}

type TableSampleResult struct {
	Database string        `json:"database"`
	Schema   string        `json:"schema,omitempty"`
	Table    string        `json:"table"`
	Mode     string        `json:"mode"`
	Status   string        `json:"status"`
	Stats    *TableProfile `json:"stats,omitempty"`
	Cost     DiscoveryCost `json:"cost"`
}
