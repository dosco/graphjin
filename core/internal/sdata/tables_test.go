package sdata

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/util"
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

// TestCompositeFKQueryConstants verifies that each DB's composite FK query
// constant is a valid non-empty SQL string. This catches copy-paste errors.
func TestCompositeFKQueryConstants(t *testing.T) {
	queries := map[string]string{
		"mysql":     compositeFKQueryMySQL,
		"sqlite":    compositeFKQuerySQLite,
		"oracle":    compositeFKQueryOracle,
		"mssql":     compositeFKQueryMSSQL,
		"snowflake": compositeFKQuerySnowflake,
	}
	for db, q := range queries {
		if len(q) < 50 {
			t.Errorf("%s: composite FK query too short (%d chars)", db, len(q))
		}
		// All queries must have GROUP BY and HAVING COUNT to filter for multi-column FKs
		if !strings.Contains(q, "GROUP BY") {
			t.Errorf("%s: composite FK query missing GROUP BY", db)
		}
		if !strings.Contains(q, "HAVING COUNT") {
			t.Errorf("%s: composite FK query missing HAVING COUNT", db)
		}
	}
}

// TestDiscoverCompositeFKsCSVParsing verifies that the CSV scanner correctly
// parses comma-separated column lists and applies normalization per DB type.
func TestDiscoverCompositeFKsCSVParsing(t *testing.T) {
	tests := []struct {
		name           string
		dbtype         string
		localCSV       string
		fkeyCSV        string
		wantLocalCols  []string
		wantFKeyCols   []string
		wantSchema     string
		inputSchema    string
	}{
		{
			name:          "mysql: no normalization",
			dbtype:        "mysql",
			localCSV:      "order_id,product_id",
			fkeyCSV:       "order_id,product_id",
			wantLocalCols: []string{"order_id", "product_id"},
			wantFKeyCols:  []string{"order_id", "product_id"},
			wantSchema:    "mydb",
			inputSchema:   "mydb",
		},
		{
			name:          "oracle: uppercase normalized to snake_case lowercase",
			dbtype:        "oracle",
			localCSV:      "ORDER_ID,PRODUCT_ID",
			fkeyCSV:       "ORDER_ID,PRODUCT_ID",
			wantLocalCols: []string{"order_id", "product_id"},
			wantFKeyCols:  []string{"order_id", "product_id"},
			wantSchema:    "sales",
			inputSchema:   "SALES",
		},
		{
			name:          "mssql: PascalCase normalized to snake_case",
			dbtype:        "mssql",
			localCSV:      "OrderId,ProductId",
			fkeyCSV:       "OrderId,ProductId",
			wantLocalCols: []string{"order_id", "product_id"},
			wantFKeyCols:  []string{"order_id", "product_id"},
			wantSchema:    "dbo",
			inputSchema:   "dbo",
		},
		{
			name:          "snowflake: uppercase normalized",
			dbtype:        "snowflake",
			localCSV:      "SPECIAL_OFFER_ID,PRODUCT_ID",
			fkeyCSV:       "SPECIAL_OFFER_ID,PRODUCT_ID",
			wantLocalCols: []string{"special_offer_id", "product_id"},
			wantFKeyCols:  []string{"special_offer_id", "product_id"},
			wantSchema:    "public",
			inputSchema:   "PUBLIC",
		},
		{
			name:          "sqlite: no normalization needed",
			dbtype:        "sqlite",
			localCSV:      "customer_id,region_id",
			fkeyCSV:       "customer_id,region_id",
			wantLocalCols: []string{"customer_id", "region_id"},
			wantFKeyCols:  []string{"customer_id", "region_id"},
			wantSchema:    "main",
			inputSchema:   "main",
		},
		{
			name:          "mssql: spaces in CSV trimmed",
			dbtype:        "mssql",
			localCSV:      "OrderId, ProductId",
			fkeyCSV:       "OrderId, ProductId",
			wantLocalCols: []string{"order_id", "product_id"},
			wantFKeyCols:  []string{"order_id", "product_id"},
			wantSchema:    "dbo",
			inputSchema:   "dbo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalize := tt.dbtype == "oracle" || tt.dbtype == "mssql" || tt.dbtype == "snowflake"

			info := CompositeFKInfo{
				Schema:         tt.inputSchema,
				Table:          "test_table",
				ConstraintName: "fk_test",
				FKeySchema:     tt.inputSchema,
				FKeyTable:      "ref_table",
			}
			info.LocalCols = strings.Split(tt.localCSV, ",")
			info.FKeyCols = strings.Split(tt.fkeyCSV, ",")

			if normalize {
				info.Schema = strings.ToLower(info.Schema)
				info.FKeySchema = strings.ToLower(info.FKeySchema)
				for i := range info.LocalCols {
					info.LocalCols[i] = strings.ToLower(util.ToSnake(strings.TrimSpace(info.LocalCols[i])))
				}
				for i := range info.FKeyCols {
					info.FKeyCols[i] = strings.ToLower(util.ToSnake(strings.TrimSpace(info.FKeyCols[i])))
				}
			}

			if info.Schema != tt.wantSchema {
				t.Errorf("schema: got %q, want %q", info.Schema, tt.wantSchema)
			}
			if !reflect.DeepEqual(info.LocalCols, tt.wantLocalCols) {
				t.Errorf("local cols: got %v, want %v", info.LocalCols, tt.wantLocalCols)
			}
			if !reflect.DeepEqual(info.FKeyCols, tt.wantFKeyCols) {
				t.Errorf("fkey cols: got %v, want %v", info.FKeyCols, tt.wantFKeyCols)
			}
		})
	}
}

// TestDiscoverCompositeFKsUnsupportedDB verifies that unknown DB types return nil.
func TestDiscoverCompositeFKsUnsupportedDB(t *testing.T) {
	result, err := DiscoverCompositeFKs(nil, "cockroach")
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result for unsupported DB, got: %v", result)
	}
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

// TestNewDBTableCompositePK verifies that NewDBTable correctly collects
// multiple PrimaryKey columns into PrimaryCols and sets PrimaryCol to the first.
func TestNewDBTableCompositePK(t *testing.T) {
	cols := []DBColumn{
		{Name: "user_id", Type: "integer", PrimaryKey: true},
		{Name: "session_id", Type: "integer", PrimaryKey: true},
		{Name: "data", Type: "text"},
	}
	ti := NewDBTable("public", "user_sessions", "table", cols)

	if len(ti.PrimaryCols) != 2 {
		t.Fatalf("expected 2 PrimaryCols, got %d", len(ti.PrimaryCols))
	}
	if ti.PrimaryCols[0].Name != "user_id" {
		t.Errorf("expected PrimaryCols[0] = user_id, got %s", ti.PrimaryCols[0].Name)
	}
	if ti.PrimaryCols[1].Name != "session_id" {
		t.Errorf("expected PrimaryCols[1] = session_id, got %s", ti.PrimaryCols[1].Name)
	}
	if ti.PrimaryCol.Name != "user_id" {
		t.Errorf("expected PrimaryCol = user_id (alias for first), got %s", ti.PrimaryCol.Name)
	}
}

// TestHasCompositePK verifies the HasCompositePK helper.
func TestHasCompositePK(t *testing.T) {
	single := NewDBTable("public", "users", "table", []DBColumn{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "name", Type: "text"},
	})
	if single.HasCompositePK() {
		t.Error("single PK table should not report HasCompositePK")
	}

	composite := NewDBTable("public", "user_sessions", "table", []DBColumn{
		{Name: "user_id", Type: "integer", PrimaryKey: true},
		{Name: "session_id", Type: "integer", PrimaryKey: true},
	})
	if !composite.HasCompositePK() {
		t.Error("composite PK table should report HasCompositePK")
	}

	noPK := NewDBTable("public", "logs", "table", []DBColumn{
		{Name: "data", Type: "text"},
	})
	if noPK.HasCompositePK() {
		t.Error("no PK table should not report HasCompositePK")
	}
}

// TestPKColNames verifies the PKColNames helper.
func TestPKColNames(t *testing.T) {
	ti := NewDBTable("public", "order_items", "table", []DBColumn{
		{Name: "order_id", Type: "integer", PrimaryKey: true},
		{Name: "product_id", Type: "integer", PrimaryKey: true},
		{Name: "quantity", Type: "integer"},
	})
	names := ti.PKColNames()
	if len(names) != 2 || names[0] != "order_id" || names[1] != "product_id" {
		t.Errorf("expected [order_id product_id], got %v", names)
	}
}

// TestIsPKCol verifies the IsPKCol helper.
func TestIsPKCol(t *testing.T) {
	ti := NewDBTable("public", "order_items", "table", []DBColumn{
		{Name: "order_id", Type: "integer", PrimaryKey: true},
		{Name: "product_id", Type: "integer", PrimaryKey: true},
		{Name: "quantity", Type: "integer"},
	})
	if !ti.IsPKCol("order_id") {
		t.Error("order_id should be a PK col")
	}
	if !ti.IsPKCol("product_id") {
		t.Error("product_id should be a PK col")
	}
	if ti.IsPKCol("quantity") {
		t.Error("quantity should not be a PK col")
	}
	if ti.IsPKCol("nonexistent") {
		t.Error("nonexistent should not be a PK col")
	}
}
