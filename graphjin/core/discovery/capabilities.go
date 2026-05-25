package discovery

import (
	"strings"

	core "github.com/dosco/graphjin/core/v3"
)

// DefaultCapabilities returns the conservative discovery contract used when
// a database type cannot be resolved.
func DefaultCapabilities() Capabilities {
	return Capabilities{
		BatchSchemaMetadata:            true,
		BatchRowCounts:                 true,
		ExactPerTableRowCountByDefault: false,
	}
}

// CapabilitiesFor returns the internal discovery capability model for a database.
func CapabilitiesFor(gj *core.GraphJin, database string) Capabilities {
	caps := DefaultCapabilities()
	if gj == nil {
		return caps
	}
	_, dbtype, err := gj.DBForDatabase(database)
	if err != nil {
		return caps
	}
	switch strings.ToLower(dbtype) {
	case "postgres", "postgresql", "cockroachdb", "cockroach":
		caps.AsyncRowCountWarmup = true
		caps.ConstraintPreflight = true
	case "bigquery", "snowflake":
		caps.AsyncRowCountWarmup = true
		caps.ConstraintPreflight = true
	case "mongodb":
		caps.BatchRowCounts = false
	}
	return caps
}
