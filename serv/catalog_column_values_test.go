package serv

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// Benchmark generation 2028.1 found the agent writing status "closed" against a
// schema whose statuses are open, pending, and resolved. It could not have known:
// the catalog published a placeholder. These tests pin the sampling that fixes it,
// and the guardrails that keep it from publishing anything else.

func TestEnumLikeColumnSelection(t *testing.T) {
	for _, tc := range []struct {
		name, column, columnType string
		array, want              bool
	}{
		{"status text", "status", "text", false, true},
		{"severity varchar", "severity", "varchar(20)", false, true},
		{"account plan", "plan", "TEXT", false, true},
		{"user role", "role", "text", false, true},
		// A name match is not enough: these carry no closed vocabulary worth showing.
		{"numeric type column", "type_id", "integer", false, false},
		{"timestamp named state", "state_changed_at", "timestamp", false, false},
		{"array of statuses", "status", "text", true, false},
		{"free text body", "subject", "text", false, false},
		{"identifier", "id", "integer", false, false},
	} {
		if got := enumLikeColumn(tc.column, tc.columnType, tc.array); got != tc.want {
			t.Errorf("%s: enumLikeColumn(%q,%q,array=%v) = %v, want %v",
				tc.name, tc.column, tc.columnType, tc.array, got, tc.want)
		}
	}
}

func newSampleDB(t *testing.T, ddl string, rows ...string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if _, err := db.Exec(row); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestSampleDistinctValuesReturnsClosedSet(t *testing.T) {
	db := newSampleDB(t, `CREATE TABLE support_tickets (id INTEGER, status TEXT)`,
		`INSERT INTO support_tickets VALUES (1,'open'),(2,'resolved'),(3,'pending'),(4,'open'),(5,NULL)`)

	values, ok := sampleDistinctValues(context.Background(), db, "main", "support_tickets", "status")
	if !ok {
		t.Fatal("a low-cardinality column must yield its value set")
	}
	if strings.Join(values, ",") != "open,pending,resolved" {
		t.Fatalf("values = %v, want sorted open,pending,resolved with NULL excluded", values)
	}
}

// TestSampleDistinctValuesRejectsHighCardinality is the privacy guard: a column
// with more distinct values than the cap is free text or an identifier, and must
// publish nothing rather than a truncated sample.
func TestSampleDistinctValuesRejectsHighCardinality(t *testing.T) {
	db := newSampleDB(t, `CREATE TABLE notes (id INTEGER, category TEXT)`,
		`INSERT INTO notes VALUES (1,'a'),(2,'b'),(3,'c'),(4,'d'),(5,'e'),(6,'f'),(7,'g'),(8,'h'),(9,'i')`)

	if values, ok := sampleDistinctValues(context.Background(), db, "main", "notes", "category"); ok {
		t.Fatalf("expected no values beyond the cardinality cap, got %v", values)
	}
}

// TestSampleDistinctValuesRejectsLongValues keeps prose out of catalog cards even
// when only a few distinct rows exist.
func TestSampleDistinctValuesRejectsLongValues(t *testing.T) {
	db := newSampleDB(t, `CREATE TABLE notes (id INTEGER, kind TEXT)`,
		`INSERT INTO notes VALUES (1,'`+strings.Repeat("x", columnValueMaxLength+1)+`'),(2,'short')`)

	if values, ok := sampleDistinctValues(context.Background(), db, "main", "notes", "kind"); ok {
		t.Fatalf("expected no values when one exceeds the length cap, got %v", values)
	}
}

func TestSampleDistinctValuesSurvivesMissingTable(t *testing.T) {
	db := newSampleDB(t, `CREATE TABLE present (id INTEGER)`)
	if _, ok := sampleDistinctValues(context.Background(), db, "main", "absent", "status"); ok {
		t.Fatal("a failed sample must report not-ok rather than fabricate values")
	}
}

func TestQuoteSQLIdentifierEscapesQuotes(t *testing.T) {
	if got := quoteSQLIdentifier(`we"ird`); got != `"we""ird"` {
		t.Fatalf("quoteSQLIdentifier = %s, want doubled inner quote", got)
	}
	if got := qualifiedSQLName("", "tickets"); got != `"tickets"` {
		t.Fatalf("qualifiedSQLName with no schema = %s", got)
	}
	if got := qualifiedSQLName("app", "tickets"); got != `"app"."tickets"` {
		t.Fatalf("qualifiedSQLName = %s", got)
	}
	// SQLite's implicit schema is not a qualifier worth emitting.
	if got := qualifiedSQLName("main", "tickets"); got != `"tickets"` {
		t.Fatalf("qualifiedSQLName(main) = %s, want unqualified", got)
	}
}
