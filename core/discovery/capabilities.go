package discovery

import (
	"strings"

	core "github.com/dosco/graphjin/core/v3"
)

type metadataScope string

const (
	metadataScopeAllUserSchemas metadataScope = "all_user_schemas"
	metadataScopeCurrentCatalog metadataScope = "current_catalog"
	metadataScopeDataset        metadataScope = "dataset"
	metadataScopeSingleFile     metadataScope = "single_file"
	metadataScopeCollections    metadataScope = "collections"
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
	if gj == nil {
		return DefaultCapabilities()
	}
	_, dbtype, err := gj.DBForDatabase(database)
	if err != nil {
		return DefaultCapabilities()
	}
	return capabilitiesForDBType(dbtype)
}

func capabilitiesForDBType(dbtype string) Capabilities {
	caps := DefaultCapabilities()
	switch strings.ToLower(dbtype) {
	case "postgres", "postgresql", "cockroachdb", "cockroach":
		caps.AsyncRowCountWarmup = true
		caps.ConstraintPreflight = true
	case "bigquery", "snowflake", "mysql", "mariadb":
		caps.AsyncRowCountWarmup = true
		caps.ConstraintPreflight = true
	case "mssql", "oracle":
		caps.AsyncRowCountWarmup = true
	case "mongodb":
		caps.BatchRowCounts = false
	}
	return caps
}

func metadataScopeForDBType(dbtype string) metadataScope {
	switch strings.ToLower(dbtype) {
	case "bigquery":
		return metadataScopeDataset
	case "mysql", "mariadb":
		return metadataScopeCurrentCatalog
	case "sqlite":
		return metadataScopeSingleFile
	case "mongodb":
		return metadataScopeCollections
	default:
		return metadataScopeAllUserSchemas
	}
}
