package serv

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/spf13/afero"
	_ "modernc.org/sqlite"
)

func TestArtifactControlPlaneInitializesAndScopesRowsByUser(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "queries"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "queries", "hello.graphql"), []byte(`query { countries { id } }`), 0o644); err != nil {
		t.Fatal(err)
	}

	dsn := "file:" + filepath.Join(tmp, "app.db")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	autoInit := true
	conf := &Config{Core: core.Config{
		Sources: []core.SourceConfig{
			{Name: "app", Kind: "database", Type: "sqlite", Path: dsn, Default: true},
			{Name: "graphjin", Kind: "graphjin"},
		},
		Artifacts: core.ArtifactsConfig{Enabled: true, Source: "app", AutoInit: &autoInit, GlobalsPath: "."},
	}}
	conf.ConfigPath = tmp
	if err := conf.Core.NormalizeSources(); err != nil {
		t.Fatalf("NormalizeSources: %v", err)
	}
	svc := &graphjinService{
		conf: conf,
		dbs:  map[string]*sql.DB{"app": db},
		fs:   newAferoFS(afero.NewMemMapFs(), "/"),
	}
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	if !sqliteTableExists(t, db, "_graphjin_artifacts") {
		t.Fatal("expected artifact table to be created")
	}
	if sqliteTableExists(t, db, "_graphjin_artifact_revisions") {
		t.Fatal("artifact revisions table should not be created")
	}
	if !sqliteTableExists(t, db, "_graphjin_revisions") {
		t.Fatal("expected shared revisions table to be created")
	}
	startSQLiteArtifactCore(t, svc, db)
	for _, column := range []string{"content_hash", "status"} {
		ok, err := sqliteColumnExists(context.Background(), db, quoteSQLIdent("_graphjin_artifacts"), column)
		if err != nil {
			t.Fatalf("check artifact column %s: %v", column, err)
		}
		if !ok {
			t.Fatalf("expected artifact column %s to be created", column)
		}
	}
	cp := newArtifactControlPlane(svc)
	ctx := context.WithValue(context.Background(), core.IdentityVarsKey, map[string]interface{}{"account_id": "acct_1", "user_id": "user_1"})
	content := "query { plans { id } }"
	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     artifactsRootTable,
		Operation: "insert",
		Input:     map[string]interface{}{"name": "hello", "kind": "query", "content": content},
	}); err != nil {
		t.Fatalf("insert artifact: %v", err)
	}
	if got := sqliteRevision(t, db, "artifacts"); got != 1 {
		t.Fatalf("artifact revision after insert = %d, want 1", got)
	}
	rows, err := cp.artifactRows(ctx)
	if err != nil {
		t.Fatalf("artifactRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("db artifact should override same-name global, got rows=%+v", rows)
	}
	if rows[0]["source"] != "database" || rows[0]["visibility"] != "user" || rows[0]["read_only"] != false {
		t.Fatalf("unexpected artifact row: %+v", rows[0])
	}
	if rows[0]["content_hash"] != hashString(content) || rows[0]["status"] != "approved" {
		t.Fatalf("unexpected artifact hash/status: %+v", rows[0])
	}
	acct1ArtifactID, _ := rows[0]["id"].(string)

	otherCtx := context.WithValue(context.Background(), core.IdentityVarsKey, map[string]interface{}{"account_id": "acct_2", "user_id": "user_2"})
	rows, err = cp.artifactRows(otherCtx)
	if err != nil {
		t.Fatalf("artifactRows other user: %v", err)
	}
	if len(rows) != 1 || rows[0]["source"] != "config" || rows[0]["read_only"] != true {
		t.Fatalf("other user should see read-only global, got rows=%+v", rows)
	}

	if _, err := cp.mutateRow(otherCtx, core.ManagedMutationRoot{
		Table:     artifactsRootTable,
		Operation: "upsert",
		Input: map[string]interface{}{
			"id":      acct1ArtifactID,
			"name":    "hello",
			"kind":    "query",
			"content": "query { leaked { id } }",
		},
	}); err != nil {
		t.Fatalf("cross-user id upsert attempt: %v", err)
	}
	rows, err = cp.artifactRows(ctx)
	if err != nil {
		t.Fatalf("artifactRows original user after cross-user attempt: %v", err)
	}
	if len(rows) != 1 || rows[0]["content"] != "query { plans { id } }" {
		t.Fatalf("cross-user id must not overwrite original artifact, got rows=%+v", rows)
	}
	rows, err = cp.artifactRows(otherCtx)
	if err != nil {
		t.Fatalf("artifactRows other user after upsert: %v", err)
	}
	if len(rows) != 1 || rows[0]["source"] != "database" || rows[0]["content"] != "query { leaked { id } }" {
		t.Fatalf("other user should get its own db artifact, got rows=%+v", rows)
	}
}

func TestArtifactMigrationAddsReservedColumnsIdempotently(t *testing.T) {
	tmp := t.TempDir()
	dsn := "file:" + filepath.Join(tmp, "old.db")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`CREATE TABLE "_graphjin_artifacts" (
id TEXT PRIMARY KEY,
name TEXT NOT NULL,
kind TEXT NOT NULL,
path TEXT NOT NULL DEFAULT '',
source TEXT NOT NULL DEFAULT 'database',
visibility TEXT NOT NULL DEFAULT 'user',
read_only BOOLEAN NOT NULL DEFAULT 0,
account_id TEXT NOT NULL DEFAULT '',
owner_id TEXT NOT NULL DEFAULT '',
content TEXT NOT NULL DEFAULT '',
content_json TEXT,
metadata_json TEXT,
revision INTEGER NOT NULL DEFAULT 1,
created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`); err != nil {
		t.Fatalf("create old artifact table: %v", err)
	}

	svc := newSQLiteArtifactService(t, db, dsn, nil)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("first initArtifactsBeforeCore: %v", err)
	}
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("second initArtifactsBeforeCore: %v", err)
	}
	for _, column := range []string{"content_hash", "status"} {
		ok, err := sqliteColumnExists(context.Background(), db, quoteSQLIdent("_graphjin_artifacts"), column)
		if err != nil {
			t.Fatalf("check migrated column %s: %v", column, err)
		}
		if !ok {
			t.Fatalf("expected migrated column %s", column)
		}
	}
	if !sqliteTableExists(t, db, "_graphjin_revisions") {
		t.Fatal("expected revisions table during migration")
	}
}

func TestArtifactLockedKindsBlockWritePaths(t *testing.T) {
	tmp := t.TempDir()
	dsn := "file:" + filepath.Join(tmp, "locked.db")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	svc := newSQLiteArtifactService(t, db, dsn, []string{"query"})
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	ctx := context.WithValue(context.Background(), core.IdentityVarsKey, map[string]interface{}{"account_id": "acct_1", "user_id": "user_1"})
	cp := newArtifactControlPlane(svc)
	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     artifactsRootTable,
		Operation: "insert",
		Input:     map[string]interface{}{"name": "locked", "kind": "query", "content": "query { users { id } }"},
	}); !isArtifactKindLocked(err) {
		t.Fatalf("control-plane locked write error = %v, want artifactPolicyError", err)
	}
	if _, err := svc.saveUserArtifact(ctx, artifactKindSavedQuery, "locked", "query { users { id } }", nil); !isArtifactKindLocked(err) {
		t.Fatalf("overlay locked write error = %v, want artifactPolicyError", err)
	}
}

func TestArtifactLockedKindsBlockDelete(t *testing.T) {
	tmp := t.TempDir()
	dsn := "file:" + filepath.Join(tmp, "delete.db")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	svc := newSQLiteArtifactService(t, db, dsn, nil)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	startSQLiteArtifactCore(t, svc, db)
	ctx := context.WithValue(context.Background(), core.IdentityVarsKey, map[string]interface{}{"account_id": "acct_1", "user_id": "user_1"})
	row, err := svc.saveUserArtifact(ctx, artifactKindSavedQuery, "delete_me", "query { users { id } }", nil)
	if err != nil {
		t.Fatalf("save artifact: %v", err)
	}
	svc.conf.Core.Artifacts.Locked = []string{"saved_query"}
	if err := svc.deleteUserArtifact(ctx, artifactKindSavedQuery, "delete_me"); !isArtifactKindLocked(err) {
		t.Fatalf("overlay locked delete error = %v, want artifactPolicyError", err)
	}
	cp := newArtifactControlPlane(svc)
	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     artifactsRootTable,
		Operation: "delete",
		Input:     map[string]interface{}{"id": row["id"]},
	}); !isArtifactKindLocked(err) {
		t.Fatalf("control-plane locked delete error = %v, want artifactPolicyError", err)
	}
}

func sqliteTableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&count); err != nil {
		t.Fatalf("check sqlite table %s: %v", name, err)
	}
	return count != 0
}

func sqliteRevision(t *testing.T, db *sql.DB, domain string) int64 {
	t.Helper()
	var revision int64
	if err := db.QueryRow(`SELECT revision FROM "_graphjin_revisions" WHERE domain = ?`, domain).Scan(&revision); err != nil {
		t.Fatalf("query revision %s: %v", domain, err)
	}
	return revision
}

func isArtifactKindLocked(err error) bool {
	var policyErr artifactPolicyError
	return errors.As(err, &policyErr) && policyErr.Code == "artifact_kind_locked" && policyErr.PolicyFinal
}

func newSQLiteArtifactService(t *testing.T, db *sql.DB, dsn string, locked []string) *graphjinService {
	t.Helper()
	autoInit := true
	conf := &Config{Core: core.Config{
		Sources: []core.SourceConfig{
			{Name: "app", Kind: "database", Type: "sqlite", Path: dsn, Default: true},
			{Name: "graphjin", Kind: "graphjin"},
		},
		Artifacts: core.ArtifactsConfig{Enabled: true, Source: "app", AutoInit: &autoInit, GlobalsPath: ".", Locked: locked},
	}}
	conf.ConfigPath = t.TempDir()
	if err := conf.Core.NormalizeSources(); err != nil {
		t.Fatalf("NormalizeSources: %v", err)
	}
	return &graphjinService{
		conf: conf,
		dbs:  map[string]*sql.DB{"app": db},
		fs:   newAferoFS(afero.NewMemMapFs(), "/"),
	}
}

func startSQLiteArtifactCore(t *testing.T, svc *graphjinService, db *sql.DB) {
	t.Helper()
	svc.metadataDB = "app"
	svc.conf.Core.FS = svc.fs
	svc.injectInternalStoreRole()
	artifacts := newArtifactControlPlane(svc)
	opts := []core.Option{
		core.OptionSetFS(svc.fs),
		core.OptionSetDatabases(svc.dbs),
		core.OptionSetSavedQuerySaveHook(svc.saveSavedQueryArtifactOrFallback),
		core.OptionSetReservedRoleAuthorizer(svc.authorizeReservedRole),
		core.OptionSetManagedQueryHandler("app", artifacts),
		core.OptionSetManagedMutationHandler("app", artifacts),
	}
	gj, err := core.NewGraphJin(&svc.conf.Core, db, opts...)
	if err != nil {
		t.Fatalf("start GraphJin core: %v", err)
	}
	svc.gj = gj
}
