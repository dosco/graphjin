package core

import (
	"errors"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

func TestInsertConflictGetResultEmpty(t *testing.T) {
	qc := &qcode.QCode{
		Roots:   []int32{0},
		Selects: []qcode.Select{{Field: qcode.Field{FieldName: "users"}}},
	}
	tests := []struct {
		json  string
		empty bool
	}{
		{`{"users":[]}`, true},
		{`{"users":null}`, true},
		{`{"users":[{"id":1}]}`, false},
		{`{"other":[]}`, true},
	}
	for _, tt := range tests {
		if got := insertConflictGetResultEmpty([]byte(tt.json), qc); got != tt.empty {
			t.Errorf("insertConflictGetResultEmpty(%s) = %v, want %v", tt.json, got, tt.empty)
		}
	}
}

func TestSQLiteLockError(t *testing.T) {
	if !isSQLiteLockError(errors.New("database table is locked")) || !isSQLiteLockError(errors.New("database is locked")) {
		t.Fatal("expected SQLite lock errors to be retryable")
	}
	if isSQLiteLockError(errors.New("unique constraint failed")) {
		t.Fatal("unrelated SQLite errors must not be retried")
	}
}

func TestInsertConflictGetUnavailableError(t *testing.T) {
	if got := insertConflictGetUnavailableError(&qcode.QCode{}).Error(); !strings.Contains(got, "retryable concurrency error") {
		t.Fatalf("expected retryable concurrency error, got %q", got)
	}
	if got := insertConflictGetUnavailableError(&qcode.QCode{InsertConflictReadFiltered: true}).Error(); !strings.Contains(got, "authorization error") {
		t.Fatalf("expected authorization error, got %q", got)
	}
}
