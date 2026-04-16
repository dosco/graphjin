package sdata

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// These are pure unit tests — they use sqlmock to simulate Snowflake SHOW
// command responses and never connect to a real Snowflake instance. The
// table/column names mirror the standard test schema (users, products,
// purchases) used by the integration test harness in tests/snowflake.sql.

func mockDBForSnowflakeSHOW(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, mock
}

// TestSnowflakeSHOWEnrichmentMergesPKs feeds a mock SHOW PRIMARY KEYS result
// into enrichSnowflakeFromSHOW and asserts the matching column is marked PK.
func TestSnowflakeSHOWEnrichmentMergesPKs(t *testing.T) {
	db, mock := mockDBForSnowflakeSHOW(t)

	// SHOW PRIMARY KEYS → one row
	mock.ExpectQuery(`SHOW PRIMARY KEYS IN DATABASE`).
		WillReturnRows(sqlmock.NewRows(
			[]string{"created_on", "database_name", "schema_name", "table_name", "column_name", "key_sequence", "constraint_name"},
		).AddRow("t", "DB", "PUBLIC", "USERS", "ID", 1, "PK"))
	// SHOW UNIQUE KEYS → empty
	mock.ExpectQuery(`SHOW UNIQUE KEYS IN DATABASE`).
		WillReturnRows(sqlmock.NewRows([]string{"schema_name", "table_name", "column_name"}))
	// SHOW IMPORTED KEYS → empty
	mock.ExpectQuery(`SHOW IMPORTED KEYS IN DATABASE`).
		WillReturnRows(sqlmock.NewRows([]string{
			"fk_schema_name", "fk_table_name", "fk_column_name",
			"pk_schema_name", "pk_table_name", "pk_column_name",
		}))
	// _gj_fk_metadata overlay probe — return an error to simulate missing table
	mock.ExpectQuery(`FROM _gj_fk_metadata`).
		WillReturnError(sql.ErrNoRows)

	cmap := map[string]DBColumn{
		"PUBLIC:USERS:ID": {Schema: "PUBLIC", Table: "USERS", Name: "ID", Type: "bigint"},
	}

	enrichSnowflakeFromSHOW(context.Background(), db, cmap)

	got := cmap["PUBLIC:USERS:ID"]
	if !got.PrimaryKey {
		t.Error("expected ID to be marked PrimaryKey=true after SHOW PRIMARY KEYS")
	}
	if !got.UniqueKey {
		t.Error("expected ID to be UniqueKey=true (PK implies unique)")
	}
}

// TestSnowflakeSHOWEnrichmentTolerantOfMissingCommands asserts that when
// SHOW commands themselves error (emulator, restricted role), the cmap is
// unchanged — the enrichment is strictly additive and never fatal.
func TestSnowflakeSHOWEnrichmentTolerantOfMissingCommands(t *testing.T) {
	db, mock := mockDBForSnowflakeSHOW(t)

	// Every SHOW and the overlay probe all fail.
	mock.ExpectQuery(`SHOW PRIMARY KEYS IN DATABASE`).WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`SHOW UNIQUE KEYS IN DATABASE`).WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`SHOW IMPORTED KEYS IN DATABASE`).WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`FROM _gj_fk_metadata`).WillReturnError(sql.ErrNoRows)

	cmap := map[string]DBColumn{
		"PUBLIC:USERS:ID": {Schema: "PUBLIC", Table: "USERS", Name: "ID", Type: "bigint"},
	}
	before := cmap["PUBLIC:USERS:ID"]

	enrichSnowflakeFromSHOW(context.Background(), db, cmap)

	after := cmap["PUBLIC:USERS:ID"]
	if after.PrimaryKey != before.PrimaryKey || after.UniqueKey != before.UniqueKey || after.FKeyTable != before.FKeyTable {
		t.Errorf("enrichSnowflakeFromSHOW mutated cmap despite all probes failing; before=%+v after=%+v", before, after)
	}
}

// TestSnowflakeSHOWImportedKeysSetsFK feeds a mock SHOW IMPORTED KEYS result
// and asserts the FK fields are populated on the local column.
func TestSnowflakeSHOWImportedKeysSetsFK(t *testing.T) {
	db, mock := mockDBForSnowflakeSHOW(t)

	mock.ExpectQuery(`SHOW PRIMARY KEYS IN DATABASE`).
		WillReturnRows(sqlmock.NewRows([]string{"schema_name", "table_name", "column_name"}))
	mock.ExpectQuery(`SHOW UNIQUE KEYS IN DATABASE`).
		WillReturnRows(sqlmock.NewRows([]string{"schema_name", "table_name", "column_name"}))
	mock.ExpectQuery(`SHOW IMPORTED KEYS IN DATABASE`).
		WillReturnRows(sqlmock.NewRows([]string{
			"fk_schema_name", "fk_table_name", "fk_column_name",
			"pk_schema_name", "pk_table_name", "pk_column_name",
		}).AddRow("PUBLIC", "PURCHASES", "CUSTOMER_ID", "PUBLIC", "USERS", "ID"))
	mock.ExpectQuery(`FROM _gj_fk_metadata`).WillReturnError(sql.ErrNoRows)

	cmap := map[string]DBColumn{
		"PUBLIC:PURCHASES:CUSTOMER_ID": {Schema: "PUBLIC", Table: "PURCHASES", Name: "CUSTOMER_ID", Type: "bigint"},
	}
	enrichSnowflakeFromSHOW(context.Background(), db, cmap)

	got := cmap["PUBLIC:PURCHASES:CUSTOMER_ID"]
	if got.FKeySchema != "PUBLIC" || got.FKeyTable != "USERS" || got.FKeyCol != "ID" {
		t.Errorf("FK fields not populated; got {%q.%q.%q}, want {PUBLIC.USERS.ID}",
			got.FKeySchema, got.FKeyTable, got.FKeyCol)
	}
}

// TestSnowflakeFKMetadataOverlay verifies that _gj_fk_metadata rows are
// applied to columns that SHOW IMPORTED KEYS did not already claim.
func TestSnowflakeFKMetadataOverlay(t *testing.T) {
	db, mock := mockDBForSnowflakeSHOW(t)

	// SHOW commands all empty
	mock.ExpectQuery(`SHOW PRIMARY KEYS IN DATABASE`).
		WillReturnRows(sqlmock.NewRows([]string{"schema_name", "table_name", "column_name"}))
	mock.ExpectQuery(`SHOW UNIQUE KEYS IN DATABASE`).
		WillReturnRows(sqlmock.NewRows([]string{"schema_name", "table_name", "column_name"}))
	mock.ExpectQuery(`SHOW IMPORTED KEYS IN DATABASE`).
		WillReturnRows(sqlmock.NewRows([]string{"fk_schema_name", "fk_table_name", "fk_column_name", "pk_schema_name", "pk_table_name", "pk_column_name"}))
	// _gj_fk_metadata contains one overlay row matching the emulator seed
	// schema in tests/snowflake.sql (main.products.owner_id → main.users.id).
	mock.ExpectQuery(`FROM _gj_fk_metadata`).
		WillReturnRows(sqlmock.NewRows(
			[]string{"table_schema", "table_name", "column_name", "foreign_table_schema", "foreign_table_name", "foreign_column_name"},
		).AddRow("main", "products", "owner_id", "main", "users", "id"))

	cmap := map[string]DBColumn{
		"main:products:owner_id": {Schema: "main", Table: "products", Name: "owner_id", Type: "bigint"},
	}
	enrichSnowflakeFromSHOW(context.Background(), db, cmap)

	got := cmap["main:products:owner_id"]
	if got.FKeyTable != "users" || got.FKeyCol != "id" {
		t.Errorf("overlay FK not applied; got {%q.%q.%q}, want {main.users.id}",
			got.FKeySchema, got.FKeyTable, got.FKeyCol)
	}
}

// TestSnowflakeCompositeFKFromSHOW exercises discoverCompositeFKsSnowflake
// with a mocked SHOW IMPORTED KEYS that returns two rows sharing fk_name.
// The function should assemble them into a single CompositeFKInfo.
func TestSnowflakeCompositeFKFromSHOW(t *testing.T) {
	db, mock := mockDBForSnowflakeSHOW(t)

	// Simulate a two-column FK on a (PRODUCT_ID, CUSTOMER_ID) composite —
	// same pattern as multi-column FKs elsewhere in the GraphJin test
	// corpus (composite_fk_test.go).
	mock.ExpectQuery(`SHOW IMPORTED KEYS IN DATABASE`).
		WillReturnRows(sqlmock.NewRows([]string{
			"fk_schema_name", "fk_table_name", "fk_column_name",
			"pk_schema_name", "pk_table_name", "pk_column_name",
			"fk_name", "key_sequence",
		}).
			AddRow("PUBLIC", "PURCHASES", "CUSTOMER_ID", "PUBLIC", "USERS", "ID", "FK_COMP", 1).
			AddRow("PUBLIC", "PURCHASES", "PRODUCT_ID", "PUBLIC", "USERS", "ALT_ID", "FK_COMP", 2))
	// Overlay probe errors — no _gj_fk_metadata in this test
	mock.ExpectQuery(`FROM _gj_fk_metadata`).WillReturnError(sql.ErrNoRows)

	got, err := discoverCompositeFKsSnowflake(context.Background(), db)
	if err != nil {
		t.Fatalf("discoverCompositeFKsSnowflake err = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("composite FK count = %d, want 1", len(got))
	}
	info := got[0]
	if info.Schema != "PUBLIC" || info.Table != "PURCHASES" || info.ConstraintName != "FK_COMP" {
		t.Errorf("unexpected CompositeFKInfo meta: %+v", info)
	}
	if got, want := info.LocalCols, []string{"CUSTOMER_ID", "PRODUCT_ID"}; !eqStrs(got, want) {
		t.Errorf("LocalCols = %v, want %v", got, want)
	}
	if got, want := info.FKeyCols, []string{"ID", "ALT_ID"}; !eqStrs(got, want) {
		t.Errorf("FKeyCols = %v, want %v", got, want)
	}
}

func eqStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestSnowflakeDiscoverClusteringKeys feeds a mock INFORMATION_SCHEMA.TABLES
// response shaped like real Snowflake (quoted-lowercase identifier cases
// preserved, CLUSTER BY expression wrapped in LINEAR(...)) and asserts the
// discovery layer parses it back to the per-table column list.
func TestSnowflakeDiscoverClusteringKeys(t *testing.T) {
	db, mock := mockDBForSnowflakeSHOW(t)

	mock.ExpectQuery(`FROM information_schema.tables`).
		WillReturnRows(sqlmock.NewRows(
			[]string{"schema_name", "table_name", "clustering_key"},
		).
			AddRow("PUBLIC", "ORDERS", "LINEAR(CREATED_AT, REGION)").
			AddRow("PUBLIC", "EVENTS", "LINEAR(EVENT_TIME)").
			// Bare-parens form (no LINEAR wrapper).
			AddRow("PUBLIC", "METRICS", "(METRIC_DATE)"))

	got, err := discoverClusteringKeys(context.Background(), db)
	if err != nil {
		t.Fatalf("discoverClusteringKeys err = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d: %v", len(got), got)
	}
	if !eqStrs(got["PUBLIC:ORDERS"], []string{"CREATED_AT", "REGION"}) {
		t.Errorf("ORDERS clustering keys = %v, want [CREATED_AT REGION]", got["PUBLIC:ORDERS"])
	}
	if !eqStrs(got["PUBLIC:EVENTS"], []string{"EVENT_TIME"}) {
		t.Errorf("EVENTS clustering keys = %v, want [EVENT_TIME]", got["PUBLIC:EVENTS"])
	}
	if !eqStrs(got["PUBLIC:METRICS"], []string{"METRIC_DATE"}) {
		t.Errorf("METRICS clustering keys = %v, want [METRIC_DATE]", got["PUBLIC:METRICS"])
	}
}

// TestSnowflakeDiscoverClusteringKeysTolerantOfMissingColumn asserts that
// the discovery is non-fatal when INFORMATION_SCHEMA.TABLES doesn't expose
// a clustering_key column (older Snowflake accounts or restricted roles).
// The caller treats a returned error as "skip clustering optimization"
// rather than failing schema discovery.
func TestSnowflakeDiscoverClusteringKeysTolerantOfMissingColumn(t *testing.T) {
	db, mock := mockDBForSnowflakeSHOW(t)
	mock.ExpectQuery(`FROM information_schema.tables`).
		WillReturnError(sql.ErrConnDone)

	_, err := discoverClusteringKeys(context.Background(), db)
	if err == nil {
		t.Fatal("expected error propagation when INFORMATION_SCHEMA query fails")
	}
}

// TestSnowflakeClusteringAutoPartitionFromDiscovery exercises the full
// column-map + clustering-key → partition-key inference path end-to-end
// on a mocked discovery result. This is what enables automatic
// "missing partition filter" hinting on clustered tables.
func TestSnowflakeClusteringAutoPartitionFromDiscovery(t *testing.T) {
	// Build a DBInfo as if DiscoverInfo just returned it, then simulate
	// the attach-clustering step that runs in DiscoverInfo for Snowflake.
	di := NewDBInfo("snowflake", 0, "PUBLIC", "db", []DBColumn{
		{Schema: "PUBLIC", Table: "ORDERS", Name: "ID", Type: "bigint", PrimaryKey: true},
		{Schema: "PUBLIC", Table: "ORDERS", Name: "CREATED_AT", Type: "timestamp_ntz"},
		{Schema: "PUBLIC", Table: "ORDERS", Name: "REGION", Type: "varchar"},
	}, nil, nil)

	tbl := &di.Tables[0]
	tbl.ClusteringKeys = []string{"CREATED_AT", "REGION"}
	autoSetPartitionFromClustering(tbl)

	if tbl.PartitionKey != "CREATED_AT" {
		t.Errorf("expected PartitionKey=CREATED_AT (leading temporal cluster key), got %q", tbl.PartitionKey)
	}
	if tbl.PartitionRangeDays != 60 {
		t.Errorf("expected default PartitionRangeDays=60, got %d", tbl.PartitionRangeDays)
	}
}
