// Package filecontent turns files in a GraphJin file source into content an
// agent can use without splitting the file up: plain text with page, slide
// and sheet markers, one search unit per natural part of the file, and every
// spreadsheet or CSV as Parquet tables DuckDB can query.
//
// Everything here is pure Go, so the server binary stays static.
package filecontent

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ledongthuc/pdf"
	"github.com/xuri/excelize/v2"
)

// Kind is the shape of an extracted file.
type Kind string

const (
	KindText        Kind = "text"
	KindPDF         Kind = "pdf"
	KindDocument    Kind = "document"
	KindSlides      Kind = "slides"
	KindSpreadsheet Kind = "spreadsheet"
	KindTable       Kind = "table"
)

// Unit is one natural part of a file: a page, slide, sheet or section. Units
// are what search finds; the file is still read whole or by unit range.
type Unit struct {
	Ordinal int    // 1-based position in the file
	Label   string // "page 3", "slide 2", "sheet Sales", "section Booking"
	Text    string
}

// CellType says how a spreadsheet cell's Value is written.
type CellType byte

const (
	CellEmpty  CellType = 0
	CellString CellType = 's'
	CellNumber CellType = 'n' // Value is a plain decimal, no formatting
	CellDate   CellType = 'd' // Value is YYYY-MM-DD or YYYY-MM-DD HH:MM:SS
	CellBool   CellType = 'b'
)

// Cell is one spreadsheet value.
type Cell struct {
	Type  CellType
	Value string
}

// Sheet is one worksheet or CSV file as a grid of cells.
type Sheet struct {
	Name          string
	Hidden        bool
	HiddenColumns []string
	Rows          [][]Cell
}

// Document is everything extracted from one file.
type Document struct {
	Kind   Kind
	Units  []Unit
	Sheets []Sheet
}

// ErrUnsupported means the file type has no extractor.
var ErrUnsupported = errors.New("filecontent: unsupported file type")

// MaxUnitChars caps one unit's text. A sheet larger than this is still fully
// queryable through its Parquet tables.
const MaxUnitChars = 2 << 20

// Supported reports whether name has an extractor.
func Supported(name string) bool {
	_, ok := extractors[strings.ToLower(path.Ext(name))]
	return ok
}

var extractors = map[string]func([]byte) (*Document, error){
	".txt":      extractText,
	".md":       extractMarkdown,
	".markdown": extractMarkdown,
	".csv":      func(b []byte) (*Document, error) { return extractDelimited(b, ',') },
	".tsv":      func(b []byte) (*Document, error) { return extractDelimited(b, '\t') },
	".pdf":      extractPDF,
	".docx":     extractDOCX,
	".pptx":     extractPPTX,
	".xlsx":     extractXLSX,
	".xlsm":     extractXLSX,
}

// Extract reads one file. Malformed files return an error, never a panic.
func Extract(name string, data []byte) (doc *Document, err error) {
	fn, ok := extractors[strings.ToLower(path.Ext(name))]
	if !ok {
		return nil, ErrUnsupported
	}
	defer func() {
		if r := recover(); r != nil {
			doc, err = nil, fmt.Errorf("filecontent: %s: %v", name, r)
		}
	}()
	return fn(data)
}

// Text renders the whole file with its markers: [page n], [slide n],
// [sheet Name] with rows as r12: a | b, or the text itself.
func (d *Document) Text() string {
	if d.Kind == KindText && len(d.Units) > 0 {
		var b strings.Builder
		for _, u := range d.Units {
			b.WriteString(u.Text)
		}
		return b.String()
	}
	var b strings.Builder
	for i, u := range d.Units {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[" + u.Label + "]\n")
		b.WriteString(u.Text)
	}
	return b.String()
}

// ---- text ---------------------------------------------------------------

func extractText(b []byte) (*Document, error) {
	return &Document{Kind: KindText, Units: blockUnits(string(b))}, nil
}

var markdownHeading = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)

// Markdown units follow its headings, so search can point at a section.
func extractMarkdown(b []byte) (*Document, error) {
	s := string(b)
	locs := markdownHeading.FindAllStringSubmatchIndex(s, -1)
	if len(locs) == 0 {
		return extractText(b)
	}
	var units []Unit
	if locs[0][0] > 0 && strings.TrimSpace(s[:locs[0][0]]) != "" {
		units = append(units, Unit{Label: "section start", Text: s[:locs[0][0]]})
	}
	for i, loc := range locs {
		end := len(s)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		units = append(units, Unit{Label: "section " + strings.TrimSpace(s[loc[2]:loc[3]]), Text: s[loc[0]:end]})
	}
	return &Document{Kind: KindText, Units: number(units)}, nil
}

// blockUnits keeps short text as one unit and cuts long text at blank lines
// near every 8,000 characters, so search can still point near a match.
func blockUnits(s string) []Unit {
	const target = 8000
	if len(s) <= target {
		return []Unit{{Ordinal: 1, Label: "lines 1-" + strconv.Itoa(strings.Count(s, "\n")+1), Text: s}}
	}
	var units []Unit
	line := 1
	for len(s) > 0 {
		cut := len(s)
		if cut > target {
			if i := strings.LastIndex(s[:target], "\n\n"); i > target/2 {
				cut = i + 2
			} else if i := strings.LastIndex(s[:target], "\n"); i > 0 {
				cut = i + 1
			} else {
				cut = target
			}
		}
		part := s[:cut]
		lines := strings.Count(part, "\n")
		units = append(units, Unit{Label: fmt.Sprintf("lines %d-%d", line, line+max(lines-1, 0)), Text: part})
		line += lines
		s = s[cut:]
	}
	return number(units)
}

func number(units []Unit) []Unit {
	for i := range units {
		units[i].Ordinal = i + 1
	}
	return units
}

// ---- delimited ----------------------------------------------------------

func extractDelimited(b []byte, sep rune) (*Document, error) {
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))))
	r.Comma = sep
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("filecontent: delimited: %w", err)
	}
	sheet := Sheet{Name: "data"}
	for _, rec := range records {
		row := make([]Cell, len(rec))
		for i, v := range rec {
			row[i] = typedCell(v)
		}
		sheet.Rows = append(sheet.Rows, row)
	}
	return &Document{Kind: KindTable, Sheets: []Sheet{sheet}, Units: []Unit{{Ordinal: 1, Label: "sheet data", Text: renderSheet(sheet)}}}, nil
}

var plainNumber = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

func typedCell(v string) Cell {
	v = strings.TrimSpace(v)
	switch {
	case v == "":
		return Cell{}
	case plainNumber.MatchString(v):
		return Cell{Type: CellNumber, Value: v}
	}
	return Cell{Type: CellString, Value: v}
}

// ---- PDF ----------------------------------------------------------------

func extractPDF(b []byte) (*Document, error) {
	r, err := pdf.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, fmt.Errorf("filecontent: pdf: %w", err)
	}
	doc := &Document{Kind: KindPDF}
	for i := 1; i <= r.NumPage(); i++ {
		p := r.Page(i)
		var text strings.Builder
		if !p.V.IsNull() {
			rows, err := p.GetTextByRow()
			if err == nil {
				for _, row := range rows {
					for _, w := range row.Content {
						text.WriteString(w.S)
					}
					text.WriteString("\n")
				}
			}
		}
		doc.Units = append(doc.Units, Unit{Ordinal: i, Label: fmt.Sprintf("page %d", i), Text: text.String()})
	}
	return doc, nil
}

// HasText reports whether extraction found readable text. A scanned PDF has
// pages but no text; it needs OCR, which this package does not do.
func (d *Document) HasText() bool {
	for _, u := range d.Units {
		if strings.TrimSpace(u.Text) != "" {
			return true
		}
	}
	return len(d.Sheets) > 0
}

// ---- DOCX / PPTX ----------------------------------------------------------

func zipFile(zr *zip.Reader, name string) ([]byte, error) {
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(io.LimitReader(rc, 256<<20))
		}
	}
	return nil, fmt.Errorf("filecontent: %s missing", name)
}

// officeText walks Office XML, writing paragraphs as lines and table rows as
// cells joined with " | ". textTag, paraTag, rowTag and cellTag are local
// names (w:t/w:p/w:tr/w:tc in Word, a:t/a:p/a:tr/a:tc in slides). onPara is
// called with each finished paragraph's text and style.
func officeText(data []byte, textTag, paraTag, rowTag, cellTag string, onPara func(text, style string)) error {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var para strings.Builder
	var style string
	var cells []string
	inRow, inText, rowDepth := false, false, 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("filecontent: xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case textTag:
				inText = true
			case rowTag:
				if !inRow {
					inRow, cells, rowDepth = true, nil, 0
				}
				rowDepth++
			case cellTag:
				if inRow {
					cells = append(cells, "")
				}
			case "pStyle":
				for _, a := range t.Attr {
					if a.Name.Local == "val" {
						style = a.Value
					}
				}
			case "tab":
				para.WriteString("\t")
			case "br":
				para.WriteString("\n")
			}
		case xml.CharData:
			if inText {
				if inRow && len(cells) > 0 {
					cells[len(cells)-1] += string(t)
				} else {
					para.WriteString(string(t))
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case textTag:
				inText = false
			case paraTag:
				if inRow && len(cells) > 0 {
					cells[len(cells)-1] += " "
				} else if s := strings.TrimSpace(para.String()); s != "" {
					onPara(s, style)
				}
				para.Reset()
				style = ""
			case rowTag:
				rowDepth--
				if rowDepth == 0 && inRow {
					inRow = false
					for i := range cells {
						cells[i] = strings.TrimSpace(cells[i])
					}
					onPara(strings.Join(cells, " | "), "")
				}
			}
		}
	}
}

func extractDOCX(b []byte) (*Document, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, fmt.Errorf("filecontent: docx: %w", err)
	}
	body, err := zipFile(zr, "word/document.xml")
	if err != nil {
		return nil, err
	}
	// Units follow the document's headings, so search can point at a section.
	var units []Unit
	cur := Unit{Label: "section start"}
	err = officeText(body, "t", "p", "tr", "tc", func(text, style string) {
		heading := strings.HasPrefix(strings.ToLower(style), "heading") || strings.EqualFold(style, "title")
		if heading && strings.TrimSpace(cur.Text) != "" {
			units = append(units, cur)
			cur = Unit{}
		}
		if heading {
			cur.Label = "section " + text
		}
		cur.Text += text + "\n"
	})
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cur.Text) != "" {
		units = append(units, cur)
	}
	return &Document{Kind: KindDocument, Units: number(units)}, nil
}

var slideName = regexp.MustCompile(`^ppt/slides/slide(\d+)\.xml$`)

func extractPPTX(b []byte) (*Document, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, fmt.Errorf("filecontent: pptx: %w", err)
	}
	type slide struct {
		n    int
		name string
	}
	var slides []slide
	for _, f := range zr.File {
		if m := slideName.FindStringSubmatch(f.Name); m != nil {
			n, _ := strconv.Atoi(m[1])
			slides = append(slides, slide{n, f.Name})
		}
	}
	sort.Slice(slides, func(i, j int) bool { return slides[i].n < slides[j].n })
	doc := &Document{Kind: KindSlides}
	for i, s := range slides {
		data, err := zipFile(zr, s.name)
		if err != nil {
			return nil, err
		}
		var text strings.Builder
		if err := officeText(data, "t", "p", "tr", "tc", func(t, _ string) { text.WriteString(t + "\n") }); err != nil {
			return nil, err
		}
		doc.Units = append(doc.Units, Unit{Ordinal: i + 1, Label: fmt.Sprintf("slide %d", i+1), Text: text.String()})
	}
	return doc, nil
}

// ---- XLSX ---------------------------------------------------------------

func extractXLSX(b []byte) (*Document, error) {
	f, err := excelize.OpenReader(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("filecontent: xlsx: %w", err)
	}
	defer f.Close()
	doc := &Document{Kind: KindSpreadsheet}
	date1904 := false
	if props, err := f.GetWorkbookProps(); err == nil && props.Date1904 != nil {
		date1904 = *props.Date1904
	}
	for i, name := range f.GetSheetList() {
		sheet, err := readSheet(f, name, date1904)
		if err != nil {
			return nil, err
		}
		doc.Sheets = append(doc.Sheets, sheet)
		doc.Units = append(doc.Units, Unit{Ordinal: i + 1, Label: "sheet " + name, Text: renderSheet(sheet)})
	}
	return doc, nil
}

func readSheet(f *excelize.File, name string, date1904 bool) (Sheet, error) {
	sheet := Sheet{Name: name}
	if visible, err := f.GetSheetVisible(name); err == nil {
		sheet.Hidden = !visible
	}
	shown, err := f.GetRows(name)
	if err != nil {
		return sheet, fmt.Errorf("filecontent: sheet %q: %w", name, err)
	}
	raw, err := f.GetRows(name, excelize.Options{RawCellValue: true})
	if err != nil {
		return sheet, fmt.Errorf("filecontent: sheet %q: %w", name, err)
	}
	width := 0
	for r := range raw {
		width = max(width, len(raw[r]))
		row := make([]Cell, len(raw[r]))
		for c, v := range raw[r] {
			display := ""
			if r < len(shown) && c < len(shown[r]) {
				display = shown[r][c]
			}
			row[c] = sheetCell(f, name, r, c, v, display, date1904)
		}
		sheet.Rows = append(sheet.Rows, row)
	}
	for c := 1; c <= width; c++ {
		col, _ := excelize.ColumnNumberToName(c)
		if visible, err := f.GetColVisible(name, col); err == nil && !visible {
			sheet.HiddenColumns = append(sheet.HiddenColumns, col)
		}
	}
	return sheet, nil
}

// sheetCell keeps numbers unformatted and turns date-formatted serial numbers
// into ISO dates, so SQL compares real values instead of display strings.
func sheetCell(f *excelize.File, sheet string, r, c int, raw, display string, date1904 bool) Cell {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Cell{}
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		if raw == "TRUE" || raw == "FALSE" {
			return Cell{Type: CellBool, Value: strings.ToLower(raw)}
		}
		return Cell{Type: CellString, Value: strings.TrimSpace(display)}
	}
	if display != raw && isDateCell(f, sheet, r, c) {
		if t, err := excelize.ExcelDateToTime(n, date1904); err == nil {
			if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 {
				return Cell{Type: CellDate, Value: t.Format(time.DateOnly)}
			}
			return Cell{Type: CellDate, Value: t.Format(time.DateTime)}
		}
	}
	if math.IsInf(n, 0) || math.IsNaN(n) {
		return Cell{Type: CellString, Value: display}
	}
	return Cell{Type: CellNumber, Value: strconv.FormatFloat(n, 'f', -1, 64)}
}

var (
	dateFormat     = regexp.MustCompile(`(?i)(^|[^"\\])(d{1,4}|m{1,5}|y{2,4}|h{1,2}|s{1,2})`)
	formatBrackets = regexp.MustCompile(`\[[^\]]*\]`)
)

func isDateCell(f *excelize.File, sheet string, r, c int) bool {
	axis, err := excelize.CoordinatesToCellName(c+1, r+1)
	if err != nil {
		return false
	}
	id, err := f.GetCellStyle(sheet, axis)
	if err != nil {
		return false
	}
	style, err := f.GetStyle(id)
	if err != nil || style == nil {
		return false
	}
	if (style.NumFmt >= 14 && style.NumFmt <= 22) || (style.NumFmt >= 45 && style.NumFmt <= 47) {
		return true
	}
	if style.CustomNumFmt != nil {
		// Strip bracketed parts like [Red] or [$-409] before looking for date codes.
		code := formatBrackets.ReplaceAllString(*style.CustomNumFmt, "")
		return dateFormat.MatchString(code) && !strings.ContainsAny(code, "0#?")
	}
	return false
}

// renderSheet writes a sheet as lines "r12: a | b | c", skipping empty rows,
// with a note of hidden columns.
func renderSheet(s Sheet) string {
	var b strings.Builder
	if len(s.HiddenColumns) > 0 {
		b.WriteString("hidden columns: " + strings.Join(s.HiddenColumns, ", ") + "\n")
	}
	for i, row := range s.Rows {
		empty := true
		for _, c := range row {
			if c.Type != CellEmpty {
				empty = false
				break
			}
		}
		if empty {
			continue
		}
		vals := make([]string, len(row))
		for j, c := range row {
			vals[j] = c.Value
		}
		line := "r" + strconv.Itoa(i+1) + ": " + strings.Join(vals, " | ") + "\n"
		if b.Len()+len(line) > MaxUnitChars {
			b.WriteString("[sheet cut here; query it with SQL]\n")
			break
		}
		b.WriteString(line)
	}
	return b.String()
}
