package filecontent

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/xuri/excelize/v2"
)

func workbook(t *testing.T) []byte {
	t.Helper()
	f := excelize.NewFile()
	sh := "Sales 2025"
	if err := f.SetSheetName("Sheet1", sh); err != nil {
		t.Fatal(err)
	}
	// A title row, then a two-row header: a merged title over years.
	set := func(axis string, v any) {
		if err := f.SetCellValue(sh, axis, v); err != nil {
			t.Fatal(err)
		}
	}
	set("A1", "Regional sales")
	set("A3", "Region")
	set("B3", "Units sold")
	set("B4", 2024)
	set("C4", 2025)
	set("D3", "Opened")
	set("E3", "Internal cost")
	dateStyle, err := f.NewStyle(&excelize.Style{NumFmt: 14})
	if err != nil {
		t.Fatal(err)
	}
	rows := []struct {
		region   string
		y24, y25 int
		opened   time.Time
		cost     float64
	}{
		{"North", 120, 150, time.Date(2021, 3, 1, 0, 0, 0, 0, time.UTC), 10.5},
		{"South", 90, 80, time.Date(2022, 7, 15, 0, 0, 0, 0, time.UTC), 7.25},
	}
	for i, r := range rows {
		n := i + 5
		set("A"+strconv.Itoa(n), r.region)
		set("B"+strconv.Itoa(n), r.y24)
		set("C"+strconv.Itoa(n), r.y25)
		set("D"+strconv.Itoa(n), r.opened)
		if err := f.SetCellStyle(sh, "D"+strconv.Itoa(n), "D"+strconv.Itoa(n), dateStyle); err != nil {
			t.Fatal(err)
		}
		set("E"+strconv.Itoa(n), r.cost)
	}
	if err := f.SetColVisible(sh, "E", false); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipped(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestXLSXKeepsDatesNumbersAndHiddenColumns(t *testing.T) {
	doc, err := Extract("sales.xlsx", workbook(t))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Kind != KindSpreadsheet || len(doc.Sheets) != 1 {
		t.Fatalf("kind %s sheets %d", doc.Kind, len(doc.Sheets))
	}
	s := doc.Sheets[0]
	if got := s.Rows[4][3]; got.Type != CellDate || got.Value != "2021-03-01" {
		t.Fatalf("date cell = %+v", got)
	}
	if got := s.Rows[4][1]; got.Type != CellNumber || got.Value != "120" {
		t.Fatalf("number cell = %+v", got)
	}
	if len(s.HiddenColumns) != 1 || s.HiddenColumns[0] != "E" {
		t.Fatalf("hidden columns = %v", s.HiddenColumns)
	}
	text := doc.Text()
	for _, want := range []string{"[sheet Sales 2025]", "hidden columns: E", "r5: North | 120 | 150 | 2021-03-01 | 10.5"} {
		if !strings.Contains(text, want) {
			t.Fatalf("text missing %q:\n%s", want, text)
		}
	}
}

func readParquet(t *testing.T, path string) (columns []string, rows []map[string]any) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, _ := f.Stat()
	pf, err := parquet.OpenFile(f, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range pf.Schema().Columns() {
		columns = append(columns, c[0])
	}
	r := parquet.NewReader(pf)
	defer r.Close()
	for {
		row := map[string]any{}
		if err := r.Read(&row); err != nil {
			break
		}
		rows = append(rows, row)
	}
	return columns, rows
}

func TestWriteTablesCombinesTwoHeaderRowsAndTypesColumns(t *testing.T) {
	doc, err := Extract("Regional Sales.xlsx", workbook(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	tables, err := WriteTables(doc, dir, "Regional Sales")
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 2 {
		t.Fatalf("tables = %+v", tables)
	}
	raw, typed := tables[0], tables[1]
	if !raw.Raw || raw.Name != "regional_sales__sales_2025__raw" || raw.Rows != 6 {
		t.Fatalf("raw table = %+v", raw)
	}
	if typed.Name != "regional_sales__sales_2025" || typed.HeaderRow != 4 || typed.Rows != 2 {
		t.Fatalf("typed table = %+v", typed)
	}
	want := []Column{{"source_row", "BIGINT"}, {"region", "VARCHAR"}, {"units_sold_2024", "DOUBLE"},
		{"units_sold_2025", "DOUBLE"}, {"opened", "DATE"}, {"internal_cost", "DOUBLE"}}
	for i, c := range want {
		if typed.Columns[i] != c {
			t.Fatalf("column %d = %+v, want %+v", i, typed.Columns[i], c)
		}
	}
	_, rows := readParquet(t, filepath.Join(dir, typed.File))
	if len(rows) != 2 || rows[1]["region"] != "South" || rows[1]["units_sold_2025"] != 80.0 || rows[0]["source_row"] != int64(5) {
		t.Fatalf("typed rows = %+v", rows)
	}
	_, rawRows := readParquet(t, filepath.Join(dir, raw.File))
	if rawRows[0]["row_number"] != int64(1) || rawRows[0]["A"] != "Regional sales" {
		t.Fatalf("raw first row = %+v", rawRows[0])
	}
}

func TestCSVBecomesOneTable(t *testing.T) {
	doc, err := Extract("prices.csv", []byte("\xef\xbb\xbfSKU,Price\nHI-1,12.50\nHI-2,7\n"))
	if err != nil {
		t.Fatal(err)
	}
	tables, err := WriteTables(doc, t.TempDir(), "prices")
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 2 || tables[1].Name != "prices" || tables[1].Columns[2] != (Column{"price", "DOUBLE"}) {
		t.Fatalf("tables = %+v", tables)
	}
}

func TestDOCXSectionsFollowHeadingsAndTablesKeepRows(t *testing.T) {
	body := `<w:document xmlns:w="w"><w:body>
<w:p><w:pPr><w:pStyle w:val="Title"/></w:pPr><w:r><w:t>Travel policy</w:t></w:r></w:p>
<w:p><w:r><w:t>Applies to everyone.</w:t></w:r></w:p>
<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Per diem</w:t></w:r></w:p>
<w:tbl><w:tr><w:tc><w:p><w:r><w:t>Tier 1</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>70</w:t></w:r></w:p></w:tc></w:tr></w:tbl>
</w:body></w:document>`
	doc, err := Extract("policy.docx", zipped(t, map[string]string{"word/document.xml": body}))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Units) != 2 || doc.Units[1].Label != "section Per diem" || !strings.Contains(doc.Units[1].Text, "Tier 1 | 70") {
		t.Fatalf("units = %+v", doc.Units)
	}
}

func TestPPTXUnitsAreSlidesInOrder(t *testing.T) {
	slide := func(text string) string {
		return `<p:sld xmlns:p="p" xmlns:a="a"><a:p><a:r><a:t>` + text + `</a:t></a:r></a:p></p:sld>`
	}
	doc, err := Extract("deck.pptx", zipped(t, map[string]string{
		"ppt/slides/slide10.xml": slide("Ten"),
		"ppt/slides/slide2.xml":  slide("Two"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Units) != 2 || doc.Units[0].Text != "Two\n" || doc.Units[1].Label != "slide 2" {
		t.Fatalf("units = %+v", doc.Units)
	}
}

func TestMarkdownUnitsFollowHeadings(t *testing.T) {
	doc, err := Extract("notes.md", []byte("intro\n# One\na\n## Two\nb\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Units) != 3 || doc.Units[2].Label != "section Two" || doc.Text() != "intro\n# One\na\n## Two\nb\n" {
		t.Fatalf("units = %+v text %q", doc.Units, doc.Text())
	}
}

func TestMalformedFilesReturnErrors(t *testing.T) {
	for _, name := range []string{"broken.pdf", "broken.xlsx", "broken.docx"} {
		if _, err := Extract(name, []byte("not a real file")); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
	if _, err := Extract("image.png", nil); err != ErrUnsupported {
		t.Fatalf("png: %v", err)
	}
}
