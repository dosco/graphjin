package sdata

import (
	"bytes"
	"reflect"
	"testing"
)

func TestDBInfoSnapshotRoundTripPreservesDiscoveryMetadata(t *testing.T) {
	columns := []DBColumn{
		{
			ID: 1, Name: "tenant_id", Type: "uuid", Schema: "public", Table: "orders",
			Comment: "Owning tenant", NotNull: true, PrimaryKey: true, UniqueKey: true,
			Default: "gen_random_uuid()", Index: true, IndexName: "orders_tenant_idx",
			OrigName: "TenantID", OrigTable: "Orders", OrigSchema: "dbo",
		},
		{
			ID: 2, Name: "customer_id", Type: "bigint", Schema: "public", Table: "orders",
			Comment: "Customer reference", FKeySchema: "public", FKeyTable: "customers", FKeyCol: "id",
			FKeyIsUnique: true, FKOnDelete: "cascade", FKOnUpdate: "restrict", Index: true,
		},
		{ID: 3, Name: "search_text", Type: "text", Schema: "public", Table: "orders", FullText: true},
	}
	functions := []DBFunction{{
		Schema: "public", Name: "order_total", Type: "numeric", Comment: "Computes total", Agg: true,
		Inputs:  []DBFuncParam{{ID: 1, Name: "order_id", Type: "bigint"}},
		Outputs: []DBFuncParam{{ID: 1, Name: "total", Type: "numeric"}},
	}}
	original := NewDBInfo("postgres", 160000, "public", "shop", columns, functions, nil)
	table, err := original.GetTable("public", "orders")
	if err != nil {
		t.Fatal(err)
	}
	table.Comment = "All purchases"
	table.ClusteringKeys = []string{"tenant_id", "customer_id"}
	table.ClusteringOrder = map[string]string{"tenant_id": "asc"}
	table.PartitionKeys = []string{"tenant_id", "customer_id"}
	table.PartitionKey = "tenant_id"
	table.PartitionRangeDays = 31
	table.AllowFiltering = true
	table.Args = []DBColumn{{Name: "region", Type: "text"}}
	original.VTables = []VirtualTable{{Name: "line_items", IDColumn: "id", TypeColumn: "kind", FKeyColumn: "order_id"}}
	original.CompositeFKs = []CompositeFKInfo{{
		Schema: "public", Table: "orders", ConstraintName: "orders_tenant_customer_fk",
		LocalCols: []string{"tenant_id", "customer_id"}, FKeySchema: "public", FKeyTable: "customers", FKeyCols: []string{"tenant_id", "id"},
	}}

	data, err := MarshalDBInfoSnapshot(original)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	loaded, err := UnmarshalDBInfoSnapshot(data)
	if err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	loadedData, err := MarshalDBInfoSnapshot(loaded)
	if err != nil {
		t.Fatalf("marshal loaded snapshot: %v", err)
	}
	if !reflect.DeepEqual(data, loadedData) {
		t.Fatalf("snapshot changed after round trip\noriginal: %s\nloaded:   %s", data, loadedData)
	}
	loadedTable, err := loaded.GetTable("public", "orders")
	if err != nil {
		t.Fatalf("restored table lookup: %v", err)
	}
	if _, err := loaded.GetColumn("public", "orders", "customer_id"); err != nil {
		t.Fatalf("restored column lookup: %v", err)
	}
	if len(loadedTable.PrimaryCols) != 1 || loadedTable.PrimaryCol.Name != "tenant_id" || len(loadedTable.FullText) != 1 {
		t.Fatalf("derived columns not restored: primary=%+v fulltext=%+v", loadedTable.PrimaryCols, loadedTable.FullText)
	}
}

func TestDBInfoSnapshotRejectsUnknownVersion(t *testing.T) {
	if _, err := UnmarshalDBInfoSnapshot([]byte(`{"version":999}`)); err == nil {
		t.Fatal("expected unsupported snapshot version error")
	}
}

func TestDBInfoSnapshotIsCanonicalAcrossDiscoveryOrder(t *testing.T) {
	columns := []DBColumn{
		{ID: 3, Name: "status", Type: "text", Schema: "public", Table: "orders"},
		{ID: 1, Name: "id", Type: "bigint", Schema: "public", Table: "orders", PrimaryKey: true},
		{ID: 2, Name: "customer_id", Type: "bigint", Schema: "public", Table: "orders", FKeySchema: "public", FKeyTable: "customers", FKeyCol: "id"},
		{ID: 2, Name: "name", Type: "text", Schema: "public", Table: "customers"},
		{ID: 1, Name: "id", Type: "bigint", Schema: "public", Table: "customers", PrimaryKey: true},
	}
	functions := []DBFunction{
		{Schema: "public", Name: "z_total", Type: "numeric"},
		{Schema: "public", Name: "a_total", Type: "numeric"},
	}
	first := NewDBInfo("postgres", 160000, "public", "shop", columns, functions, nil)
	first.VTables = []VirtualTable{{Name: "z_items"}, {Name: "a_items"}}
	first.CompositeFKs = []CompositeFKInfo{
		{Schema: "public", Table: "orders", ConstraintName: "z_fk", LocalCols: []string{"customer_id"}, FKeySchema: "public", FKeyTable: "customers", FKeyCols: []string{"id"}},
		{Schema: "public", Table: "orders", ConstraintName: "a_fk", LocalCols: []string{"customer_id"}, FKeySchema: "public", FKeyTable: "customers", FKeyCols: []string{"id"}},
	}

	reversedColumns := append([]DBColumn(nil), columns...)
	for left, right := 0, len(reversedColumns)-1; left < right; left, right = left+1, right-1 {
		reversedColumns[left], reversedColumns[right] = reversedColumns[right], reversedColumns[left]
	}
	second := NewDBInfo("postgres", 160000, "public", "shop", reversedColumns, []DBFunction{functions[1], functions[0]}, nil)
	second.VTables = []VirtualTable{{Name: "a_items"}, {Name: "z_items"}}
	second.CompositeFKs = []CompositeFKInfo{first.CompositeFKs[1], first.CompositeFKs[0]}

	firstData, err := MarshalDBInfoSnapshot(first)
	if err != nil {
		t.Fatalf("marshal first snapshot: %v", err)
	}
	secondData, err := MarshalDBInfoSnapshot(second)
	if err != nil {
		t.Fatalf("marshal second snapshot: %v", err)
	}
	if !bytes.Equal(firstData, secondData) {
		t.Fatalf("equivalent discovery metadata produced different snapshots\nfirst:  %s\nsecond: %s", firstData, secondData)
	}
}
