package sdata

import "fmt"

func (ti *DBTableInfo) String() string {
	return ti.Name
}

func (col *DBColumn) String() string {
	return col.Table + "." + col.Name
}

func (re *DBRel) String() string {
	if re.Type == RelOneToManyThrough {
		return fmt.Sprintf("'%s' --(%s, %s)--> '%s'",
			re.Left.Col,
			re.Through.ColL,
			re.Through.ColR,
			re.Right.Col)
	}
	return fmt.Sprintf("'%s' --(%s)--> '%s'",
		re.Left.Col,
		re.Type,
		re.Right.Col)

}
