package sdata

import "fmt"

func (ti *DBTable) String() string {
	return ti.Schema + "." + ti.Name
}

func (col DBColumn) String() string {
	return col.Schema + "." + col.Table + "." + col.Name
}

func (re *DBRel) String() string {
	lc := re.Left.Col.String()
	if re.Left.Col.Array {
		lc += "[]"
	}

	rc := re.Right.Col.String()
	if re.Right.Col.Array {
		lc += "[]"
	}

	return fmt.Sprintf("'%s' --(%s)--> '%s'",
		lc,
		re.Type,
		rc)

}
