package sdata

import (
	"fmt"
	"strings"
	"testing"
)

func ticketTable() *DBTable {
	t := NewDBTable("public", "support_tickets", "", []DBColumn{
		{Name: "id", Type: "bigint"},
		{Name: "subject", Type: "text"},
		{Name: "status", Type: "text"},
		{Name: "severity", Type: "text"},
		{Name: "sla_due_at", Type: "timestamp"},
	})
	return &t
}

// The names here are the ones recorded runs actually guessed. A caller told
// only "not found" re-guesses; told what the table has, it can pick.
func TestGetColumnNamesWhatTheTableHas(t *testing.T) {
	ti := ticketTable()
	for _, want := range []string{"priority", "urgency"} {
		_, err := ti.GetColumn(want)
		if err == nil {
			t.Fatalf("GetColumn(%q) should miss on this table", want)
		}
		if !strings.Contains(err.Error(), "available columns:") {
			t.Fatalf("error must list the real columns: %v", err)
		}
		if !strings.Contains(err.Error(), "severity") {
			t.Fatalf("the column the caller meant must appear: %v", err)
		}
		// The original message has to survive; it is what names the failure.
		if !strings.Contains(err.Error(), "column: 'support_tickets."+want+"' not found") {
			t.Fatalf("original not-found message lost: %v", err)
		}
	}
}

// A near miss gets the answer, not just the haystack.
func TestGetColumnSuggestsANearMiss(t *testing.T) {
	ti := ticketTable()
	_, err := ti.GetColumn("sla_due")
	if err == nil || !strings.Contains(err.Error(), `did you mean "sla_due_at"`) {
		t.Fatalf("error = %v, want a sla_due_at suggestion", err)
	}
}

// A hit stays a hit.
func TestGetColumnStillResolves(t *testing.T) {
	if _, err := ticketTable().GetColumn("severity"); err != nil {
		t.Fatalf("an existing column must resolve: %v", err)
	}
}

// A wide table must not bury the answer in its own schema.
func TestGetColumnCapsTheColumnList(t *testing.T) {
	cols := make([]DBColumn, 0, 60)
	for i := 0; i < 60; i++ {
		cols = append(cols, DBColumn{Name: "c" + string(rune('a'+i%26)) + string(rune('0'+i/26)), Type: "text"})
	}
	wide := NewDBTable("public", "wide", "", cols)
	_, err := (&wide).GetColumn("nope")
	if err == nil {
		t.Fatal("expected a miss")
	}
	if !strings.Contains(err.Error(), "more)") {
		t.Fatalf("a wide table must report a bounded list: %v", err)
	}
	if strings.Count(err.Error(), ",") > maxListedColumns {
		t.Fatalf("column list exceeded the cap: %v", err)
	}
}

// The names below are the ones recorded runs actually produced. A caller told
// only "not a column or a function" has nothing to act on and re-sends: the
// bare message was the most common dead end across both models.
func TestColumnHintNamesTheLikelyColumn(t *testing.T) {
	ti := NewDBTable("public", "support_tickets", "", []DBColumn{
		{Name: "id", Type: "bigint"},
		{Name: "status", Type: "text"},
		{Name: "severity", Type: "text"},
		{Name: "resolution_note", Type: "text"},
		{Name: "resolved_at", Type: "timestamp"},
	})
	for _, want := range []string{"resolution", "resolution_notes"} {
		hint := (&ti).ColumnHint(want)
		if !strings.Contains(hint, `did you mean "resolution_note"`) {
			t.Fatalf("ColumnHint(%q) = %q, want a resolution_note suggestion", want, hint)
		}
	}
	// A near miss on a remote table's column: the case that beat the strong
	// model, which re-sent the same query until its budget ran out.
	health := NewDBTable("public", "account_health", "remote", []DBColumn{
		{Name: "account_id", Type: "bigint"},
		{Name: "executive_owner", Type: "text"},
		{Name: "health", Type: "text"},
		{Name: "open_risk_count", Type: "int"},
	})
	if hint := (&health).ColumnHint("executive_owne"); !strings.Contains(hint, `did you mean "executive_owner"`) {
		t.Fatalf("ColumnHint(executive_owne) = %q", hint)
	}
	// Even with no near miss, the caller learns what the table has.
	if hint := (&health).ColumnHint("totally_unrelated"); !strings.Contains(hint, "available columns:") ||
		!strings.Contains(hint, "open_risk_count") {
		t.Fatalf("a miss with no near match must still list the columns: %q", hint)
	}
}

// A hint is bounded on a wide table. Repeating hundreds of column names on
// every miss costs real tokens and, past the first few dozen, an arbitrary
// prefix of the schema rarely contains the column being looked for.
func TestColumnHintStaysBoundedOnAWideTable(t *testing.T) {
	cols := make([]DBColumn, 0, 500)
	for i := 0; i < 500; i++ {
		cols = append(cols, DBColumn{Name: fmt.Sprintf("field_%03d", i), Type: "text"})
	}
	wide := NewDBTable("public", "wide", "", cols)

	// No near match: the list is the only help available, so it appears, but
	// bounded and honest about what it is not showing.
	miss := (&wide).ColumnHint("totally_unrelated")
	if !strings.Contains(miss, "more)") {
		t.Fatalf("a wide table must report a bounded list: %q", miss)
	}
	if len(miss) > 600 {
		t.Fatalf("hint is %d bytes on a 500-column table; it must stay bounded", len(miss))
	}

	// A near match answers the question outright, so the list is not repeated
	// behind it — the case that would otherwise pay the full cost every time.
	near := (&wide).ColumnHint("field_007")
	if !strings.Contains(near, `did you mean "field_007"`) && !strings.Contains(near, "did you mean") {
		t.Fatalf("a near match must be named: %q", near)
	}
	if strings.Contains(near, "available columns") {
		t.Fatalf("a named suggestion must not also carry the list: %q", near)
	}
	if len(near) > 120 {
		t.Fatalf("a suggestion-only hint should be short, got %d bytes: %q", len(near), near)
	}
}
