package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// fakeConn satisfies Conn for unit tests. It records executed SQL and answers
// migration-table queries from in-memory state.
type fakeConn struct {
	exec         []string
	execArgs     [][]any
	applied      map[string]bool
	orgRegclass  *uint32 // non-nil → public.organization "exists"
	wantMigStart bool
}

func (f *fakeConn) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.exec = append(f.exec, sql)
	f.execArgs = append(f.execArgs, args)
	if strings.Contains(sql, "INSERT INTO schema_migrations") && len(args) == 1 {
		if name, ok := args[0].(string); ok {
			if f.applied == nil {
				f.applied = map[string]bool{}
			}
			f.applied[name] = true
		}
	}
	return pgconn.CommandTag{}, nil
}

func (f *fakeConn) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "FROM schema_migrations") {
		names := []string{}
		for n := range f.applied {
			names = append(names, n)
		}
		return &fakeRows{names: names}, nil
	}
	return &fakeRows{}, nil
}

func (f *fakeConn) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return &fakeRow{regclass: f.orgRegclass}
}

func (f *fakeConn) Begin(_ context.Context) (pgx.Tx, error) {
	return &fakeTx{parent: f}, nil
}

func (f *fakeConn) Close(_ context.Context) error { return nil }

type fakeRow struct {
	regclass *uint32
}

func (r *fakeRow) Scan(dest ...any) error {
	if len(dest) != 1 {
		return errors.New("fakeRow Scan expects 1 dest")
	}
	switch d := dest[0].(type) {
	case **uint32:
		*d = r.regclass
		return nil
	default:
		return errors.New("fakeRow Scan: unsupported dest type")
	}
}

type fakeRows struct {
	names []string
	idx   int
}

func (r *fakeRows) Close()                                       {}
func (r *fakeRows) Err() error                                   { return nil }
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) RawValues() [][]byte                          { return nil }
func (r *fakeRows) Values() ([]any, error)                       { return nil, nil }
func (r *fakeRows) Conn() *pgx.Conn                              { return nil }
func (r *fakeRows) Next() bool {
	if r.idx >= len(r.names) {
		return false
	}
	r.idx++
	return true
}
func (r *fakeRows) Scan(dest ...any) error {
	if len(dest) != 1 {
		return errors.New("fakeRows Scan expects 1 dest")
	}
	p, ok := dest[0].(*string)
	if !ok {
		return errors.New("fakeRows Scan: unsupported dest")
	}
	*p = r.names[r.idx-1]
	return nil
}

type fakeTx struct {
	parent *fakeConn
}

func (t *fakeTx) Begin(_ context.Context) (pgx.Tx, error) { return t, nil }
func (t *fakeTx) Commit(_ context.Context) error          { return nil }
func (t *fakeTx) Rollback(_ context.Context) error        { return nil }
func (t *fakeTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (t *fakeTx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults { return nil }
func (t *fakeTx) LargeObjects() pgx.LargeObjects                             { return pgx.LargeObjects{} }
func (t *fakeTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (t *fakeTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return t.parent.Exec(ctx, sql, args...)
}
func (t *fakeTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return t.parent.Query(ctx, sql, args...)
}
func (t *fakeTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return t.parent.QueryRow(ctx, sql, args...)
}
func (t *fakeTx) Conn() *pgx.Conn { return nil }

func TestApplyFreshDatabase(t *testing.T) {
	mfs := fstest.MapFS{
		"migrations/0001_init.sql":   {Data: []byte("CREATE TABLE x (id int);")},
		"migrations/0002_second.sql": {Data: []byte("CREATE TABLE y (id int);")},
	}
	c := &fakeConn{}
	mig := &Migrator{FS: mfs, Dir: "migrations"}
	ran, err := mig.Apply(context.Background(), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ran != 2 {
		t.Fatalf("expected 2 migrations, got %d", ran)
	}
	if !c.applied["0001_init.sql"] || !c.applied["0002_second.sql"] {
		t.Fatalf("expected both applied, got %v", c.applied)
	}
}

func TestApplyExistingSchemaBootstrap(t *testing.T) {
	mfs := fstest.MapFS{
		"migrations/0001_init.sql":   {Data: []byte("CREATE TABLE x (id int);")},
		"migrations/0002_second.sql": {Data: []byte("CREATE TABLE y (id int);")},
	}
	oid := uint32(12345) // public.organization "exists"
	c := &fakeConn{orgRegclass: &oid}
	mig := &Migrator{FS: mfs, Dir: "migrations"}
	ran, err := mig.Apply(context.Background(), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ran != 0 {
		t.Fatalf("bootstrap should have marked all applied, ran=%d", ran)
	}
	if !c.applied["0001_init.sql"] || !c.applied["0002_second.sql"] {
		t.Fatalf("expected bootstrap insertion: %v", c.applied)
	}
}

func TestApplyAlreadyApplied(t *testing.T) {
	mfs := fstest.MapFS{
		"migrations/0001_init.sql": {Data: []byte("noop;")},
		"migrations/0002_more.sql": {Data: []byte("noop;")},
	}
	c := &fakeConn{applied: map[string]bool{"0001_init.sql": true}}
	mig := &Migrator{FS: mfs, Dir: "migrations"}
	ran, err := mig.Apply(context.Background(), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ran != 1 {
		t.Fatalf("expected 1 pending, got %d", ran)
	}
}

func TestDSNDefaults(t *testing.T) {
	got := ConnConfig{}.DSN()
	want := "host=localhost port=5432 user=neko password=secret dbname=neko sslmode=disable"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDSNOverrides(t *testing.T) {
	got := ConnConfig{Host: "h", Port: 9, User: "u", Password: "p", Database: "d", SSLMode: "require"}.DSN()
	want := "host=h port=9 user=u password=p dbname=d sslmode=require"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestApplyAcquiresAndReleasesAdvisoryLock(t *testing.T) {
	mfs := fstest.MapFS{
		"migrations/0001_init.sql": {Data: []byte("noop;")},
	}
	c := &fakeConn{}
	mig := &Migrator{FS: mfs, Dir: "migrations"}
	if _, err := mig.Apply(context.Background(), c, nil); err != nil {
		t.Fatal(err)
	}
	// First exec must be the lock; some later exec must be the unlock.
	if len(c.exec) == 0 || !strings.Contains(c.exec[0], "pg_advisory_lock") {
		t.Fatalf("expected first exec to acquire pg_advisory_lock, got: %v", c.exec)
	}
	unlocked := false
	for _, s := range c.exec {
		if strings.Contains(s, "pg_advisory_unlock") {
			unlocked = true
			break
		}
	}
	if !unlocked {
		t.Fatalf("expected pg_advisory_unlock to be called, got: %v", c.exec)
	}
}

func TestShouldRunInTransaction(t *testing.T) {
	cases := map[string]bool{
		"0001_init.sql": true,
		"0016_reindex_after_pgvector_swap_no_tx.sql": false,
		"weird_no_tx.sql": false,
		"_no_tx.sql":      false,
		"":                true,
		"plain.sql":       true,
	}
	for name, want := range cases {
		if got := shouldRunInTransaction(name); got != want {
			t.Errorf("shouldRunInTransaction(%q)=%v want %v", name, got, want)
		}
	}
}
