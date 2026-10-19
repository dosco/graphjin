package serv

import (
	"database/sql"
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

func TestAllocateRuntimeDatabaseNameAvoidsPublicAndRuntimeCollisions(t *testing.T) {
	conf := &core.Config{
		Sources: []core.SourceConfig{
			{Name: internalSystemDatabaseBase, Kind: "database", Type: "sqlite"},
			{Name: internalArtifactDatabaseBase, Kind: "database", Type: "sqlite"},
		},
	}
	runtime := &core.Config{Databases: map[string]core.DatabaseConfig{
		internalSystemDatabaseBase + "_2":   {Type: "sqlite"},
		internalArtifactDatabaseBase + "_2": {Type: "sqlite"},
	}}
	active := map[string]*sql.DB{
		internalSystemDatabaseBase + "_3":   nil,
		internalArtifactDatabaseBase + "_3": nil,
	}

	if got := allocateRuntimeDatabaseName(internalSystemDatabaseBase, conf, runtime, active); got != internalSystemDatabaseBase+"_4" {
		t.Fatalf("system runtime name = %q, want collision-free suffix", got)
	}
	if got := allocateRuntimeDatabaseName(internalArtifactDatabaseBase, conf, runtime, active); got != internalArtifactDatabaseBase+"_4" {
		t.Fatalf("artifact runtime name = %q, want collision-free suffix", got)
	}
}

func TestAttachStagedManagedArtifactStoreMovesRuntimeAliasAroundApplicationSource(t *testing.T) {
	artifactDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = artifactDB.Close() })

	service := &graphjinService{
		conf: &Config{
			Core:                 core.Config{Artifacts: core.ArtifactsConfig{Enabled: true}},
			managedArtifactStore: true,
		},
		dbs: map[string]*sql.DB{internalArtifactDatabaseBase: artifactDB},
		runtimeCore: &core.Config{Databases: map[string]core.DatabaseConfig{
			internalArtifactDatabaseBase: {Type: "sqlite", Path: "artifacts.sqlite3"},
		}},
		managedArtifactDB: internalArtifactDatabaseBase,
	}
	stagedCore := &core.Config{
		Sources:   []core.SourceConfig{{Name: internalArtifactDatabaseBase, Kind: "database", Type: "sqlite"}},
		Artifacts: core.ArtifactsConfig{Enabled: true},
	}
	applicationDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = applicationDB.Close() })
	stage := &stagedRuntimeState{
		dbs: map[string]*sql.DB{internalArtifactDatabaseBase: applicationDB},
		runtimeCore: &core.Config{Databases: map[string]core.DatabaseConfig{
			internalArtifactDatabaseBase: {Type: "sqlite"},
		}},
	}

	(&mcpServer{service: service}).attachStagedManagedArtifactStore(stagedCore, stage)

	if stage.managedArtifactDB == "" || stage.managedArtifactDB == internalArtifactDatabaseBase {
		t.Fatalf("managed artifact runtime alias = %q, want collision-free alias", stage.managedArtifactDB)
	}
	if stage.dbs[internalArtifactDatabaseBase] != applicationDB {
		t.Fatal("application source was overwritten by the managed artifact store")
	}
	if stage.dbs[stage.managedArtifactDB] != artifactDB {
		t.Fatal("managed artifact database was not retained under its runtime-only alias")
	}
	if stagedCore.Artifacts.Source != "" {
		t.Fatalf("runtime alias leaked into public config: %q", stagedCore.Artifacts.Source)
	}
	if stage.runtimeCore.Artifacts.Source != stage.managedArtifactDB {
		t.Fatalf("runtime artifact source = %q, want %q", stage.runtimeCore.Artifacts.Source, stage.managedArtifactDB)
	}
}
