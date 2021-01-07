package psql

import (
	"bytes"
	"strconv"
)

func alias(w *bytes.Buffer, alias string) {
	w.WriteString(` AS `)
	w.WriteString(alias)
}

func aliasWithID(w *bytes.Buffer, alias string, id int32) {
	w.WriteString(` AS `)
	w.WriteString(alias)
	w.WriteString(`_`)
	int32String(w, id)
}

func colWithTable(w *bytes.Buffer, table, col string) {
	w.WriteString(table)
	w.WriteString(`.`)
	w.WriteString(col)
}

func colWithTableID(w *bytes.Buffer, table string, id int32, col string) {
	w.WriteString(table)
	if id >= 0 {
		w.WriteString(`_`)
		int32String(w, id)
	}
	w.WriteString(`.`)
	w.WriteString(col)
}

func quoted(w *bytes.Buffer, identifier string) {
	// w.WriteString(`"`)
	w.WriteString(identifier)
	// w.WriteString(`"`)
}

func squoted(w *bytes.Buffer, identifier string) {
	w.WriteString(`'`)
	w.WriteString(identifier)
	w.WriteString(`'`)
}

func int32String(w *bytes.Buffer, val int32) {
	w.WriteString(strconv.FormatInt(int64(val), 10))
}
