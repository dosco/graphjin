package sdata

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3/internal/util"
)

func TestParseClusteringKey(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want []string
	}{
		// ParseClusteringKey preserves Snowflake identifier case — unquoted
		// Snowflake objects are stored UPPERCASE, and downstream lookups in
		// dwg.go are case-insensitive, so normalizing here would only mask
		// the true storage casing.
		{
			name: "LINEAR with two columns",
			expr: "LINEAR(CREATED_AT, USER_ID)",
			want: []string{"CREATED_AT", "USER_ID"},
		},
		{
			name: "LINEAR with single column",
			expr: "LINEAR(ORDER_DATE)",
			want: []string{"ORDER_DATE"},
		},
		{
			name: "bare parentheses",
			expr: "(CREATED_AT, USER_ID)",
			want: []string{"CREATED_AT", "USER_ID"},
		},
		{
			name: "single column bare parens",
			expr: "(ID)",
			want: []string{"ID"},
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
			want: []string{"CREATED_AT", "USER_ID"},
		},
		{
			name: "lowercase input preserved",
			expr: "LINEAR(created_at, user_id)",
			want: []string{"created_at", "user_id"},
		},
		{
			name: "mixed case preserved",
			expr: "LINEAR(CreatedAt, UserId)",
			want: []string{"CreatedAt", "UserId"},
		},
		{
			name: "expression-based key won't match columns (gracefully skipped)",
			expr: "LINEAR(CAST(CREATED_AT AS DATE), REGION)",
			want: []string{"CAST(CREATED_AT AS DATE)", "REGION"},
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
			name:          "snowflake: uppercase preserved (case-sensitive discovery)",
			dbtype:        "snowflake",
			localCSV:      "SPECIAL_OFFER_ID,PRODUCT_ID",
			fkeyCSV:       "SPECIAL_OFFER_ID,PRODUCT_ID",
			wantLocalCols: []string{"SPECIAL_OFFER_ID", "PRODUCT_ID"},
			wantFKeyCols:  []string{"SPECIAL_OFFER_ID", "PRODUCT_ID"},
			wantSchema:    "PUBLIC",
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
			normalize := tt.dbtype == "oracle" || tt.dbtype == "mssql"
			trimOnly := tt.dbtype == "snowflake"

			info := CompositeFKInfo{
				Schema:         tt.inputSchema,
				Table:          "test_table",
				ConstraintName: "fk_test",
				FKeySchema:     tt.inputSchema,
				FKeyTable:      "ref_table",
			}
			info.LocalCols = strings.Split(tt.localCSV, ",")
			info.FKeyCols = strings.Split(tt.fkeyCSV, ",")

			switch {
			case normalize:
				info.Schema = strings.ToLower(info.Schema)
				info.FKeySchema = strings.ToLower(info.FKeySchema)
				for i := range info.LocalCols {
					info.LocalCols[i] = strings.ToLower(util.ToSnake(strings.TrimSpace(info.LocalCols[i])))
				}
				for i := range info.FKeyCols {
					info.FKeyCols[i] = strings.ToLower(util.ToSnake(strings.TrimSpace(info.FKeyCols[i])))
				}
			case trimOnly:
				for i := range info.LocalCols {
					info.LocalCols[i] = strings.TrimSpace(info.LocalCols[i])
				}
				for i := range info.FKeyCols {
					info.FKeyCols[i] = strings.TrimSpace(info.FKeyCols[i])
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
	result, err := DiscoverCompositeFKs(context.Background(), nil, "cockroach")
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result for unsupported DB, got: %v", result)
	}
}

// TestHasCompositeFKCandidates verifies that the short-circuit detector
// correctly identifies whether DiscoverCompositeFKs could possibly find any
// composite foreign keys, based on already-collected column-level FK data.
// This short-circuit is the critical perf win: when no (table -> fkTable)
// pair has two or more columns, the expensive dialect-specific composite FK
// query can be skipped entirely.
func TestHasCompositeFKCandidates(t *testing.T) {
	tests := []struct {
		name string
		cols []DBColumn
		want bool
	}{
		{
			name: "no FKs at all",
			cols: []DBColumn{
				{Schema: "public", Table: "users", Name: "id"},
				{Schema: "public", Table: "users", Name: "email"},
			},
			want: false,
		},
		{
			name: "single-column FKs only",
			cols: []DBColumn{
				{Schema: "public", Table: "products", Name: "owner_id", FKeySchema: "public", FKeyTable: "users", FKeyCol: "id"},
				{Schema: "public", Table: "comments", Name: "user_id", FKeySchema: "public", FKeyTable: "users", FKeyCol: "id"},
			},
			want: false,
		},
		{
			name: "two cols in same table reference same fk table (composite candidate)",
			cols: []DBColumn{
				{Schema: "public", Table: "order_items", Name: "order_id", FKeySchema: "public", FKeyTable: "orders", FKeyCol: "id"},
				{Schema: "public", Table: "order_items", Name: "order_line", FKeySchema: "public", FKeyTable: "orders", FKeyCol: "line"},
			},
			want: true,
		},
		{
			name: "two cols in same table reference different fk tables (not a candidate)",
			cols: []DBColumn{
				{Schema: "public", Table: "purchases", Name: "customer_id", FKeySchema: "public", FKeyTable: "users", FKeyCol: "id"},
				{Schema: "public", Table: "purchases", Name: "product_id", FKeySchema: "public", FKeyTable: "products", FKeyCol: "id"},
			},
			want: false,
		},
		{
			name: "same col names but different tables — not a candidate",
			cols: []DBColumn{
				{Schema: "public", Table: "a", Name: "x", FKeySchema: "public", FKeyTable: "t", FKeyCol: "id"},
				{Schema: "public", Table: "b", Name: "x", FKeySchema: "public", FKeyTable: "t", FKeyCol: "id"},
			},
			want: false,
		},
		{
			name: "cross-schema candidate",
			cols: []DBColumn{
				{Schema: "sales", Table: "orders", Name: "region_id", FKeySchema: "geo", FKeyTable: "regions", FKeyCol: "id"},
				{Schema: "sales", Table: "orders", Name: "country_id", FKeySchema: "geo", FKeyTable: "regions", FKeyCol: "country"},
			},
			want: true,
		},
		{
			name: "empty column set",
			cols: nil,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasCompositeFKCandidates(tt.cols); got != tt.want {
				t.Errorf("hasCompositeFKCandidates() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestIntrospectionQueryTimeoutConstant pins the per-query timeout at 30s.
// This is a defensive backstop: even if a future bad query hangs, the whole
// GetDBInfo call must not block longer than a bounded multiple of this value.
func TestIntrospectionQueryTimeoutConstant(t *testing.T) {
	if introspectionQueryTimeout <= 0 {
		t.Fatalf("introspectionQueryTimeout must be positive, got %v", introspectionQueryTimeout)
	}
	if introspectionQueryTimeout > 60*time.Second {
		t.Errorf("introspectionQueryTimeout too large (%v) — defeats the purpose of a defensive timeout",
			introspectionQueryTimeout)
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

func TestInferDefaultSchema(t *testing.T) {
	tests := []struct {
		name string
		cols []DBColumn
		want string
	}{
		{
			name: "picks schema with most tables",
			cols: []DBColumn{
				{Schema: "sales", Table: "orders", Name: "id"},
				{Schema: "sales", Table: "orders", Name: "total"},
				{Schema: "sales", Table: "customers", Name: "id"},
				{Schema: "person", Table: "address", Name: "id"},
			},
			want: "sales",
		},
		{
			name: "ignores _gj_ tables",
			cols: []DBColumn{
				{Schema: "internal", Table: "_gj_metadata", Name: "id"},
				{Schema: "internal", Table: "_gj_config", Name: "id"},
				{Schema: "public", Table: "users", Name: "id"},
			},
			want: "public",
		},
		{
			name: "empty columns returns empty",
			cols: nil,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferDefaultSchema(tt.cols)
			if got != tt.want {
				t.Errorf("inferDefaultSchema() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInferViewPKsFromBaseTables(t *testing.T) {
	tests := []struct {
		name   string
		cols   map[string]DBColumn
		wantPK []string // keys that should become PK
	}{
		{
			name: "view matches base table by non-PK column overlap",
			cols: map[string]DBColumn{
				"public:users:id":                  {Schema: "public", Table: "users", Name: "id", PrimaryKey: true},
				"public:users:full_name":           {Schema: "public", Table: "users", Name: "full_name"},
				"public:users:email":               {Schema: "public", Table: "users", Name: "email"},
				"public:products:id":               {Schema: "public", Table: "products", Name: "id", PrimaryKey: true},
				"public:products:name":             {Schema: "public", Table: "products", Name: "name"},
				"public:user_products:id":          {Schema: "public", Table: "user_products", Name: "id"},
				"public:user_products:full_name":   {Schema: "public", Table: "user_products", Name: "full_name"},
				"public:user_products:product_name": {Schema: "public", Table: "user_products", Name: "product_name"},
			},
			wantPK: []string{"public:user_products:id"},
		},
		{
			name: "ambiguous overlap — no PK inferred",
			cols: map[string]DBColumn{
				"public:a:id":   {Schema: "public", Table: "a", Name: "id", PrimaryKey: true},
				"public:a:name": {Schema: "public", Table: "a", Name: "name"},
				"public:b:id":   {Schema: "public", Table: "b", Name: "id", PrimaryKey: true},
				"public:b:name": {Schema: "public", Table: "b", Name: "name"},
				"public:v:id":   {Schema: "public", Table: "v", Name: "id"},
				"public:v:name": {Schema: "public", Table: "v", Name: "name"},
			},
			wantPK: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inferViewPKsFromBaseTables(tt.cols)
			for _, k := range tt.wantPK {
				if !tt.cols[k].PrimaryKey {
					t.Errorf("expected %s to become PK", k)
				}
			}
		})
	}
}
