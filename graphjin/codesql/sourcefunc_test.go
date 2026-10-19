package codesql

import (
	"database/sql/driver"
	"os"
	"path/filepath"
	"testing"
)

func TestCodeSQLSourceIndexedContent(t *testing.T) {
	const content = "// café\npackage main\n\nfunc Example() {}\n"
	for _, tc := range []struct {
		name        string
		start, end  int64
		withContext int64
		want        string
		wantError   bool
	}{
		{name: "whole block", end: int64(len(content)), want: content},
		{name: "symbol", start: 23, end: 40, want: "func Example() {}"},
		{name: "context", start: 23, end: 40, withContext: 1, want: "\npackage main\n\nfunc Example() {}\n"},
		{name: "negative start", start: -1, wantError: true},
		{name: "reversed range", start: 2, end: 1, wantError: true},
		{name: "past end", end: int64(len(content)) + 1, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := codeSQLSourceFunc(nil, []driver.Value{[]byte(content), tc.start, tc.end, tc.withContext})
			if tc.wantError {
				if err == nil {
					t.Fatal("expected invalid byte range error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCodeSQLSourcePhysicalFile(t *testing.T) {
	// A real filename resembling a virtual path still takes the file-read path.
	path := filepath.Join(t.TempDir(), "README.md#fence-3.go")
	const content = "package main\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := codeSQLSourceFunc(nil, []driver.Value{path, int64(0), int64(len(content)), int64(0)})
	if err != nil {
		t.Fatal(err)
	}
	if got != content {
		t.Fatalf("got %q, want %q", got, content)
	}
}
