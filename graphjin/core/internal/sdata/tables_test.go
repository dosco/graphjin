package sdata

import (
	"reflect"
	"testing"
)

func TestParseClusteringKey(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want []string
	}{
		{
			name: "LINEAR with two columns",
			expr: "LINEAR(CREATED_AT, USER_ID)",
			want: []string{"created_at", "user_id"},
		},
		{
			name: "LINEAR with single column",
			expr: "LINEAR(ORDER_DATE)",
			want: []string{"order_date"},
		},
		{
			name: "bare parentheses",
			expr: "(CREATED_AT, USER_ID)",
			want: []string{"created_at", "user_id"},
		},
		{
			name: "single column bare parens",
			expr: "(ID)",
			want: []string{"id"},
		},
		{
			name: "empty string",
			expr: "",
			want: nil,
		},
		{
			name: "whitespace only",
			expr: "   ",
			want: nil,
		},
		{
			name: "columns with extra spaces",
			expr: "LINEAR(  CREATED_AT ,  USER_ID  )",
			want: []string{"created_at", "user_id"},
		},
		{
			name: "lowercase input",
			expr: "LINEAR(created_at, user_id)",
			want: []string{"created_at", "user_id"},
		},
		{
			name: "mixed case PascalCase columns",
			expr: "LINEAR(CreatedAt, UserId)",
			want: []string{"created_at", "user_id"},
		},
		{
			name: "expression-based key won't match columns (gracefully skipped)",
			expr: "LINEAR(CAST(CREATED_AT AS DATE), REGION)",
			want: []string{"cast(created_at_as_date)", "region"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseClusteringKey(tt.expr)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseClusteringKey(%q) = %v, want %v", tt.expr, got, tt.want)
			}
		})
	}
}

func TestAutoSetPartitionFromClustering(t *testing.T) {
	tests := []struct {
		name            string
		clusteringKeys  []string
		columns         []DBColumn
		wantPartition   string
	}{
		{
			name:           "leading temporal column becomes partition key",
			clusteringKeys: []string{"created_at", "user_id"},
			columns: []DBColumn{
				{Name: "id", Type: "bigint", PrimaryKey: true},
				{Name: "created_at", Type: "timestamp"},
				{Name: "user_id", Type: "bigint"},
			},
			wantPartition: "created_at",
		},
		{
			name:           "leading non-temporal column — no partition",
			clusteringKeys: []string{"user_id", "created_at"},
			columns: []DBColumn{
				{Name: "id", Type: "bigint", PrimaryKey: true},
				{Name: "created_at", Type: "timestamp"},
				{Name: "user_id", Type: "bigint"},
			},
			wantPartition: "",
		},
		{
			name:           "date type is temporal",
			clusteringKeys: []string{"event_date"},
			columns: []DBColumn{
				{Name: "id", Type: "bigint", PrimaryKey: true},
				{Name: "event_date", Type: "date"},
			},
			wantPartition: "event_date",
		},
		{
			name:           "timestamp_ltz is temporal (Snowflake)",
			clusteringKeys: []string{"created_at"},
			columns: []DBColumn{
				{Name: "id", Type: "bigint", PrimaryKey: true},
				{Name: "created_at", Type: "timestamp_ltz"},
			},
			wantPartition: "created_at",
		},
		{
			name:           "datetime is temporal (MySQL)",
			clusteringKeys: []string{"created_at"},
			columns: []DBColumn{
				{Name: "id", Type: "bigint", PrimaryKey: true},
				{Name: "created_at", Type: "datetime"},
			},
			wantPartition: "created_at",
		},
		{
			name:           "empty clustering keys",
			clusteringKeys: nil,
			columns: []DBColumn{
				{Name: "id", Type: "bigint", PrimaryKey: true},
			},
			wantPartition: "",
		},
		{
			name:           "clustering key column not found in table",
			clusteringKeys: []string{"nonexistent"},
			columns: []DBColumn{
				{Name: "id", Type: "bigint", PrimaryKey: true},
			},
			wantPartition: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table := NewDBTable("public", "test_table", "", tt.columns)
			table.ClusteringKeys = tt.clusteringKeys
			autoSetPartitionFromClustering(&table)
			if table.PartitionKey != tt.wantPartition {
				t.Errorf("PartitionKey = %q, want %q", table.PartitionKey, tt.wantPartition)
			}
			// When a partition key is set, the default range should be 60 days
			if tt.wantPartition != "" && table.PartitionRangeDays != 60 {
				t.Errorf("PartitionRangeDays = %d, want 60", table.PartitionRangeDays)
			}
			if tt.wantPartition == "" && table.PartitionRangeDays != 0 {
				t.Errorf("PartitionRangeDays = %d, want 0 (no partition)", table.PartitionRangeDays)
			}
		})
	}
}

func TestSnowflakeAutoPartitionFilter(t *testing.T) {
	// Verify that GetTestSnowflakeDBInfo auto-sets partition key AND default
	// range from clustering keys — enables auto-injection of time-range filter.
	di := GetTestSnowflakeDBInfo()
	for _, table := range di.Tables {
		if table.Name == "products" {
			if table.PartitionKey != "created_at" {
				t.Errorf("expected auto-derived PartitionKey %q, got %q",
					"created_at", table.PartitionKey)
			}
			// Auto-derived from temporal clustering key: default 60-day range
			if table.PartitionRangeDays != 60 {
				t.Errorf("expected PartitionRangeDays 60 for auto-derived, got %d",
					table.PartitionRangeDays)
			}
			return
		}
	}
	t.Fatal("products table not found")
}

func TestIsInList(t *testing.T) {
	list := []string{
		"foo",
		"bar_.*",
	}

	for value, isPresent := range map[string]bool{
		"foo":     true,
		"foo_bar": false,
		"baz":     false,
		"bar_foo": true,
	} {
		if isInList(value, list) != isPresent {
			expected := "not be"
			if isPresent {
				expected = "be"
			}
			t.Fatalf("expected %s to %s in %v", value, expected, list)
		}
	}
}
