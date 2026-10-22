package filecontent

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/xuri/excelize/v2"
)

// Column is one column of a table derived from a sheet.
type Column struct {
	Name string `json:"name"`
	Type string `json:"type"` // DOUBLE, DATE, BOOLEAN or VARCHAR, as DuckDB reads it
}

// Table is one Parquet file derived from a sheet. Every sheet gives a raw
// table; a sheet with a recognisable header also gives a typed table.
type Table struct {
	Sheet     string   `json:"sheet"`
	Name      string   `json:"name"` // SQL name, e.g. sales_2025__sales_2025 or sales_2025__sales_2025__raw
	File      string   `json:"file"` // Parquet file name inside the derived directory
	Raw       bool     `json:"raw"`
	HeaderRow int      `json:"header_row,omitempty"` // 1-based sheet row of the last header row
	Columns   []Column `json:"columns"`
	Rows      int      `json:"rows"`
}

// WriteTables writes every sheet of doc as Parquet into dir and describes the
// tables. base names the file, e.g. its sanitized file name.
//
// The raw table keeps every cell as text, named by column letter, with the
// sheet row number, so messy layouts (title rows, several tables per sheet,
// notes) stay queryable. The typed table uses the detected header rows and
// gives each column one type, so sums and comparisons work directly.
func WriteTables(doc *Document, dir, base string) ([]Table, error) {
	var tables []Table
	for _, sheet := range doc.Sheets {
		name := Identifier(base)
		if doc.Kind != KindTable {
			name += "__" + Identifier(sheet.Name)
		}
		raw, err := writeRaw(sheet, dir, name+"__raw")
		if err != nil {
			return nil, err
		}
		tables = append(tables, raw)
		typed, ok, err := writeTyped(sheet, dir, name)
		if err != nil {
			return nil, err
		}
		if ok {
			tables = append(tables, typed)
		}
	}
	return tables, nil
}

var nonIdent = regexp.MustCompile(`[^0-9a-z]+`)

// Identifier makes s a lowercase SQL identifier: letters, digits and _.
func Identifier(s string) string {
	id := strings.Trim(nonIdent.ReplaceAllString(strings.ToLower(s), "_"), "_")
	if id == "" {
		return "t"
	}
	if id[0] >= '0' && id[0] <= '9' {
		id = "t_" + id
	}
	return id
}

func sheetWidth(s Sheet) int {
	w := 0
	for _, r := range s.Rows {
		w = max(w, len(r))
	}
	return w
}

func cell(row []Cell, j int) Cell {
	if j < len(row) {
		return row[j]
	}
	return Cell{}
}

func writeRaw(s Sheet, dir, name string) (Table, error) {
	width := sheetWidth(s)
	cols := []Column{{Name: "row_number", Type: "BIGINT"}}
	group := parquet.Group{"row_number": parquet.Int(64)}
	for j := 0; j < width; j++ {
		letter, _ := excelize.ColumnNumberToName(j + 1)
		cols = append(cols, Column{Name: letter, Type: "VARCHAR"})
		group[letter] = parquet.Optional(parquet.String())
	}
	rows := make([][]any, 0, len(s.Rows))
	for i, r := range s.Rows {
		vals := make([]any, len(cols))
		vals[0] = int64(i + 1)
		for j := 0; j < width; j++ {
			if c := cell(r, j); c.Type != CellEmpty {
				vals[j+1] = c.Value
			}
		}
		rows = append(rows, vals)
	}
	t := Table{Sheet: s.Name, Name: name, File: name + ".parquet", Raw: true, Columns: cols, Rows: len(rows)}
	return t, writeParquet(filepath.Join(dir, t.File), group, cols, rows)
}

// headerLike: a row of column titles has at least two values, all text or
// year-like numbers (a row of years often sits under a merged title).
func headerLike(row []Cell) bool {
	filled, texts := 0, 0
	for _, c := range row {
		switch c.Type {
		case CellEmpty:
		case CellString:
			filled++
			texts++
		case CellNumber:
			n, err := strconv.ParseFloat(c.Value, 64)
			if err != nil || n != float64(int(n)) || n < 1900 || n > 2100 {
				return false
			}
			filled++
		default:
			return false
		}
	}
	return filled >= 2 && (texts > 0 || filled >= 3)
}

// subHeaderLike: under a header row, a row of only titles or years (one or
// more) continues the header.
func subHeaderLike(row []Cell) bool {
	filled := 0
	for _, c := range row {
		switch c.Type {
		case CellEmpty:
		case CellString:
			filled++
		case CellNumber:
			n, err := strconv.ParseFloat(c.Value, 64)
			if err != nil || n != float64(int(n)) || n < 1900 || n > 2100 {
				return false
			}
			filled++
		default:
			return false
		}
	}
	return filled >= 1
}

func dataLike(row []Cell) bool {
	filled, values := 0, 0
	for _, c := range row {
		if c.Type == CellEmpty {
			continue
		}
		filled++
		if c.Type == CellNumber || c.Type == CellDate || c.Type == CellBool {
			values++
		}
	}
	return filled >= 2 && values > 0
}

// detectHeader finds the header block: one or two header rows directly above
// the first data row, within the first 30 rows. It returns the 0-based index
// of the first header row and how many there are.
func detectHeader(s Sheet) (int, int, bool) {
	limit := min(len(s.Rows), 30)
	for i := 0; i < limit; i++ {
		if !headerLike(s.Rows[i]) {
			continue
		}
		// A second header-like row (often years under a merged title) belongs
		// to the header, even though a row of years also looks like data.
		if i+2 < len(s.Rows) && subHeaderLike(s.Rows[i+1]) && dataLike(s.Rows[i+2]) {
			return i, 2, true
		}
		if i+1 < len(s.Rows) && dataLike(s.Rows[i+1]) {
			return i, 1, true
		}
	}
	return 0, 0, false
}

func writeTyped(s Sheet, dir, name string) (Table, bool, error) {
	h, n, ok := detectHeader(s)
	if !ok {
		return Table{}, false, nil
	}
	width := sheetWidth(s)
	// Combine header rows. A title over several columns sits in its first
	// cell only, so carry it right until the next title.
	labels := make([]string, width)
	for k := 0; k < n; k++ {
		carry := ""
		for j := 0; j < width; j++ {
			v := strings.TrimSpace(cell(s.Rows[h+k], j).Value)
			if v != "" {
				carry = v
			} else if k == 0 && n > 1 && j > 0 {
				v = carry
			}
			if v != "" {
				labels[j] = strings.TrimSpace(labels[j] + " " + v)
			}
		}
	}
	data := s.Rows[h+n:]
	seen := map[string]bool{"source_row": true}
	cols := []Column{{Name: "source_row", Type: "BIGINT"}}
	group := parquet.Group{"source_row": parquet.Int(64)}
	types := make([]CellType, width)
	for j := 0; j < width; j++ {
		types[j] = columnType(data, j)
		id := Identifier(labels[j])
		if labels[j] == "" {
			letter, _ := excelize.ColumnNumberToName(j + 1)
			id = "col_" + strings.ToLower(letter)
		}
		for seen[id] {
			id += "_2"
		}
		seen[id] = true
		col := Column{Name: id}
		switch types[j] {
		case CellNumber:
			col.Type = "DOUBLE"
			group[id] = parquet.Optional(parquet.Leaf(parquet.DoubleType))
		case CellDate:
			col.Type = "DATE"
			group[id] = parquet.Optional(parquet.Date())
		case CellBool:
			col.Type = "BOOLEAN"
			group[id] = parquet.Optional(parquet.Leaf(parquet.BooleanType))
		default:
			col.Type = "VARCHAR"
			group[id] = parquet.Optional(parquet.String())
		}
		cols = append(cols, col)
	}
	rows := make([][]any, 0, len(data))
	for i, r := range data {
		vals := make([]any, len(cols))
		vals[0] = int64(h + n + i + 1)
		empty := true
		for j := 0; j < width; j++ {
			c := cell(r, j)
			if c.Type == CellEmpty {
				continue
			}
			empty = false
			vals[j+1] = typedValue(c, types[j])
		}
		if !empty {
			rows = append(rows, vals)
		}
	}
	t := Table{Sheet: s.Name, Name: name, File: name + ".parquet", HeaderRow: h + n, Columns: cols, Rows: len(rows)}
	return t, true, writeParquet(filepath.Join(dir, t.File), group, cols, rows)
}

// columnType is the one type every value in the column has; anything mixed is
// text. Datetimes stay text so no time is lost.
func columnType(data [][]Cell, j int) CellType {
	var t CellType
	for _, r := range data {
		c := cell(r, j)
		if c.Type == CellEmpty {
			continue
		}
		ct := c.Type
		if ct == CellDate && len(c.Value) > len(time.DateOnly) {
			ct = CellString
		}
		if t == CellEmpty {
			t = ct
		} else if t != ct {
			return CellString
		}
	}
	if t == CellEmpty {
		return CellString
	}
	return t
}

func typedValue(c Cell, t CellType) any {
	switch t {
	case CellNumber:
		n, _ := strconv.ParseFloat(c.Value, 64)
		return n
	case CellDate:
		d, err := time.Parse(time.DateOnly, c.Value)
		if err != nil {
			return nil
		}
		return d
	case CellBool:
		return c.Value == "true"
	}
	return c.Value
}

var unixEpoch = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)

func writeParquet(path string, group parquet.Group, cols []Column, rows [][]any) error {
	schema := parquet.NewSchema("t", group)
	index := make([]int, len(cols))
	for i, c := range cols {
		leaf, ok := schema.Lookup(c.Name)
		if !ok {
			return fmt.Errorf("filecontent: parquet column %q missing", c.Name)
		}
		index[i] = leaf.ColumnIndex
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := parquet.NewWriter(f, schema, parquet.Compression(&parquet.Zstd))
	batch := make([]parquet.Row, 0, 1024)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		_, err := w.WriteRows(batch)
		batch = batch[:0]
		return err
	}
	for _, vals := range rows {
		row := make(parquet.Row, len(cols))
		for i, v := range vals {
			// Columns other than the row number are optional: definition
			// level 1 holds a value, 0 is null.
			def := 1
			if i == 0 {
				def = 0
			}
			var pv parquet.Value
			switch x := v.(type) {
			case nil:
				pv, def = parquet.NullValue(), 0
			case int64:
				pv = parquet.Int64Value(x)
			case float64:
				pv = parquet.DoubleValue(x)
			case bool:
				pv = parquet.BooleanValue(x)
			case time.Time:
				pv = parquet.Int32Value(int32(x.Sub(unixEpoch).Hours() / 24))
			case string:
				pv = parquet.ByteArrayValue([]byte(x))
			}
			row[index[i]] = pv.Level(0, def, index[i])
		}
		batch = append(batch, row)
		if len(batch) == cap(batch) {
			if err := flush(); err != nil {
				f.Close()
				return err
			}
		}
	}
	if err := flush(); err != nil {
		f.Close()
		return err
	}
	if err := w.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
