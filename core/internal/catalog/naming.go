package catalog

import "strings"

// NameColumnCandidates are the column names that name a row, in order of
// preference. The catalog teaches selecting this column and core reports a
// result that identifies rows without it, so both read the same list.
var NameColumnCandidates = []string{"name", "title", "full_name", "display_name", "label", "username"}

// NamingColumn is one column considered when choosing the column that names a
// table's rows.
type NamingColumn struct {
	Name   string
	Type   string
	Unique bool
	// Excluded marks a column that identifies rows or cannot name them: a
	// primary or foreign key, an array, a blocked column.
	Excluded bool
}

// PickNameColumn returns the column that names a table's rows: the first
// NameColumnCandidates match, else the first unique text column (a
// reference, a slug, an email), else "". It reads the n columns through at,
// so core can call it per query without building a slice.
func PickNameColumn(n int, at func(int) NamingColumn) string {
	for _, want := range NameColumnCandidates {
		for i := 0; i < n; i++ {
			if col := at(i); col.Name == want && namingUsable(col) {
				return col.Name
			}
		}
	}
	for i := 0; i < n; i++ {
		if col := at(i); col.Unique && namingUsable(col) {
			return col.Name
		}
	}
	return ""
}

func namingUsable(col NamingColumn) bool {
	if col.Excluded {
		return false
	}
	typ := strings.ToLower(col.Type)
	return strings.Contains(typ, "text") || strings.Contains(typ, "char") || strings.Contains(typ, "string")
}

// metadataNameColumn picks the naming column of a catalog table. fkColumns
// holds the ids of columns that are foreign keys.
func metadataNameColumn(cols []MetadataColumn, fkColumns map[string]bool) string {
	return PickNameColumn(len(cols), func(i int) NamingColumn {
		c := cols[i]
		return NamingColumn{Name: c.ColumnName, Type: c.Type, Unique: c.UniqueKey, Excluded: c.PrimaryKey || c.Array || fkColumns[c.ID]}
	})
}
