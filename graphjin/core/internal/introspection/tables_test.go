package introspection

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
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
		name           string
		clusteringKeys []string
		columns        []DBColumn
		wantPartition  string
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
			table := sdata.NewDBTable("public", "test_table", "", tt.columns)
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
	di := sdata.GetTestSnowflakeDBInfo()
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
		name          string
		dbtype        string
		localCSV      string
		fkeyCSV       string
		wantLocalCols []string
		wantFKeyCols  []string
		wantSchema    string
		inputSchema   string
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
	ti := sdata.NewDBTable("public", "user_sessions", "table", cols)

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
	single := sdata.NewDBTable("public", "users", "table", []DBColumn{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "name", Type: "text"},
	})
	if single.HasCompositePK() {
		t.Error("single PK table should not report HasCompositePK")
	}

	composite := sdata.NewDBTable("public", "user_sessions", "table", []DBColumn{
		{Name: "user_id", Type: "integer", PrimaryKey: true},
		{Name: "session_id", Type: "integer", PrimaryKey: true},
	})
	if !composite.HasCompositePK() {
		t.Error("composite PK table should report HasCompositePK")
	}

	noPK := sdata.NewDBTable("public", "logs", "table", []DBColumn{
		{Name: "data", Type: "text"},
	})
	if noPK.HasCompositePK() {
		t.Error("no PK table should not report HasCompositePK")
	}
}

// TestPKColNames verifies the PKColNames helper.
func TestPKColNames(t *testing.T) {
	ti := sdata.NewDBTable("public", "order_items", "table", []DBColumn{
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
	ti := sdata.NewDBTable("public", "order_items", "table", []DBColumn{
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
				"public:users:id":                   {Schema: "public", Table: "users", Name: "id", PrimaryKey: true},
				"public:users:full_name":            {Schema: "public", Table: "users", Name: "full_name"},
				"public:users:email":                {Schema: "public", Table: "users", Name: "email"},
				"public:products:id":                {Schema: "public", Table: "products", Name: "id", PrimaryKey: true},
				"public:products:name":              {Schema: "public", Table: "products", Name: "name"},
				"public:user_products:id":           {Schema: "public", Table: "user_products", Name: "id"},
				"public:user_products:full_name":    {Schema: "public", Table: "user_products", Name: "full_name"},
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

func TestPostgresDiscoverColumnsSkipsConstraintRowsWhenPreflightIsEmpty(t *testing.T) {
	state := &postgresDiscoveryFakeState{
		basicRows: [][]driver.Value{
			{"public", "users", "id", "integer", true, false, false, false, false, "", "", ""},
			{"public", "users", "email", "text", false, false, false, false, false, "", "", ""},
		},
	}
	db := openPostgresDiscoveryFakeDB(t, state)
	defer db.Close()

	cols, err := DiscoverColumns(context.Background(), db, "postgres", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 2 {
		t.Fatalf("len(cols) = %d, want 2", len(cols))
	}
	if state.count("basic") != 1 {
		t.Fatalf("basic discovery queries = %d, want 1", state.count("basic"))
	}
	if state.count("constraint_preflight") != 1 {
		t.Fatalf("constraint preflight queries = %d, want 1", state.count("constraint_preflight"))
	}
	if state.count("constraints") != 0 {
		t.Fatalf("constraint row queries = %d, want 0", state.count("constraints"))
	}
}

func TestPostgresDiscoverColumnsMergesBatchedConstraintRows(t *testing.T) {
	state := &postgresDiscoveryFakeState{
		hasConstraints: true,
		basicRows: [][]driver.Value{
			{"public", "products", "id", "integer", true, false, false, false, false, "", "", ""},
			{"public", "order_items", "id", "integer", true, false, false, false, false, "", "", ""},
			{"public", "order_items", "product_id", "integer", true, false, false, false, false, "", "", ""},
		},
		constraintRows: [][]driver.Value{
			{"public", "products", "id", "", false, true, false, false, false, "", "", ""},
			{"public", "order_items", "id", "", false, true, false, false, false, "", "", ""},
			{"public", "order_items", "product_id", "", false, false, false, false, false, "public", "products", "id"},
		},
	}
	db := openPostgresDiscoveryFakeDB(t, state)
	defer db.Close()

	cols, err := DiscoverColumns(context.Background(), db, "postgres", nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.count("constraints") != 1 {
		t.Fatalf("constraint row queries = %d, want 1", state.count("constraints"))
	}

	byColumn := map[string]DBColumn{}
	for _, c := range cols {
		byColumn[c.Schema+":"+c.Table+":"+c.Name] = c
	}
	if got := byColumn["public:products:id"]; !got.PrimaryKey || !got.UniqueKey {
		t.Fatalf("products.id flags = primary:%v unique:%v, want both true", got.PrimaryKey, got.UniqueKey)
	}
	if got := byColumn["public:order_items:product_id"]; got.FKeySchema != "public" || got.FKeyTable != "products" || got.FKeyCol != "id" {
		t.Fatalf("order_items.product_id FK = %s.%s.%s, want public.products.id", got.FKeySchema, got.FKeyTable, got.FKeyCol)
	}
}

func TestSnowflakeDiscoverColumnsUsesBulkMetadataOnly(t *testing.T) {
	state := &snowflakeDiscoveryFakeState{
		columnRows: [][]driver.Value{
			snowflakeColumnRow("PUBLIC", "ACCOUNTS", "ID"),
		},
	}
	db := openSnowflakeDiscoveryFakeDB(t, state)
	defer db.Close()

	cols, err := DiscoverColumns(context.Background(), db, "snowflake", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 1 {
		t.Fatalf("len(cols) = %d, want 1", len(cols))
	}
	if got := cols[0].Schema + ":" + cols[0].Table + ":" + cols[0].Name; got != "public:accounts:id" {
		t.Fatalf("column = %q, want public:accounts:id", got)
	}
	if state.count("columns") != 1 {
		t.Fatalf("bulk column queries = %d, want 1", state.count("columns"))
	}
	if state.count("show_exec") != 0 || state.count("show_scan") != 0 {
		t.Fatalf("SHOW discovery was used: exec=%d scan=%d, want 0", state.count("show_exec"), state.count("show_scan"))
	}
}

func TestSnowflakeDiscoverColumnsMergesBulkConstraintRows(t *testing.T) {
	state := &snowflakeDiscoveryFakeState{
		columnRows: [][]driver.Value{
			snowflakeColumnRow("PUBLIC", "PRODUCTS", "ID"),
			snowflakeColumnRow("PUBLIC", "ORDER_ITEMS", "PRODUCT_ID"),
			{"PUBLIC", "PRODUCTS", "ID", "", false, true, false, false, false, "", "", ""},
			{"PUBLIC", "ORDER_ITEMS", "PRODUCT_ID", "", false, false, false, false, false, "PUBLIC", "PRODUCTS", "ID"},
		},
	}
	db := openSnowflakeDiscoveryFakeDB(t, state)
	defer db.Close()

	cols, err := DiscoverColumns(context.Background(), db, "snowflake", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 2 {
		t.Fatalf("len(cols) = %d, want 2", len(cols))
	}
	if state.count("columns") != 1 {
		t.Fatalf("bulk column queries = %d, want 1", state.count("columns"))
	}
	if state.count("constraints") != 0 || state.count("foreign_keys") != 0 {
		t.Fatalf("separate constraint queries used: constraints=%d foreign_keys=%d, want 0", state.count("constraints"), state.count("foreign_keys"))
	}

	byColumn := map[string]DBColumn{}
	for _, c := range cols {
		byColumn[c.Schema+":"+c.Table+":"+c.Name] = c
	}
	if got := byColumn["public:products:id"]; !got.PrimaryKey || !got.UniqueKey {
		t.Fatalf("products.id flags = primary:%v unique:%v, want both true", got.PrimaryKey, got.UniqueKey)
	}
	if got := byColumn["public:order_items:product_id"]; got.FKeySchema != "public" || got.FKeyTable != "products" || got.FKeyCol != "id" {
		t.Fatalf("order_items.product_id FK = %s.%s.%s, want public.products.id", got.FKeySchema, got.FKeyTable, got.FKeyCol)
	}
}

func TestSnowflakeDiscoverColumnsAppliesFKMetadataOverlay(t *testing.T) {
	state := &snowflakeDiscoveryFakeState{
		fkMetadataExists: true,
		columnRows: [][]driver.Value{
			snowflakeColumnRow("PUBLIC", "ORDER_ITEMS", "PRODUCT_ID"),
		},
		fkMetadataRows: [][]driver.Value{
			{"PUBLIC", "ORDER_ITEMS", "PRODUCT_ID", "", false, false, false, false, false, "PUBLIC", "PRODUCTS", "ID"},
		},
	}
	db := openSnowflakeDiscoveryFakeDB(t, state)
	defer db.Close()

	cols, err := DiscoverColumns(context.Background(), db, "snowflake", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 1 {
		t.Fatalf("len(cols) = %d, want 1", len(cols))
	}
	got := cols[0]
	if got.FKeySchema != "public" || got.FKeyTable != "products" || got.FKeyCol != "id" {
		t.Fatalf("overlay FK = %s.%s.%s, want public.products.id", got.FKeySchema, got.FKeyTable, got.FKeyCol)
	}
	if state.count("fk_metadata_exists") != 1 {
		t.Fatalf("_gj_fk_metadata exists checks = %d, want 1", state.count("fk_metadata_exists"))
	}
	if state.count("fk_metadata") != 1 {
		t.Fatalf("_gj_fk_metadata row queries = %d, want 1", state.count("fk_metadata"))
	}
}

func TestSnowflakeDiscoverColumnsRunsLargeMultiSchemaCatalogWithOneBulkQuery(t *testing.T) {
	const (
		schemaCount     = 40
		tablesPerSchema = 125
		totalTables     = schemaCount * tablesPerSchema
	)
	state := &snowflakeDiscoveryFakeState{
		delay: time.Millisecond,
	}
	for i := 0; i < schemaCount; i++ {
		schema := fmt.Sprintf("SCHEMA_%02d", i)
		for j := 0; j < tablesPerSchema; j++ {
			state.columnRows = append(state.columnRows, snowflakeColumnRow(schema, fmt.Sprintf("TABLE_%02d_%03d", i, j), "ID"))
		}
	}
	db := openSnowflakeDiscoveryFakeDB(t, state)
	defer db.Close()

	cols, err := DiscoverColumns(context.Background(), db, "snowflake", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != totalTables {
		t.Fatalf("len(cols) = %d, want %d", len(cols), totalTables)
	}
	seenIDs := map[int32]bool{}
	seenSchemas := map[string]bool{}
	for _, col := range cols {
		if seenIDs[col.ID] {
			t.Fatalf("duplicate column ID %d in %+v", col.ID, cols)
		}
		seenIDs[col.ID] = true
		seenSchemas[col.Schema] = true
	}
	if len(seenSchemas) != schemaCount {
		t.Fatalf("schemas discovered = %d, want %d", len(seenSchemas), schemaCount)
	}
	if state.count("columns") != 1 {
		t.Fatalf("bulk column queries = %d, want 1", state.count("columns"))
	}
	if state.count("schemas") != 0 {
		t.Fatalf("schema enumeration queries = %d, want 0", state.count("schemas"))
	}
	if state.count("constraint_schemas") != 0 {
		t.Fatalf("constraint schema preflight queries = %d, want 0", state.count("constraint_schemas"))
	}
	if state.count("show_exec") != 0 || state.count("show_scan") != 0 {
		t.Fatalf("SHOW discovery was used: exec=%d scan=%d, want 0", state.count("show_exec"), state.count("show_scan"))
	}
	if state.count("constraints") != 0 || state.count("foreign_keys") != 0 || state.count("basic") != 0 {
		t.Fatalf("extra metadata queries used: constraints=%d foreign_keys=%d basic=%d, want 0",
			state.count("constraints"), state.count("foreign_keys"), state.count("basic"))
	}
	if state.count("fk_metadata_exists") != 1 {
		t.Fatalf("_gj_fk_metadata exists checks = %d, want 1", state.count("fk_metadata_exists"))
	}
}

func TestSnowflakeDiscoverCompositeFKsUsesInformationSchemaAndOverrides(t *testing.T) {
	state := &snowflakeDiscoveryFakeState{
		fkMetadataExists: true,
		compositeRows: [][]driver.Value{
			{"PUBLIC", "ORDER_LINES", "ORDER_LINES_ORDER_FK", "ORDER_ID,PRODUCT_ID", "PUBLIC", "ORDERS", "ID,PRODUCT_ID"},
		},
		compositeOverrideRows: [][]driver.Value{
			{"PUBLIC", "LINE_ITEMS", "PUBLIC:LINE_ITEMS:PRODUCTS", "ORDER_ID,PRODUCT_ID", "PUBLIC", "PRODUCTS", "ORDER_ID,ID"},
		},
	}
	db := openSnowflakeDiscoveryFakeDB(t, state)
	defer db.Close()

	fks, err := DiscoverCompositeFKs(context.Background(), db, "snowflake")
	if err != nil {
		t.Fatal(err)
	}
	if len(fks) != 2 {
		t.Fatalf("len(fks) = %d, want 2", len(fks))
	}
	if state.count("composite_fks") != 1 {
		t.Fatalf("composite FK information_schema queries = %d, want 1", state.count("composite_fks"))
	}
	if state.count("composite_fk_overrides") != 1 {
		t.Fatalf("composite FK override queries = %d, want 1", state.count("composite_fk_overrides"))
	}
	if state.count("show_exec") != 0 || state.count("show_scan") != 0 {
		t.Fatalf("SHOW discovery was used: exec=%d scan=%d, want 0", state.count("show_exec"), state.count("show_scan"))
	}

	byTable := map[string]CompositeFKInfo{}
	for _, fk := range fks {
		byTable[fk.Table] = fk
	}
	if got := byTable["order_lines"]; !reflect.DeepEqual(got.LocalCols, []string{"order_id", "product_id"}) ||
		!reflect.DeepEqual(got.FKeyCols, []string{"id", "product_id"}) {
		t.Fatalf("order_lines composite FK = local:%v fkey:%v", got.LocalCols, got.FKeyCols)
	}
	if got := byTable["line_items"]; got.FKeyTable != "products" ||
		!reflect.DeepEqual(got.LocalCols, []string{"order_id", "product_id"}) ||
		!reflect.DeepEqual(got.FKeyCols, []string{"order_id", "id"}) {
		t.Fatalf("line_items override FK = table:%s local:%v fkey:%v", got.FKeyTable, got.LocalCols, got.FKeyCols)
	}
}

func TestDiscoverColumnsScaleUsesBatchedMetadata(t *testing.T) {
	const tableCount = 5000
	tests := []struct {
		dbtype     string
		schema     string
		maxQueries int
	}{
		{dbtype: "postgres", schema: "public", maxQueries: 3},
		{dbtype: "mysql", schema: "app", maxQueries: 3},
		{dbtype: "mariadb", schema: "app", maxQueries: 3},
		{dbtype: "mssql", schema: "dbo", maxQueries: 2},
		{dbtype: "oracle", schema: "APP", maxQueries: 2},
		{dbtype: "sqlite", schema: "main", maxQueries: 2},
	}

	for _, tt := range tests {
		t.Run(tt.dbtype, func(t *testing.T) {
			state := &scaleDiscoveryFakeState{
				dbtype: tt.dbtype,
				rows:   scaleColumnRows(tt.schema, tableCount),
			}
			db := openScaleDiscoveryFakeDB(t, state)
			defer db.Close()

			var (
				eventMu sync.Mutex
				events  []discoveryQueryEvent
			)
			ctx := withDiscoveryQueryRecorder(context.Background(), func(ev discoveryQueryEvent) {
				eventMu.Lock()
				defer eventMu.Unlock()
				events = append(events, ev)
			})

			cols, err := DiscoverColumns(ctx, db, tt.dbtype, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(cols) != tableCount {
				t.Fatalf("len(cols) = %d, want %d", len(cols), tableCount)
			}
			eventMu.Lock()
			gotEvents := append([]discoveryQueryEvent(nil), events...)
			eventMu.Unlock()
			if len(gotEvents) > tt.maxQueries {
				t.Fatalf("discovery query count = %d, want <= %d", len(gotEvents), tt.maxQueries)
			}
			for _, ev := range gotEvents {
				if ev.TableSpecific {
					t.Fatalf("table-specific discovery query detected for %s: %s", tt.dbtype, ev.SQL)
				}
			}
			if state.tableSpecificQueries() != 0 {
				t.Fatalf("fake driver saw %d table-specific metadata queries, want 0", state.tableSpecificQueries())
			}
		})
	}
}

var postgresDiscoveryFakeSeq atomic.Uint64

func openPostgresDiscoveryFakeDB(t *testing.T, state *postgresDiscoveryFakeState) *sql.DB {
	t.Helper()

	name := fmt.Sprintf("postgres_discovery_fake_%d", postgresDiscoveryFakeSeq.Add(1))
	sql.Register(name, postgresDiscoveryFakeDriver{state: state})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	return db
}

type postgresDiscoveryFakeState struct {
	mu             sync.Mutex
	counts         map[string]int
	hasConstraints bool
	basicRows      [][]driver.Value
	constraintRows [][]driver.Value
}

func (s *postgresDiscoveryFakeState) count(kind string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[kind]
}

func (s *postgresDiscoveryFakeState) record(kind string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.counts == nil {
		s.counts = map[string]int{}
	}
	s.counts[kind]++
}

type postgresDiscoveryFakeDriver struct {
	state *postgresDiscoveryFakeState
}

func (d postgresDiscoveryFakeDriver) Open(_ string) (driver.Conn, error) {
	return postgresDiscoveryFakeConn{state: d.state}, nil
}

type postgresDiscoveryFakeConn struct {
	state *postgresDiscoveryFakeState
}

func (c postgresDiscoveryFakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("Prepare is not implemented")
}

func (c postgresDiscoveryFakeConn) Close() error {
	return nil
}

func (c postgresDiscoveryFakeConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("Begin is not implemented")
}

func (c postgresDiscoveryFakeConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	switch {
	case query == postgresColumnsBasicStmt:
		c.state.record("basic")
		return newPostgresDiscoveryFakeRows(discoveredColumnScanColumns(), c.state.basicRows), nil
	case query == postgresConstraintsCountStmt:
		c.state.record("constraint_preflight")
		var n int64
		if c.state.hasConstraints {
			n = 1
		}
		return newPostgresDiscoveryFakeRows([]string{"has_constraints"}, [][]driver.Value{{n}}), nil
	case query == postgresConstraintColumnsStmt:
		c.state.record("constraints")
		return newPostgresDiscoveryFakeRows(discoveredColumnScanColumns(), c.state.constraintRows), nil
	case strings.Contains(query, "relkind IN ('v','m')"):
		c.state.record("view_preflight")
		return newPostgresDiscoveryFakeRows([]string{"exists"}, nil), nil
	default:
		return nil, fmt.Errorf("unexpected query: %.120s", query)
	}
}

func discoveredColumnScanColumns() []string {
	return []string{
		"schema",
		"table",
		"column",
		"type",
		"not_null",
		"primary_key",
		"unique_key",
		"is_array",
		"full_text",
		"foreignkey_schema",
		"foreignkey_table",
		"foreignkey_column",
	}
}

type postgresDiscoveryFakeRows struct {
	cols []string
	rows [][]driver.Value
	pos  int
}

func newPostgresDiscoveryFakeRows(cols []string, rows [][]driver.Value) *postgresDiscoveryFakeRows {
	return &postgresDiscoveryFakeRows{cols: cols, rows: rows}
}

func (r *postgresDiscoveryFakeRows) Columns() []string {
	return r.cols
}

func (r *postgresDiscoveryFakeRows) Close() error {
	return nil
}

func (r *postgresDiscoveryFakeRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.pos])
	r.pos++
	return nil
}

var scaleDiscoveryFakeSeq atomic.Uint64

func openScaleDiscoveryFakeDB(t *testing.T, state *scaleDiscoveryFakeState) *sql.DB {
	t.Helper()

	name := fmt.Sprintf("scale_discovery_fake_%d", scaleDiscoveryFakeSeq.Add(1))
	sql.Register(name, scaleDiscoveryFakeDriver{state: state})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(4)
	return db
}

type scaleDiscoveryFakeState struct {
	mu            sync.Mutex
	dbtype        string
	rows          [][]driver.Value
	tableSpecific int
}

func (s *scaleDiscoveryFakeState) tableSpecificQueries() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tableSpecific
}

func (s *scaleDiscoveryFakeState) recordQuery(query string) {
	if !strings.Contains(strings.ToLower(query), "scale_table_") {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tableSpecific++
}

type scaleDiscoveryFakeDriver struct {
	state *scaleDiscoveryFakeState
}

func (d scaleDiscoveryFakeDriver) Open(_ string) (driver.Conn, error) {
	return scaleDiscoveryFakeConn{state: d.state}, nil
}

type scaleDiscoveryFakeConn struct {
	state *scaleDiscoveryFakeState
}

func (c scaleDiscoveryFakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("Prepare is not implemented")
}

func (c scaleDiscoveryFakeConn) Close() error {
	return nil
}

func (c scaleDiscoveryFakeConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("Begin is not implemented")
}

func (c scaleDiscoveryFakeConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.state.recordQuery(query)
	switch {
	case query == postgresColumnsBasicStmt ||
		query == mysqlColumnsBasicStmt ||
		query == mariadbColumnsBasicStmt ||
		query == mssqlColumnsStmt ||
		query == oracleColumnsStmt ||
		query == sqliteColumnsStmt:
		return newPostgresDiscoveryFakeRows(discoveredColumnScanColumns(), c.state.rows), nil
	case query == postgresConstraintsCountStmt ||
		query == mysqlConstraintsCountStmt ||
		query == mariadbConstraintsCountStmt:
		return newPostgresDiscoveryFakeRows([]string{"count"}, [][]driver.Value{{int64(0)}}), nil
	case query == postgresConstraintColumnsStmt ||
		query == mysqlConstraintColumnsStmt ||
		query == mariadbConstraintColumnsStmt:
		return newPostgresDiscoveryFakeRows(discoveredColumnScanColumns(), nil), nil
	case isScaleViewPreflightQuery(query):
		return newPostgresDiscoveryFakeRows([]string{"exists"}, nil), nil
	default:
		return nil, fmt.Errorf("unexpected %s scale query: %.160s", c.state.dbtype, query)
	}
}

func isScaleViewPreflightQuery(query string) bool {
	upper := strings.ToUpper(query)
	return strings.Contains(upper, "PG_CLASS C") ||
		strings.Contains(upper, "INFORMATION_SCHEMA.VIEWS") ||
		strings.Contains(upper, "SYS.VIEWS") ||
		strings.Contains(upper, "ALL_VIEWS") ||
		strings.Contains(upper, "SQLITE_MASTER")
}

func scaleColumnRows(schema string, n int) [][]driver.Value {
	rows := make([][]driver.Value, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, []driver.Value{
			schema,
			fmt.Sprintf("scale_table_%04d", i),
			"id",
			"integer",
			true,
			false,
			false,
			false,
			false,
			"",
			"",
			"",
		})
	}
	return rows
}

var snowflakeDiscoveryFakeSeq atomic.Uint64

func openSnowflakeDiscoveryFakeDB(t *testing.T, state *snowflakeDiscoveryFakeState) *sql.DB {
	t.Helper()

	name := fmt.Sprintf("snowflake_discovery_fake_%d", snowflakeDiscoveryFakeSeq.Add(1))
	sql.Register(name, snowflakeDiscoveryFakeDriver{state: state})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(8)
	return db
}

type snowflakeDiscoveryFakeState struct {
	mu                    sync.Mutex
	columnRows            [][]driver.Value
	fkMetadataExists      bool
	fkMetadataRows        [][]driver.Value
	compositeRows         [][]driver.Value
	compositeOverrideRows [][]driver.Value
	counts                map[string]int
	delay                 time.Duration
}

func (s *snowflakeDiscoveryFakeState) count(kind string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[kind]
}

func (s *snowflakeDiscoveryFakeState) record(kind string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.counts == nil {
		s.counts = map[string]int{}
	}
	s.counts[kind]++
}

type snowflakeDiscoveryFakeDriver struct {
	state *snowflakeDiscoveryFakeState
}

func (d snowflakeDiscoveryFakeDriver) Open(_ string) (driver.Conn, error) {
	return snowflakeDiscoveryFakeConn{state: d.state}, nil
}

type snowflakeDiscoveryFakeConn struct {
	state *snowflakeDiscoveryFakeState
}

func (c snowflakeDiscoveryFakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("Prepare is not implemented")
}

func (c snowflakeDiscoveryFakeConn) Close() error {
	return nil
}

func (c snowflakeDiscoveryFakeConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("Begin is not implemented")
}

func (c snowflakeDiscoveryFakeConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	upper := strings.ToUpper(strings.TrimSpace(query))
	if strings.HasPrefix(upper, "SHOW ") {
		c.state.record("show_exec")
		return nil, fmt.Errorf("unexpected Snowflake SHOW discovery exec: %.160s", query)
	}
	return nil, fmt.Errorf("unexpected snowflake discovery exec: %.160s", query)
}

func (c snowflakeDiscoveryFakeConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	switch {
	case query == snowflakeFKMetadataExistsStmt:
		c.state.record("fk_metadata_exists")
		var n int64
		if c.state.fkMetadataExists {
			n = 1
		}
		return newPostgresDiscoveryFakeRows([]string{"count"}, [][]driver.Value{{n}}), nil
	case query == snowflakeColumnsStmt:
		c.state.record("columns")
		if c.state.delay != 0 {
			time.Sleep(c.state.delay)
		}
		return newPostgresDiscoveryFakeRows(discoveredColumnScanColumns(), c.state.columnRows), nil
	case query == snowflakeFKMetadataStmt:
		c.state.record("fk_metadata")
		return newPostgresDiscoveryFakeRows(discoveredColumnScanColumns(), c.state.fkMetadataRows), nil
	case query == compositeFKQuerySnowflake:
		c.state.record("composite_fks")
		return newPostgresDiscoveryFakeRows(compositeFKScanColumns(), c.state.compositeRows), nil
	case query == compositeFKQuerySnowflakeOverrides:
		c.state.record("composite_fk_overrides")
		return newPostgresDiscoveryFakeRows(compositeFKScanColumns(), c.state.compositeOverrideRows), nil
	case strings.Contains(strings.ToUpper(query), "SELECT LAST_QUERY_ID()"):
		c.state.record("last_query_id")
		c.state.record("show_scan")
		return nil, fmt.Errorf("unexpected Snowflake LAST_QUERY_ID discovery query")
	default:
		return nil, fmt.Errorf("unexpected snowflake discovery query: %.160s", query)
	}
}

func compositeFKScanColumns() []string {
	return []string{
		"table_schema",
		"table_name",
		"constraint_name",
		"local_columns",
		"foreignkey_schema",
		"foreignkey_table",
		"foreignkey_columns",
	}
}

func snowflakeColumnRow(schema, table, column string) []driver.Value {
	return []driver.Value{
		schema,
		table,
		column,
		"NUMBER",
		true,
		false,
		false,
		false,
		false,
		"",
		"",
		"",
	}
}
