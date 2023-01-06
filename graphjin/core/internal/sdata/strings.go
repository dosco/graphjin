package sdata

import (
	"fmt"
	"strings"
)

func (ti *DBTable) String() string {
	return ti.Schema + "." + ti.Name
}

func (col DBColumn) String() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("%s.%s.%s", col.Schema, col.Table, col.Name))
	sb.WriteString(fmt.Sprintf(" [id:%d, type:%v, array:%t, notNull:%t, fulltext:%t]",
		col.ID, col.Type, col.Array, col.NotNull, col.FullText))

	if col.FKeyCol != "" {
		sb.WriteString(fmt.Sprintf(" -> %s.%s.%s",
			col.FKeySchema, col.FKeyTable, col.FKeyCol))
	}
	return sb.String()
}

func (fn DBFunction) String() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("%s.%s", fn.Schema, fn.Name))
	sb.WriteString(fmt.Sprintf(" [type:%v, agg:%t] (", fn.Type, fn.Agg))

	for _, v := range fn.Inputs {
		if v.Name == "" {
			sb.WriteString(fmt.Sprintf("%d: %v [array:%t]", v.ID, v.Type, v.Array))
		} else {
			sb.WriteString(fmt.Sprintf("%s: %v [array:%t]", v.Name, v.Type, v.Array))
		}
	}

	sb.WriteString(") => ")

	for _, v := range fn.Outputs {
		if v.Name == "" {
			sb.WriteString(fmt.Sprintf("%d: %v [array:%t]", v.ID, v.Type, v.Array))
		} else {
			sb.WriteString(fmt.Sprintf("%s: %v [array:%t]", v.Name, v.Type, v.Array))
		}
	}

	return sb.String()
}

func (re *DBRel) String() string {
	return fmt.Sprintf("'%s' --(%s)--> '%s'",
		re.Left.Col.String(),
		re.Type,
		re.Right.Col.String())

}
