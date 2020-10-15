package sdata

import "fmt"

func (ti *DBTableInfo) String() string {
	return ti.Name
}

func (col DBColumn) String() string {
	return col.Table + "." + col.Name
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

	if re.Type == RelOneToManyThrough {
		tlc := re.Through.ColL.String()
		if re.Through.ColL.Array {
			tlc += "[]"
		}

		trc := re.Through.ColR.String()
		if re.Through.ColR.Array {
			trc += "[]"
		}

		return fmt.Sprintf("'%s' --(%s, %s)--> '%s'",
			lc,
			tlc,
			trc,
			rc)
	}
	return fmt.Sprintf("'%s' --(%s)--> '%s'",
		lc,
		re.Type,
		rc)

}
