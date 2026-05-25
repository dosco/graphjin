package discovery

// Capabilities describes the discovery shape a database can support.
type Capabilities struct {
	BatchSchemaMetadata            bool
	BatchRowCounts                 bool
	AsyncRowCountWarmup            bool
	ConstraintPreflight            bool
	ExactPerTableRowCountByDefault bool
}

// NamespaceRollup summarizes one database/schema namespace.
type NamespaceRollup struct {
	Database          string `json:"database"`
	Schema            string `json:"schema,omitempty"`
	TableCount        int    `json:"table_count"`
	ApproxRowTotal    int64  `json:"approx_row_total"`
	RowCountAvailable bool   `json:"row_count_available"`
}
