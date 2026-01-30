package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/serv/v3"
)

func TestOutputSQL_CreateTable(t *testing.T) {
	ops := []core.SchemaOperation{
		{Type: "create_table", Table: "users", SQL: "CREATE TABLE users (id INT);", Danger: false},
	}

	output := captureStdout(func() {
		outputSQL(ops, false)
	})

	if !strings.Contains(output, "-- Create table: users") {
		t.Errorf("expected create table comment, got %q", output)
	}
	if !strings.Contains(output, "CREATE TABLE users (id INT);") {
		t.Errorf("expected CREATE TABLE SQL, got %q", output)
	}
}

func TestOutputSQL_AddColumn(t *testing.T) {
	ops := []core.SchemaOperation{
		{Type: "add_column", Table: "users", Column: "email", SQL: "ALTER TABLE users ADD COLUMN email VARCHAR(255);", Danger: false},
	}

	output := captureStdout(func() {
		outputSQL(ops, false)
	})

	if !strings.Contains(output, "-- Add column: users.email") {
		t.Errorf("expected add column comment, got %q", output)
	}
	if !strings.Contains(output, "ALTER TABLE users ADD COLUMN email VARCHAR(255);") {
		t.Errorf("expected ALTER TABLE SQL, got %q", output)
	}
}

func TestOutputSQL_DestructiveHidden(t *testing.T) {
	ops := []core.SchemaOperation{
		{Type: "drop_table", Table: "old_table", SQL: "DROP TABLE old_table;", Danger: true},
	}

	output := captureStdout(func() {
		outputSQL(ops, false) // showDestructive = false
	})

	if strings.Contains(output, "DROP TABLE") {
		t.Errorf("expected destructive operations to be hidden, got %q", output)
	}
}

func TestOutputSQL_DestructiveShown(t *testing.T) {
	ops := []core.SchemaOperation{
		{Type: "drop_table", Table: "old_table", SQL: "DROP TABLE old_table;", Danger: true},
	}

	output := captureStdout(func() {
		outputSQL(ops, true) // showDestructive = true
	})

	if !strings.Contains(output, "-- DESTRUCTIVE:") {
		t.Errorf("expected DESTRUCTIVE prefix, got %q", output)
	}
	if !strings.Contains(output, "DROP TABLE old_table;") {
		t.Errorf("expected DROP TABLE SQL, got %q", output)
	}
}

func TestOutputSQL_AddIndex(t *testing.T) {
	ops := []core.SchemaOperation{
		{Type: "add_index", Table: "users", Column: "email", SQL: "CREATE INDEX idx_users_email ON users(email);", Danger: false},
	}

	output := captureStdout(func() {
		outputSQL(ops, false)
	})

	if !strings.Contains(output, "-- Add index on: users.email") {
		t.Errorf("expected add index comment, got %q", output)
	}
}

func TestOutputSQL_AddConstraint(t *testing.T) {
	ops := []core.SchemaOperation{
		{Type: "add_constraint", Table: "orders", Column: "user_id", SQL: "ALTER TABLE orders ADD CONSTRAINT fk_user FOREIGN KEY (user_id) REFERENCES users(id);", Danger: false},
	}

	output := captureStdout(func() {
		outputSQL(ops, false)
	})

	if !strings.Contains(output, "-- Add constraint on: orders.user_id") {
		t.Errorf("expected add constraint comment, got %q", output)
	}
}

func TestOutputJSON(t *testing.T) {
	ops := []core.SchemaOperation{
		{Type: "add_column", Table: "users", Column: "email", SQL: "ALTER TABLE...", Danger: false},
		{Type: "drop_table", Table: "old_table", SQL: "DROP TABLE old_table;", Danger: true},
	}

	output := captureStdout(func() {
		outputJSON(ops)
	})

	// Parse JSON to verify structure
	var jsonOps []struct {
		Type        string `json:"type"`
		Table       string `json:"table"`
		Column      string `json:"column,omitempty"`
		SQL         string `json:"sql"`
		Destructive bool   `json:"destructive,omitempty"`
	}

	if err := json.Unmarshal([]byte(output), &jsonOps); err != nil {
		t.Fatalf("failed to parse JSON output: %s", err)
	}

	if len(jsonOps) != 2 {
		t.Errorf("expected 2 operations, got %d", len(jsonOps))
	}

	if jsonOps[0].Type != "add_column" || jsonOps[0].Table != "users" || jsonOps[0].Column != "email" {
		t.Errorf("first operation incorrect: %+v", jsonOps[0])
	}

	if jsonOps[1].Type != "drop_table" || !jsonOps[1].Destructive {
		t.Errorf("second operation incorrect: %+v", jsonOps[1])
	}
}

func TestIsMultiDBMode(t *testing.T) {
	// Save original conf
	origConf := conf

	// Test with nil conf
	conf = nil
	// isMultiDBMode should handle nil gracefully or we need to init conf
	// For safety, let's initialize a minimal conf
	conf = &serv.Config{}
	conf.Databases = nil

	if isMultiDBMode() {
		t.Error("expected false when Databases is nil")
	}

	conf.Databases = map[string]core.DatabaseConfig{}
	if isMultiDBMode() {
		t.Error("expected false when Databases is empty")
	}

	conf.Databases = map[string]core.DatabaseConfig{
		"primary": {Type: "postgres"},
	}
	if !isMultiDBMode() {
		t.Error("expected true when Databases has entries")
	}

	// Restore original conf
	conf = origConf
}

// captureStdout captures stdout output from a function
func captureStdout(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close() //nolint:errcheck
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r) //nolint:errcheck
	return buf.String()
}
