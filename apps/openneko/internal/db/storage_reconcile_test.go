package db

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeStorageConn struct {
	database string
	provider string
	recorded *string
	actual   *string
	contract *int
	exec     []string
}

func (f *fakeStorageConn) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.exec = append(f.exec, sql)
	if strings.HasPrefix(sql, "ALTER DATABASE ") {
		f.recorded = f.actual
	}
	if strings.Contains(sql, "UPDATE pg_catalog.pg_database") {
		version := args[0].(string)
		f.recorded = &version
	}
	if strings.Contains(sql, "INSERT INTO openneko_internal.storage_contract") {
		version := args[0].(int)
		f.contract = &version
	}
	return pgconn.CommandTag{}, nil
}

func TestReconcileStorageInitializesMissingLegacyCollationVersion(t *testing.T) {
	c := &fakeStorageConn{
		database: "neko",
		provider: "c",
		recorded: nil,
		actual:   storageString(StorageCollationVersion),
	}
	repaired, err := ReconcileStorage(context.Background(), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !repaired {
		t.Fatal("expected storage repair")
	}
	joined := strings.Join(c.exec, "\n")
	if !strings.Contains(joined, "UPDATE pg_catalog.pg_database") {
		t.Fatalf("missing legacy collation initialization:\n%s", joined)
	}
	if strings.Contains(joined, " REFRESH COLLATION VERSION") {
		t.Fatalf("PostgreSQL rejects NULL-to-version refreshes:\n%s", joined)
	}
}

func (f *fakeStorageConn) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	if strings.Contains(sql, "pg_database_collation_actual_version") {
		return fakeStorageRow{scan: func(dest ...any) error {
			*dest[0].(*string) = f.database
			*dest[1].(*string) = f.provider
			*dest[2].(**string) = f.recorded
			*dest[3].(**string) = f.actual
			return nil
		}}
	}
	return fakeStorageRow{scan: func(dest ...any) error {
		if f.contract == nil {
			return pgx.ErrNoRows
		}
		*dest[0].(*int) = *f.contract
		return nil
	}}
}

type fakeStorageRow struct {
	scan func(dest ...any) error
}

func (r fakeStorageRow) Scan(dest ...any) error { return r.scan(dest...) }

func storageString(value string) *string { return &value }
func storageInt(value int) *int          { return &value }

func TestReconcileStorageRepairsAndRecordsContract(t *testing.T) {
	c := &fakeStorageConn{
		database: "tenant-db",
		provider: "c",
		recorded: storageString("1.2.3"),
		actual:   storageString(StorageCollationVersion),
	}
	repaired, err := ReconcileStorage(context.Background(), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !repaired {
		t.Fatal("expected storage repair")
	}
	if c.contract == nil || *c.contract != StorageContractVersion {
		t.Fatalf("contract marker = %v, want %d", c.contract, StorageContractVersion)
	}
	joined := strings.Join(c.exec, "\n")
	for _, want := range []string{
		`REINDEX DATABASE "tenant-db"`,
		`ALTER DATABASE "tenant-db" REFRESH COLLATION VERSION`,
		"INSERT INTO openneko_internal.storage_contract",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("executed SQL does not contain %q:\n%s", want, joined)
		}
	}
}

func TestReconcileStorageQuotesActualDatabaseIdentifier(t *testing.T) {
	c := &fakeStorageConn{
		database: `tenant "one"`,
		provider: "c",
		recorded: storageString(StorageCollationVersion),
		actual:   storageString(StorageCollationVersion),
	}
	if _, err := ReconcileStorage(context.Background(), c, nil); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(c.exec, "\n")
	if !strings.Contains(joined, `REINDEX DATABASE "tenant ""one"""`) {
		t.Fatalf("database identifier was not safely quoted:\n%s", joined)
	}
}

func TestReconcileStorageNoOpsOnlyWhenMarkerAndCollationMatch(t *testing.T) {
	c := &fakeStorageConn{
		database: "neko",
		provider: "c",
		recorded: storageString(StorageCollationVersion),
		actual:   storageString(StorageCollationVersion),
		contract: storageInt(StorageContractVersion),
	}
	repaired, err := ReconcileStorage(context.Background(), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if repaired {
		t.Fatal("matching storage contract should be a no-op")
	}
	for _, sql := range c.exec {
		if strings.HasPrefix(sql, "REINDEX DATABASE ") {
			t.Fatalf("unexpected reindex on matching contract: %s", sql)
		}
	}
}

func TestReconcileStorageRejectsAlpineMuslRuntime(t *testing.T) {
	c := &fakeStorageConn{
		database: "neko",
		provider: "c",
		recorded: storageString(StorageCollationVersion),
		actual:   nil,
	}
	_, err := ReconcileStorage(context.Background(), c, nil)
	if err == nil || !strings.Contains(err.Error(), "Alpine/musl") {
		t.Fatalf("error = %v, want enforced glibc failure", err)
	}
	for _, sql := range c.exec {
		if strings.HasPrefix(sql, "REINDEX DATABASE ") {
			t.Fatalf("unsupported runtime must fail before reindex: %s", sql)
		}
	}
}

func TestReconcileStorageRejectsUnexpectedCollationVersion(t *testing.T) {
	c := &fakeStorageConn{
		database: "neko",
		provider: "c",
		recorded: storageString(StorageCollationVersion),
		actual:   storageString("9.99"),
	}
	_, err := ReconcileStorage(context.Background(), c, nil)
	if err == nil || !strings.Contains(err.Error(), `requires "2.36"`) {
		t.Fatalf("error = %v, want storage ABI mismatch", err)
	}
}

func TestReconcileStorageRefusesContractDowngrade(t *testing.T) {
	c := &fakeStorageConn{
		database: "neko",
		provider: "c",
		recorded: storageString(StorageCollationVersion),
		actual:   storageString(StorageCollationVersion),
		contract: storageInt(StorageContractVersion + 1),
	}
	_, err := ReconcileStorage(context.Background(), c, nil)
	if err == nil || !strings.Contains(err.Error(), "newer storage contract") {
		t.Fatalf("error = %v, want downgrade refusal", err)
	}
}

func TestNullableString(t *testing.T) {
	if got := nullableString(nil); got != "<none>" {
		t.Fatalf("nullableString(nil) = %q", got)
	}
	if got := nullableString(storageString("x")); got != `"x"` {
		t.Fatalf("nullableString(x) = %q", got)
	}
}
