package serv

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	_ "modernc.org/sqlite"
)

func TestArtifactControlPlaneInitializesAndScopesRows(t *testing.T) {
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
	}
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("initArtifactsBeforeCore: %v", err)
	}
	cp := newArtifactControlPlane(svc)
	ctx := context.WithValue(context.Background(), core.IdentityVarsKey, map[string]interface{}{"account_id": "acct_1", "user_id": "user_1"})
	if _, err := cp.mutateRow(ctx, core.ManagedMutationRoot{
		Table:     artifactsRootTable,
		Operation: "insert",
		Input:     map[string]interface{}{"name": "hello", "kind": "query", "content": "query { plans { id } }"},
	}); err != nil {
		t.Fatalf("insert artifact: %v", err)
	}
	rows, err := cp.artifactRows(ctx)
	if err != nil {
		t.Fatalf("artifactRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("db artifact should override same-name global, got rows=%+v", rows)
	}
	if rows[0]["source"] != "database" || rows[0]["visibility"] != "account" || rows[0]["read_only"] != false {
		t.Fatalf("unexpected artifact row: %+v", rows[0])
	}
	acct1ArtifactID, _ := rows[0]["id"].(string)

	otherCtx := context.WithValue(context.Background(), core.IdentityVarsKey, map[string]interface{}{"account_id": "acct_2", "user_id": "user_2"})
	rows, err = cp.artifactRows(otherCtx)
	if err != nil {
		t.Fatalf("artifactRows other account: %v", err)
	}
	if len(rows) != 1 || rows[0]["source"] != "config" || rows[0]["read_only"] != true {
		t.Fatalf("other account should see read-only global, got rows=%+v", rows)
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
		t.Fatalf("cross-account id upsert attempt: %v", err)
	}
	rows, err = cp.artifactRows(ctx)
	if err != nil {
		t.Fatalf("artifactRows original account after cross-account attempt: %v", err)
	}
	if len(rows) != 1 || rows[0]["content"] != "query { plans { id } }" {
		t.Fatalf("cross-account id must not overwrite original artifact, got rows=%+v", rows)
	}
	rows, err = cp.artifactRows(otherCtx)
	if err != nil {
		t.Fatalf("artifactRows other account after upsert: %v", err)
	}
	if len(rows) != 1 || rows[0]["source"] != "database" || rows[0]["content"] != "query { leaked { id } }" {
		t.Fatalf("other account should get its own db artifact, got rows=%+v", rows)
	}
}
