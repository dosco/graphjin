package secrets

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/open-neko/neko/apps/openneko/internal/config"
)

// The fixture was encrypted by the TS cipher (@neko/secret-crypt); this
// suite proves the Go mirror decrypts it and that a Go rewrite preserves
// the worker-owned _operators section byte-for-byte (the dual-writer
// clobber). The TS twin lives at
// packages/secret-crypt/test/cross-language.test.ts.
func fixtureDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "..",
		"packages", "secret-crypt", "test", "fixtures", "xdg", "openneko"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "secret-key")); err != nil {
		t.Fatalf("fixture missing: %v", err)
	}
	return dir
}

func TestCrossLanguageDecrypt(t *testing.T) {
	store, err := Read(fixtureDir(t))
	if err != nil {
		t.Fatal(err)
	}
	got := store["@open-neko/plugin-slack"]["SLACK_BOT_TOKEN"]
	if got != "xoxb-fixture-token" {
		t.Fatalf("decrypted env value = %q", got)
	}
}

func TestCrossLanguageLocalConfigPassword(t *testing.T) {
	lc, path := config.ReadLocal(fixtureDir(t))
	if path == "" || lc.Pg == nil {
		t.Fatalf("fixture config.json not read (path=%q)", path)
	}
	if lc.Pg.Password != "pg-fixture-pass" {
		t.Fatalf("decrypted pg.password = %q", lc.Pg.Password)
	}
}

func TestWritePreservesOperatorsAndEncrypts(t *testing.T) {
	src := fixtureDir(t)
	dir := t.TempDir()
	for _, name := range []string{"secret-key", "secrets.json"} {
		raw, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	var before map[string]json.RawMessage
	raw, _ := os.ReadFile(filepath.Join(dir, "secrets.json"))
	if err := json.Unmarshal(raw, &before); err != nil {
		t.Fatal(err)
	}

	store, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	store, err = Set(store, "@open-neko/plugin-telegram", "BOT_TOKEN", "tg-plain")
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(store, dir); err != nil {
		t.Fatal(err)
	}

	rewritten, err := os.ReadFile(filepath.Join(dir, "secrets.json"))
	if err != nil {
		t.Fatal(err)
	}
	var after map[string]json.RawMessage
	if err := json.Unmarshal(rewritten, &after); err != nil {
		t.Fatalf("rewritten file is invalid JSON: %v\n%s", err, rewritten)
	}

	var beforeOps, afterOps any
	if err := json.Unmarshal(before[OperatorsKey], &beforeOps); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(after[OperatorsKey], &afterOps); err != nil {
		t.Fatalf("_operators lost on rewrite: %v", err)
	}
	if !reflect.DeepEqual(beforeOps, afterOps) {
		t.Fatalf("_operators changed on rewrite:\nbefore: %v\nafter:  %v", beforeOps, afterOps)
	}

	var tg map[string]string
	if err := json.Unmarshal(after["@open-neko/plugin-telegram"], &tg); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tg["BOT_TOKEN"], "enc:v1:") {
		t.Fatalf("new value not encrypted at rest: %q", tg["BOT_TOKEN"])
	}

	again, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if again["@open-neko/plugin-telegram"]["BOT_TOKEN"] != "tg-plain" {
		t.Fatalf("round-trip read = %q", again["@open-neko/plugin-telegram"]["BOT_TOKEN"])
	}
	if again["@open-neko/plugin-slack"]["SLACK_BOT_TOKEN"] != "xoxb-fixture-token" {
		t.Fatal("pre-existing TS-encrypted value lost after Go rewrite")
	}
}
