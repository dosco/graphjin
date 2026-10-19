package serv

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/spf13/afero"
)

func TestDiscoveryGenerationRejectsPartialAndCorruptFiles(t *testing.T) {
	fs := newAferoFS(afero.NewMemMapFs(), "/")
	service := &graphjinService{conf: &Config{Serv: Serv{DiscoveryCache: DiscoveryCacheConfig{Path: ".graphjin/discovery"}}}, fs: fs}
	manager := &discoveryGenerationManager{service: service, base: ".graphjin/discovery", fingerprint: "fingerprint"}
	id := "20260714T000000.000000000Z-valid"
	dir := manager.generationDir(id)
	fileData := []byte(`{"version":1,"tables":[]}`)
	sum := sha256.Sum256(fileData)
	manifest := discoveryGenerationManifest{
		FormatVersion: discoveryGenerationFormatVersion,
		GenerationID:  id,
		Fingerprint:   "fingerprint",
		SourceRevisions: map[string]string{
			core.DefaultDBName: hex.EncodeToString(sum[:]),
		},
		Files: []discoveryGenerationFile{{Name: "default.schema.json", Size: len(fileData), SHA256: hex.EncodeToString(sum[:])}},
	}
	manifestData, _ := json.Marshal(manifest)
	if err := fs.Put(path.Join(dir, "default.schema.json"), fileData); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.loadGeneration(id); err == nil {
		t.Fatal("partial generation without manifest should not load")
	}
	if err := fs.Put(manager.manifestPath(id), manifestData); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.loadGeneration(id); err != nil {
		t.Fatalf("valid generation: %v", err)
	}
	if err := fs.Put(path.Join(dir, "default.schema.json"), []byte("corrupt")); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.loadGeneration(id); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("corrupt generation error = %v, want checksum/size mismatch", err)
	}
}

func TestDiscoveryConfigFingerprintDoesNotPersistCredentials(t *testing.T) {
	conf := &Config{
		Core: core.Config{DBType: "postgres"},
		Serv: Serv{DB: Database{
			Type: "postgres", ConnString: "postgres://secret-user:secret-password@db.example.com/app?sslmode=require",
			Host: "db.example.com", DBName: "app", User: "secret-user", Password: "secret-password",
		}},
	}
	fingerprint, err := discoveryConfigFingerprint(conf)
	if err != nil {
		t.Fatal(err)
	}
	if len(fingerprint) != sha256.Size*2 || strings.Contains(fingerprint, "secret") || strings.Contains(fingerprint, "postgres") {
		t.Fatalf("unexpected persisted fingerprint %q", fingerprint)
	}
	identity := redactedConnectionIdentity(conf.DB.ConnString)
	if strings.Contains(identity, "secret") || strings.Contains(identity, "sslmode") {
		t.Fatalf("redacted connection identity leaked credentials/query: %q", identity)
	}
	if !strings.Contains(identity, "db.example.com/app") {
		t.Fatalf("redacted connection identity lost database endpoint: %q", identity)
	}

	changedSecrets := *conf
	changedSecrets.DB = conf.DB
	changedSecrets.DB.User = "another-user"
	changedSecrets.DB.Password = "another-password"
	changedSecrets.DB.ConnString = "postgres://another-user:another-password@db.example.com/app?sslmode=disable"
	changedFingerprint, err := discoveryConfigFingerprint(&changedSecrets)
	if err != nil {
		t.Fatal(err)
	}
	if changedFingerprint != fingerprint {
		t.Fatalf("credentials changed discovery fingerprint: %q != %q", changedFingerprint, fingerprint)
	}
}

func TestDiscoveryRedisScopeChangesWithSchemaFingerprint(t *testing.T) {
	fs := newAferoFS(afero.NewMemMapFs(), "/")
	confA := &Config{
		Core: core.Config{DBType: "sqlite", Blocklist: []string{"audit_*"}},
		Serv: Serv{DiscoveryCache: DiscoveryCacheConfig{Path: ".graphjin/discovery"}},
	}
	confB := *confA
	confB.Core = confA.Core
	confB.Core.Blocklist = []string{"internal_*"}

	managerA, err := newDiscoveryGenerationManager(&graphjinService{conf: confA, fs: fs})
	if err != nil {
		t.Fatal(err)
	}
	defer managerA.Close()
	managerB, err := newDiscoveryGenerationManager(&graphjinService{conf: &confB, fs: fs})
	if err != nil {
		t.Fatal(err)
	}
	defer managerB.Close()

	if managerA.fingerprint == managerB.fingerprint {
		t.Fatal("schema-affecting blocklist change did not change discovery fingerprint")
	}
	if managerA.prefix == managerB.prefix {
		t.Fatalf("schema fingerprints share Redis coordination scope %q", managerA.prefix)
	}
}
