package serv

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/openapi"
)

func testKeystoreKey(seed byte) string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{seed}, 32))
}

func TestLocalKeystoreSealOpenSavePermissionsAndWrongKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.enc.yml")
	conf := &Config{Serv: Serv{Secrets: SecretsConfig{Keystore: KeystoreConfig{
		Key:  testKeystoreKey(7),
		Path: path,
	}}}}
	ks, err := newLocalKeystore(conf)
	if err != nil {
		t.Fatalf("new keystore: %v", err)
	}
	ref := "gjsecret://databases/main/connection_string"
	plaintext := "postgres://user:secret@example.test/app"
	if err := ks.Seal(ref, plaintext); err != nil {
		t.Fatalf("seal: %v", err)
	}
	if err := ks.Save(map[string]struct{}{ref: {}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read keystore: %v", err)
	}
	if strings.Contains(string(data), plaintext) {
		t.Fatalf("keystore file leaked plaintext:\n%s", string(data))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat keystore: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("keystore permissions = %v, want 0600", got)
	}

	reopened, err := newLocalKeystore(conf)
	if err != nil {
		t.Fatalf("reopen keystore: %v", err)
	}
	got, err := reopened.Open(ref)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got != plaintext {
		t.Fatalf("open = %q, want %q", got, plaintext)
	}

	wrongKey := &Config{Serv: Serv{Secrets: SecretsConfig{Keystore: KeystoreConfig{
		Key:  testKeystoreKey(8),
		Path: path,
	}}}}
	wrong, err := newLocalKeystore(wrongKey)
	if err != nil {
		t.Fatalf("new wrong-key keystore: %v", err)
	}
	if _, err := wrong.Open(ref); err == nil || !strings.Contains(err.Error(), "decrypt secret ref") {
		t.Fatalf("expected wrong-key decrypt error, got %v", err)
	}
}

func TestLocalKeystoreMissingKeyCorruptFileRefParsingAndPrune(t *testing.T) {
	conf := &Config{}
	coreConf := core.Config{Databases: map[string]core.DatabaseConfig{
		"main": {Type: "postgres", ConnString: "gjsecret://databases/main/connection_string"},
	}}
	svc := &graphjinService{conf: conf}
	err := svc.hydrateCoreConfigSecrets(&coreConf)
	if err == nil || !strings.Contains(err.Error(), "secrets.keystore.key") {
		t.Fatalf("expected missing key error, got %v", err)
	}
	if _, err := parseSecretRef("gjsecret://"); err == nil {
		t.Fatal("expected empty secret ref path to fail")
	}
	if _, err := parseSecretRef("env://databases/main/password"); err == nil {
		t.Fatal("expected non-gjsecret ref to fail")
	}

	path := filepath.Join(t.TempDir(), "corrupt.yml")
	if err := os.WriteFile(path, []byte("secrets: ["), 0o600); err != nil {
		t.Fatalf("write corrupt keystore: %v", err)
	}
	_, err = newLocalKeystore(&Config{Serv: Serv{Secrets: SecretsConfig{Keystore: KeystoreConfig{
		Key:  testKeystoreKey(1),
		Path: path,
	}}}})
	if err == nil || !strings.Contains(err.Error(), "parse secrets keystore") {
		t.Fatalf("expected corrupt keystore parse error, got %v", err)
	}

	prunePath := filepath.Join(t.TempDir(), "prune.yml")
	ks, err := newLocalKeystore(&Config{Serv: Serv{Secrets: SecretsConfig{Keystore: KeystoreConfig{
		Key:  testKeystoreKey(2),
		Path: prunePath,
	}}}})
	if err != nil {
		t.Fatalf("new prune keystore: %v", err)
	}
	keep := "gjsecret://databases/main/password"
	drop := "gjsecret://databases/old/password"
	if err := ks.Seal(keep, "one"); err != nil {
		t.Fatalf("seal keep: %v", err)
	}
	if err := ks.Seal(drop, "two"); err != nil {
		t.Fatalf("seal drop: %v", err)
	}
	if err := ks.Save(map[string]struct{}{keep: {}}); err != nil {
		t.Fatalf("save pruned: %v", err)
	}
	reopened, err := newLocalKeystore(&Config{Serv: Serv{Secrets: SecretsConfig{Keystore: KeystoreConfig{
		Key:  testKeystoreKey(2),
		Path: prunePath,
	}}}})
	if err != nil {
		t.Fatalf("reopen pruned: %v", err)
	}
	if _, err := reopened.Open(keep); err != nil {
		t.Fatalf("open kept ref: %v", err)
	}
	if _, err := reopened.Open(drop); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected pruned ref to be missing, got %v", err)
	}
}

func TestSealCoreConfigSecretsAndHydrateRuntimeConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.enc.yml")
	ks, err := newLocalKeystore(&Config{Serv: Serv{Secrets: SecretsConfig{Keystore: KeystoreConfig{
		Key:  testKeystoreKey(3),
		Path: path,
	}}}})
	if err != nil {
		t.Fatalf("new keystore: %v", err)
	}
	plaintextDSN := "postgres://user:secret@example.test/app"
	plaintextAPIKey := "sk_live_secret"
	coreConf := core.Config{
		Databases: map[string]core.DatabaseConfig{
			"main": {Type: "postgres", ConnString: plaintextDSN},
		},
		Sources: []core.SourceConfig{
			{
				Name: "api",
				Kind: "api",
				Specs: map[string]openapi.SpecConfig{
					"stripe": {Auth: openapi.AuthConfig{Scheme: "api_key", KeyValue: plaintextAPIKey}},
				},
			},
		},
	}
	usedRefs, err := sealCoreConfigSecrets(&coreConf, ks)
	if err != nil {
		t.Fatalf("seal config: %v", err)
	}
	dsnRef := "gjsecret://databases/main/connection_string"
	apiRef := "gjsecret://sources/api/specs/stripe/auth/key_value"
	if got := coreConf.Databases["main"].ConnString; got != dsnRef {
		t.Fatalf("database connection string ref = %q, want %q", got, dsnRef)
	}
	if got := coreConf.Sources[0].Specs["stripe"].Auth.KeyValue; got != apiRef {
		t.Fatalf("api key ref = %q, want %q", got, apiRef)
	}
	if _, ok := usedRefs[dsnRef]; !ok {
		t.Fatalf("used refs missing %s: %#v", dsnRef, usedRefs)
	}
	if _, ok := usedRefs[apiRef]; !ok {
		t.Fatalf("used refs missing %s: %#v", apiRef, usedRefs)
	}
	if err := ks.Save(usedRefs); err != nil {
		t.Fatalf("save sealed config: %v", err)
	}
	if err := hydrateConfigSecrets(&coreConf, ks); err != nil {
		t.Fatalf("hydrate config: %v", err)
	}
	if got := coreConf.Databases["main"].ConnString; got != plaintextDSN {
		t.Fatalf("hydrated dsn = %q, want %q", got, plaintextDSN)
	}
	if got := coreConf.Sources[0].Specs["stripe"].Auth.KeyValue; got != plaintextAPIKey {
		t.Fatalf("hydrated api key = %q, want %q", got, plaintextAPIKey)
	}
}

func TestPlaintextSecretUpdatePathsDetectsModelSuppliedSecrets(t *testing.T) {
	paths := plaintextSecretUpdatePaths(map[string]any{
		"databases": map[string]any{
			"main": map[string]any{
				"type":              "postgres",
				"connection_string": "postgres://user:secret@example.test/app",
				"password":          "${GJ_DATABASE_PASSWORD}",
			},
			"analytics": map[string]any{
				"type":     "postgres",
				"password": "gjsecret://databases/analytics/password",
			},
		},
		"sources": []any{
			map[string]any{
				"name": "api",
				"kind": "api",
				"specs": map[string]any{
					"stripe": map[string]any{
						"auth": map[string]any{
							"key_value": "sk_live_secret",
							"request": map[string]any{
								"body": map[string]any{"apiKey": "body-secret"},
							},
						},
					},
				},
			},
		},
		"resolvers": []any{
			map[string]any{
				"name": "remote",
				"type": "remote_api",
				"set_headers": []any{
					map[string]any{"name": "X-Api-Key", "value": "secret-header"},
				},
			},
		},
	})
	want := map[string]bool{
		"databases.main.connection_string":                  true,
		"sources.api.specs.stripe.auth.key_value":           true,
		"sources.api.specs.stripe.auth.request.body.apiKey": true,
		"resolvers.remote.set_headers.X-Api-Key.value":      true,
	}
	for _, path := range paths {
		if !want[path] {
			t.Fatalf("unexpected plaintext secret path %q in %v", path, paths)
		}
		delete(want, path)
	}
	if len(want) != 0 {
		t.Fatalf("missing plaintext secret paths: %v (got %v)", want, paths)
	}
}
