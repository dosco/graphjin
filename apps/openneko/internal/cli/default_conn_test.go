package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConnEnvOverride(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "openneko"), 0o700); err != nil {
		t.Fatal(err)
	}
	config := `{"pg":{"host":"neko-db","port":5432,"password":"rotated"}}`
	if err := os.WriteFile(filepath.Join(dir, "openneko", "config.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("NEKO_PG_HOST", "127.0.0.1")
	t.Setenv("NEKO_PG_PORT", "55432")
	t.Setenv("NEKO_PG_PASSWORD", "")

	if conn, err := defaultConn(); err != nil || conn.Host != "neko-db" || conn.Port != 5432 {
		t.Fatalf("config.json should win without the override: %+v %v", conn, err)
	}
	t.Setenv("OPENNEKO_PG_ENV_OVERRIDE", "1")
	conn, err := defaultConn()
	if err != nil {
		t.Fatal(err)
	}
	if conn.Host != "127.0.0.1" || conn.Port != 55432 || conn.Password != "rotated" {
		t.Fatalf("set env vars should win and the rotated password should stay: %+v", conn)
	}
}

func TestDefaultConnDatabaseURL(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "openneko"), 0o700); err != nil {
		t.Fatal(err)
	}
	config := `{"database_url":"postgres://neko:pw@db.example:6432/neko?sslmode=verify-full","pg":{"host":"neko-db"}}`
	if err := os.WriteFile(filepath.Join(dir, "openneko", "config.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("OPENNEKO_DATABASE_URL_FILE", "")

	conn, err := defaultConn()
	if err != nil || conn.DSN() != "postgres://neko:pw@db.example:6432/neko?sslmode=verify-full" {
		t.Fatalf("database_url should be the connection: %q %v", conn.DSN(), err)
	}

	file := filepath.Join(dir, "database-url")
	if err := os.WriteFile(file, []byte("postgres://neko:file@db.internal/neko\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENNEKO_DATABASE_URL_FILE", file)
	if conn, err := defaultConn(); err != nil || conn.DSN() != "postgres://neko:file@db.internal/neko" {
		t.Fatalf("the URL file should win over config.json: %q %v", conn.DSN(), err)
	}

	if err := os.WriteFile(file, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := defaultConn(); err == nil {
		t.Fatal("an empty URL file should be an error")
	}
}
