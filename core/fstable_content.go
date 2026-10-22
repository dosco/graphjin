package core

import (
	"context"
	"errors"

	"github.com/dosco/graphjin/core/v3/fstable"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// FileContent reads inside the files of filesystem tables: the text of
// PDFs, Office files and sheets, a ranked search over that text, and the
// SQL tables derived from sheets and CSV files. Register one with
// OptionSetFileContent; without it, text is set only for UTF-8 files and
// search, passages and tables are unavailable.
type FileContent interface {
	// Text returns the extracted text of a file, with page, slide and sheet
	// markers. ok is false when the file type has no extractor.
	Text(ctx context.Context, source string, e fstable.Entry, data []byte) (text string, ok bool, err error)

	// Search ranks the files of a source against a query and returns the
	// best first, each with its best matching passages.
	Search(ctx context.Context, source, query string, limit int) ([]FileMatch, error)

	// Tables lists the SQL tables derived from a file.
	Tables(ctx context.Context, source string, e fstable.Entry) ([]FileTable, error)
}

// FileMatch is one file a search found.
type FileMatch struct {
	Key      string        `json:"key"`
	Score    float64       `json:"score"`
	Passages []FilePassage `json:"passages"`
}

// FilePassage is a matching unit of a file: a page, slide, sheet or section.
type FilePassage struct {
	Location string `json:"location"` // e.g. "page 12", "slide 3", "sheet Q4"
	Snippet  string `json:"snippet"`
}

// FileTable is a SQL table derived from a sheet or CSV file.
type FileTable struct {
	Name      string            `json:"name"`
	Sheet     string            `json:"sheet,omitempty"`
	Raw       bool              `json:"raw,omitempty"`
	HeaderRow int               `json:"header_row,omitempty"`
	Rows      int               `json:"rows"`
	Columns   []FileTableColumn `json:"columns"`
}

// FileTableColumn is a column of a FileTable.
type FileTableColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// OptionSetFileContent registers the reader for file contents. See FileContent.
func OptionSetFileContent(fc FileContent) Option {
	return func(s *graphjinEngine) error {
		if fc == nil {
			return errors.New("file content: reader is nil")
		}
		s.fileContent = fc
		return nil
	}
}

// withFileContentColumns adds what file content indexing serves to a file
// table: the passages and tables columns and the search argument.
func withFileContentColumns(cols, args []sdata.DBColumn) ([]sdata.DBColumn, []sdata.DBColumn) {
	if len(cols) == 0 {
		return cols, args
	}
	schema, table := cols[0].Schema, cols[0].Table
	for i := range cols {
		if cols[i].Name == "text" {
			cols[i].Comment = "File text: UTF-8 files as they are; PDF, Word, PowerPoint and Excel files as extracted text with [page n], [slide n] and [sheet name] markers; else null. Selecting it inlines file content"
		}
	}
	cols = append(cols,
		sdata.DBColumn{Name: "passages", Type: "json", Comment: "With search: the file's best matching passages, each {location, snippet}"},
		sdata.DBColumn{Name: "tables", Type: "json", Comment: "SQL tables derived from the file's sheets or CSV rows, each {name, sheet, rows, columns}; query them with the file SQL tool"},
	)
	args = append(args, sdata.DBColumn{Name: "search", Type: "text"})
	for i := range cols {
		cols[i].Schema, cols[i].Table, cols[i].ID = schema, table, int32(i)
	}
	for i := range args {
		args[i].Schema, args[i].Table, args[i].ID = schema, table, int32(i)
	}
	return cols, args
}
