package hostedemu

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCaptureDuckDBEvents(t *testing.T) {
	seed := writeSeed(t)
	dir := t.TempDir()
	db := sql.OpenDB(NewConnector(Config{
		SeedPath:   seed,
		CaptureDir: dir,
		TestName:   "core capture",
		RunID:      "unit",
		Backend:    BackendDuckDB,
		Fallback:   FallbackStrict,
	}, fakeAdapter{}))
	defer db.Close()

	if _, err := db.ExecContext(context.Background(), "INSERT INTO items VALUES (?)", int64(1)); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(context.Background(), "SELECT id FROM items WHERE id = ?", int64(1))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	data, err := os.ReadFile(filepath.Join(dir, "core_capture.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("capture line count = %d, want 2\n%s", len(lines), data)
	}
	var ev struct {
		RunID         string `json:"run_id"`
		Test          string `json:"test"`
		Seq           int64  `json:"seq"`
		Op            string `json:"op"`
		Backend       string `json:"backend"`
		TranslatedSQL string `json:"translated_sql"`
		Result        string `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.RunID != "unit" || ev.Test != "core capture" || ev.Seq != 2 || ev.Op != "query" || ev.Backend != BackendDuckDB || ev.TranslatedSQL == "" || ev.Result != "rows:1" {
		t.Fatalf("unexpected event: %#v", ev)
	}
}

func TestPlaceholderFallbackRecordsExecutionError(t *testing.T) {
	seed := writeSeed(t)
	dir := t.TempDir()
	db := sql.OpenDB(NewConnector(Config{
		SeedPath:   seed,
		CaptureDir: dir,
		TestName:   "fallback",
		RunID:      "unit",
		Backend:    BackendDuckDB,
		Fallback:   FallbackPlaceholderOnError,
	}, fakeAdapter{}))
	defer db.Close()

	var value int64
	if err := db.QueryRowContext(context.Background(), "SELECT id FROM missing").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != 1 {
		t.Fatalf("fallback value = %d, want 1", value)
	}

	data, err := os.ReadFile(filepath.Join(dir, "fallback.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var ev struct {
		ExecutionError string `json:"execution_error"`
		Result         string `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(data), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.ExecutionError == "" || ev.Result != "rows:1" {
		t.Fatalf("unexpected fallback event: %#v", ev)
	}
}

func TestStrictModeReturnsExecutionError(t *testing.T) {
	seed := writeSeed(t)
	db := sql.OpenDB(NewConnector(Config{
		SeedPath: seed,
		Backend:  BackendDuckDB,
		Fallback: FallbackStrict,
	}, fakeAdapter{}))
	defer db.Close()

	if _, err := db.QueryContext(context.Background(), "SELECT id FROM missing"); err == nil {
		t.Fatal("strict mode query unexpectedly succeeded")
	}
}

func writeSeed(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "seed.sql")
	if err := os.WriteFile(path, []byte("seed"), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

type fakeAdapter struct{}

func (fakeAdapter) Name() string { return "fake" }
func (fakeAdapter) DefaultSeedPath() string {
	return ""
}
func (fakeAdapter) ParseSeed(string) (any, error) { return struct{}{}, nil }
func (fakeAdapter) NewSession(any) Session        { return fakeSession{} }
func (fakeAdapter) TranslateSetup(string, any) ([]string, error) {
	return []string{"CREATE TABLE items (id BIGINT)"}, nil
}
func (fakeAdapter) TranslateDiscoveryQuery(sql string, args []driver.NamedValue, _ any) (string, []driver.NamedValue, error) {
	return sql, args, nil
}
func (fakeAdapter) TranslateDiscoveryExec(sql string, args []driver.NamedValue, _ any) ([]string, []driver.NamedValue, error) {
	return []string{sql}, args, nil
}
func (fakeAdapter) TranslateRuntime(sql string, args []driver.NamedValue, _ any) (string, []driver.NamedValue, error) {
	return sql, args, nil
}
func (fakeAdapter) TranslateDirect(sql string, args []driver.NamedValue, _ any) (string, []driver.NamedValue, error) {
	return sql, args, nil
}
func (fakeAdapter) NormalizeIdentifier(identifier string) string { return identifier }
func (fakeAdapter) MapType(sourceType string) string             { return sourceType }
func (fakeAdapter) ClassifyPhase(sql string) string {
	if strings.Contains(strings.ToUpper(sql), "RUNTIME") {
		return "runtime"
	}
	return "direct"
}

type fakeSession struct{}

func (fakeSession) PlaceholderQuery(string) (*Rows, string, error) {
	return NewRows([]string{"id"}, []driver.Value{int64(1)}), "rows:1", nil
}
