package serv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeStoreAdminConfig writes a dev config whose sqlite database lives in a
// temp dir; sqlite paths resolve against the working directory, so the test
// substitutes an absolute path for the DBPATH placeholder.
func writeStoreAdminConfig(t *testing.T, body string) *Config {
	t.Helper()
	dir := t.TempDir()
	body = strings.ReplaceAll(body, "DBPATH", filepath.Join(dir, "app.db"))
	if err := os.WriteFile(filepath.Join(dir, "dev.yml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	conf, err := ReadInConfig(filepath.Join(dir, "dev"))
	if err != nil {
		t.Fatalf("ReadInConfig: %v", err)
	}
	return conf
}

func TestStoreAdminInitCreatesTablesIgnoringAutoInit(t *testing.T) {
	read := func() *Config {
		return writeStoreAdminConfig(t, `
app_name: store-admin
databases:
  main:
    type: sqlite
    path: DBPATH
artifacts:
  enabled: true
  source: main
  auto_init: false
watches:
  enabled: true
tasks:
  enabled: true
threads:
  enabled: true
`)
	}
	conf := read()
	before, err := InspectStore(conf)
	if err != nil {
		t.Fatalf("InspectStore: %v", err)
	}
	if before.Ready() {
		t.Fatalf("fresh store reported ready: %+v", before)
	}
	for _, table := range before.Tables {
		if table.Exists {
			t.Fatalf("table %s exists before init", table.Name)
		}
	}
	if got, want := len(before.Tables), 10; got != want {
		t.Fatalf("expected %d tables, got %d: %+v", want, got, before.Tables)
	}

	after, err := InitStore(conf)
	if err != nil {
		t.Fatalf("InitStore: %v", err)
	}
	if !after.Ready() || after.Managed || after.Source != "main" || after.Type != "sqlite" {
		t.Fatalf("unexpected status after init: %+v", after)
	}

	// Re-running init is a no-op migration.
	if _, err := InitStore(conf); err != nil {
		t.Fatalf("second InitStore: %v", err)
	}
}

func TestStoreAdminManagedStore(t *testing.T) {
	conf := writeStoreAdminConfig(t, `
app_name: store-admin
databases:
  main:
    type: sqlite
    path: DBPATH
`)
	st, err := InitStore(conf)
	if err != nil {
		t.Fatalf("InitStore: %v", err)
	}
	if !st.Managed || st.Path == "" || !st.Ready() {
		t.Fatalf("expected a ready managed store, got %+v", st)
	}
	if _, err := os.Stat(st.Path); err != nil {
		t.Fatalf("managed store file: %v", err)
	}
}

func TestStoreAdminDisabled(t *testing.T) {
	if _, err := InspectStore(&Config{}); err == nil {
		t.Fatal("expected an error when the store is disabled")
	}
}
