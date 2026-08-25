package sdata

import (
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
