package discovery

import "testing"

func TestCapabilitiesForDBType(t *testing.T) {
	tests := []struct {
		dbtype              string
		asyncRowCountWarmup bool
		constraintPreflight bool
		batchRowCounts      bool
	}{
		{"postgres", true, true, true},
		{"cockroachdb", true, true, true},
		{"bigquery", true, true, true},
		{"snowflake", true, true, true},
		{"mysql", true, true, true},
		{"mariadb", true, true, true},
		{"mssql", true, false, true},
		{"oracle", true, false, true},
		{"sqlite", false, false, true},
		{"mongodb", false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.dbtype, func(t *testing.T) {
			caps := capabilitiesForDBType(tt.dbtype)
			if caps.AsyncRowCountWarmup != tt.asyncRowCountWarmup {
				t.Fatalf("AsyncRowCountWarmup = %v, want %v", caps.AsyncRowCountWarmup, tt.asyncRowCountWarmup)
			}
			if caps.ConstraintPreflight != tt.constraintPreflight {
				t.Fatalf("ConstraintPreflight = %v, want %v", caps.ConstraintPreflight, tt.constraintPreflight)
			}
			if caps.BatchRowCounts != tt.batchRowCounts {
				t.Fatalf("BatchRowCounts = %v, want %v", caps.BatchRowCounts, tt.batchRowCounts)
			}
			if caps.ExactPerTableRowCountByDefault {
				t.Fatal("ExactPerTableRowCountByDefault = true, want false")
			}
		})
	}
}

func TestMetadataScopeForDBType(t *testing.T) {
	tests := []struct {
		dbtype string
		want   metadataScope
	}{
		{"postgres", metadataScopeAllUserSchemas},
		{"cockroachdb", metadataScopeAllUserSchemas},
		{"snowflake", metadataScopeAllUserSchemas},
		{"mssql", metadataScopeAllUserSchemas},
		{"oracle", metadataScopeAllUserSchemas},
		{"bigquery", metadataScopeDataset},
		{"mysql", metadataScopeCurrentCatalog},
		{"mariadb", metadataScopeCurrentCatalog},
		{"sqlite", metadataScopeSingleFile},
		{"mongodb", metadataScopeCollections},
	}

	for _, tt := range tests {
		t.Run(tt.dbtype, func(t *testing.T) {
			if got := metadataScopeForDBType(tt.dbtype); got != tt.want {
				t.Fatalf("metadataScopeForDBType(%q) = %q, want %q", tt.dbtype, got, tt.want)
			}
		})
	}
}
