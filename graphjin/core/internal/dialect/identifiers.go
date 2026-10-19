package dialect

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// identifierNames keeps physical names scoped to their owning schema and table.
// Aliases and generated SQL names must never pass through this mapping.
type identifierNames struct {
	tables      map[string]sdata.DBTable
	columns     map[string]map[string]string
	schemas     map[string]string
	unqualified map[string]string
}

func (n *identifierNames) SetNameMap(tables []sdata.DBTable) {
	n.tables = make(map[string]sdata.DBTable, len(tables))
	n.columns = make(map[string]map[string]string, len(tables))
	n.schemas = make(map[string]string)
	n.unqualified = make(map[string]string)
	for _, t := range tables {
		key := t.Schema + "\x00" + t.Name
		n.tables[key] = t
		n.columns[key] = make(map[string]string, len(t.Columns))
		for _, c := range t.Columns {
			n.columns[key][c.Name] = c.SQLName()
		}
		if old, ok := n.unqualified[t.Name]; ok && old != key {
			n.unqualified[t.Name] = "" // ambiguous: the caller must supply the schema
		} else if !ok {
			n.unqualified[t.Name] = key
		}
		n.schemas[t.Schema] = t.SQLSchema()
	}
}

func (n *identifierNames) columnName(schema, table, column string) (string, error) {
	key := schema + "\x00" + table
	if schema == "" {
		var ok bool
		key, ok = n.unqualified[table]
		if !ok {
			// Compiler-generated aliases append a select ID to the table name.
			if i := strings.LastIndexByte(table, '_'); i > 0 {
				if _, err := strconv.Atoi(table[i+1:]); err == nil {
					table = table[:i]
					key, ok = n.unqualified[table]
				}
			}
		}
		if ok && key == "" {
			return "", fmt.Errorf("ambiguous physical column %s.%s: schema is required", table, column)
		}
	}
	if name, ok := n.columns[key][column]; ok {
		return name, nil
	}
	// Derived fields and CTEs are already SQL identifiers, not physical columns.
	return column, nil
}

func (n *identifierNames) tableNames(schema, table string) (string, string) {
	if t, ok := n.tables[schema+"\x00"+table]; ok {
		return t.SQLSchema(), t.SQLName()
	}
	if original, ok := n.schemas[schema]; ok {
		schema = original
	}
	return schema, table
}
