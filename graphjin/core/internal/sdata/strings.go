package sdata

import "fmt"

func (ti *DBTable) String() string {
	return ti.Schema + "." + ti.Name
}

func (col DBColumn) String() string {
	colName := col.Name
	if col.Array {
		colName += "[]"
	}

	if col.FKeyCol != "" {
		return fmt.Sprintf("%s.%s.%s -FK-> %s.%s.%s",
			col.Schema, col.Table, colName, col.FKeySchema, col.FKeyTable, col.FKeyCol)
	} else {
		return fmt.Sprintf("%s.%s.%s", col.Schema, col.Table, colName)
	}
}

func (re *DBRel) String() string {
	return fmt.Sprintf("'%s' --(%s)--> '%s'",
		re.Left.Col.String(),
		re.Type,
		re.Right.Col.String())

}
