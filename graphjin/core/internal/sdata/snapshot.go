package sdata

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const dbInfoSnapshotVersion = 1

// dbInfoSnapshot is the durable, full-fidelity representation of discovery
// metadata. Lookup maps and derived table slices are rebuilt when loading.
type dbInfoSnapshot struct {
	Version      int               `json:"version"`
	Type         string            `json:"type"`
	DBVersion    int               `json:"db_version"`
	Schema       string            `json:"schema"`
	Name         string            `json:"name"`
	Tables       []DBTable         `json:"tables"`
	Functions    []DBFunction      `json:"functions,omitempty"`
	VTables      []VirtualTable    `json:"virtual_tables,omitempty"`
	CompositeFKs []CompositeFKInfo `json:"composite_foreign_keys,omitempty"`
	Hash         int               `json:"hash"`
}

// MarshalDBInfoSnapshot serializes every discovery field needed to rebuild a
// DBSchema. It deliberately excludes private lookup maps because they are
// process-local derived state.
func MarshalDBInfoSnapshot(di *DBInfo) ([]byte, error) {
	if di == nil {
		return nil, fmt.Errorf("database info is required")
	}
	tables := make([]DBTable, len(di.Tables))
	for i := range di.Tables {
		tables[i] = canonicalSnapshotTable(di.Tables[i])
	}
	sort.Slice(tables, func(i, j int) bool {
		return snapshotTableKey(tables[i]) < snapshotTableKey(tables[j])
	})
	functions := append([]DBFunction(nil), di.Functions...)
	sort.Slice(functions, func(i, j int) bool {
		return dbFunctionSortKey(functions[i]) < dbFunctionSortKey(functions[j])
	})
	virtualTables := append([]VirtualTable(nil), di.VTables...)
	sort.Slice(virtualTables, func(i, j int) bool {
		return snapshotVirtualTableKey(virtualTables[i]) < snapshotVirtualTableKey(virtualTables[j])
	})
	compositeFKs := append([]CompositeFKInfo(nil), di.CompositeFKs...)
	sort.Slice(compositeFKs, func(i, j int) bool {
		return snapshotCompositeFKKey(compositeFKs[i]) < snapshotCompositeFKKey(compositeFKs[j])
	})
	return json.Marshal(dbInfoSnapshot{
		Version:      dbInfoSnapshotVersion,
		Type:         di.Type,
		DBVersion:    di.Version,
		Schema:       di.Schema,
		Name:         di.Name,
		Tables:       tables,
		Functions:    functions,
		VTables:      virtualTables,
		CompositeFKs: compositeFKs,
		Hash:         di.hash,
	})
}

func canonicalSnapshotTable(table DBTable) DBTable {
	out := table
	out.Columns = append([]DBColumn(nil), table.Columns...)
	sort.Slice(out.Columns, func(i, j int) bool {
		left, right := out.Columns[i], out.Columns[j]
		if left.ID != right.ID {
			return left.ID < right.ID
		}
		return snapshotColumnKey(left) < snapshotColumnKey(right)
	})

	// These fields are derived from Columns when a snapshot is loaded. Rebuild
	// them here as well so their serialized representation cannot retain the
	// nondeterministic order returned by an introspection query.
	out.PrimaryCols = nil
	out.PrimaryCol = DBColumn{}
	out.FullText = nil
	for _, column := range out.Columns {
		if column.PrimaryKey {
			out.PrimaryCols = append(out.PrimaryCols, column)
		}
		if column.FullText {
			out.FullText = append(out.FullText, column)
		}
	}
	if len(out.PrimaryCols) != 0 {
		out.PrimaryCol = out.PrimaryCols[0]
	}
	return out
}

func snapshotTableKey(table DBTable) string {
	return strings.Join([]string{table.Database, table.Schema, table.Name, table.Type, table.OrigSchema, table.OrigName}, "\x00")
}

func snapshotColumnKey(column DBColumn) string {
	return strings.Join([]string{column.Database, column.Schema, column.Table, column.Name, column.Type, column.OrigName}, "\x00")
}

func dbFunctionSortKey(function DBFunction) string {
	parts := []string{function.Schema, function.Name, function.Type, function.Comment}
	for _, input := range function.Inputs {
		parts = append(parts, fmt.Sprintf("i:%d:%s:%s:%t", input.ID, input.Name, input.Type, input.Array))
	}
	for _, output := range function.Outputs {
		parts = append(parts, fmt.Sprintf("o:%d:%s:%s:%t", output.ID, output.Name, output.Type, output.Array))
	}
	return strings.Join(parts, "\x00")
}

func snapshotVirtualTableKey(table VirtualTable) string {
	return strings.Join([]string{table.Name, table.IDColumn, table.TypeColumn, table.FKeyColumn}, "\x00")
}

func snapshotCompositeFKKey(fk CompositeFKInfo) string {
	return strings.Join([]string{fk.Schema, fk.Table, fk.ConstraintName, fk.FKeySchema, fk.FKeyTable, strings.Join(fk.LocalCols, "\x01"), strings.Join(fk.FKeyCols, "\x01")}, "\x00")
}

// UnmarshalDBInfoSnapshot loads a durable discovery snapshot and reconstructs
// lookup maps and table-derived primary/full-text column slices.
func UnmarshalDBInfoSnapshot(data []byte) (*DBInfo, error) {
	var snap dbInfoSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("decode database info snapshot: %w", err)
	}
	if snap.Version != dbInfoSnapshotVersion {
		return nil, fmt.Errorf("unsupported database info snapshot version %d", snap.Version)
	}

	di := &DBInfo{
		Type:         snap.Type,
		Version:      snap.DBVersion,
		Schema:       snap.Schema,
		Name:         snap.Name,
		Functions:    snap.Functions,
		VTables:      snap.VTables,
		CompositeFKs: snap.CompositeFKs,
		colMap:       make(map[string]int),
		tableMap:     make(map[string]int),
		hash:         snap.Hash,
	}
	for _, stored := range snap.Tables {
		t := restoreDBTable(stored)
		di.AddTable(t)
	}
	return di, nil
}

func restoreDBTable(stored DBTable) DBTable {
	t := stored
	t.colMap = make(map[string]int, len(t.Columns))
	t.PrimaryCols = nil
	t.PrimaryCol = DBColumn{}
	tFullText := make([]DBColumn, 0, len(t.FullText))
	for i := range t.Columns {
		c := &t.Columns[i]
		if c.Schema == "" {
			c.Schema = t.Schema
		}
		if c.Table == "" {
			c.Table = t.Name
		}
		if c.Database == "" {
			c.Database = t.Database
		}
		t.colMap[c.Name] = i
		if c.PrimaryKey {
			t.PrimaryCols = append(t.PrimaryCols, *c)
		}
		if c.FullText {
			tFullText = append(tFullText, *c)
		}
	}
	t.FullText = tFullText
	if len(t.PrimaryCols) != 0 {
		t.PrimaryCol = t.PrimaryCols[0]
	}
	return t
}
