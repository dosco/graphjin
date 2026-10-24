package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/fstable"
)

func userFolderEngine(t *testing.T, opts ...Option) *GraphJin {
	t.Helper()
	root := t.TempDir()
	mkfile(t, root, "users/alice/a1/plan.pdf", []byte{0x25, 0x50, 0xff})
	mkfile(t, root, "users/bob/b1/secret.pdf", []byte{0x25, 0x50, 0xff})
	mkfile(t, root, "team/handbook.md", []byte("shared"))
	cfgRoot := t.TempDir()
	schema := []byte("# dbinfo:postgres,120005,public\n\ntype users {\n  id: Bigint! @id @unique\n}\n")
	if err := os.WriteFile(filepath.Join(cfgRoot, "db.ddl"), schema, 0o644); err != nil {
		t.Fatal(err)
	}
	gj, err := NewGraphJinWithFS(&Config{
		MockDB: true, EnableSchema: true, DisableAllowList: true, DBType: "postgres",
		Sources: []SourceConfig{
			{Name: "app", Kind: "database", Type: "postgres", Default: true},
			{Name: "library", Kind: "file", Backend: "local", Root: root, UserPrefix: "users/"},
		},
	}, nil, NewOsFS(cfgRoot), opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gj.Close() })
	return gj
}

func libraryKeys(t *testing.T, gj *GraphJin, ctx context.Context, query string) []string {
	t.Helper()
	res, err := gj.GraphQL(ctx, query, nil, nil)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	var out struct {
		Library []struct {
			Key string `json:"key"`
		} `json:"library"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("%v\n%s", err, res.Data)
	}
	var keys []string
	for _, r := range out.Library {
		keys = append(keys, r.Key)
	}
	sort.Strings(keys)
	return keys
}

func TestUserPrefixKeepsUserFoldersPrivate(t *testing.T) {
	gj := userFolderEngine(t, OptionSetFileContent(userFolderSearch{}))
	alice := context.WithValue(context.Background(), UserIDKey, "alice")

	if got := strings.Join(libraryKeys(t, gj, alice, `query { library { key } }`), ","); got != "team/handbook.md,users/alice/a1/plan.pdf" {
		t.Fatalf("alice lists %s", got)
	}
	if got := libraryKeys(t, gj, alice, `query { library(where: { key: { eq: "users/bob/b1/secret.pdf" } }) { key } }`); len(got) != 0 {
		t.Fatalf("alice read bob's file: %v", got)
	}
	if got := strings.Join(libraryKeys(t, gj, alice, `query { library(search: "plan") { key } }`), ","); got != "team/handbook.md,users/alice/a1/plan.pdf" {
		t.Fatalf("alice search = %s", got)
	}
	if got := strings.Join(libraryKeys(t, gj, context.Background(), `query { library { key } }`), ","); got != "team/handbook.md" {
		t.Fatalf("no user lists %s", got)
	}
	b, _ := gj.FilesystemBackend("library")
	if _, err := b.Put(alice, "users/bob/b1/x.txt", strings.NewReader("x"), fstable.PutMeta{}); err == nil {
		t.Fatal("alice wrote into bob's folder")
	}
	src, _ := gj.FilesystemSourceBackend("library")
	entries, _, err := src.List(context.Background(), fstable.ListOpts{})
	if err != nil || len(entries) != 3 {
		t.Fatalf("source backend sees %d entries, %v", len(entries), err)
	}
}

// userFolderSearch matches every file, so the test sees which ones the
// caller is allowed to get back.
type userFolderSearch struct{ fakeFileContent }

func (userFolderSearch) Search(context.Context, string, string, int) ([]FileMatch, error) {
	return []FileMatch{{Key: "users/bob/b1/secret.pdf"}, {Key: "users/alice/a1/plan.pdf"}, {Key: "team/handbook.md"}}, nil
}

// crowdedSearch ranks thirty of bob's files above everything alice can see,
// the way a semantic ranking over a shared library can.
type crowdedSearch struct {
	fakeFileContent
	limits *[]int
}

func (s crowdedSearch) Search(_ context.Context, _, _ string, limit int) ([]FileMatch, error) {
	*s.limits = append(*s.limits, limit)
	var all []FileMatch
	for i := 0; i < 30; i++ {
		all = append(all, FileMatch{Key: fmt.Sprintf("users/bob/b1/f%d.pdf", i)})
	}
	all = append(all, FileMatch{Key: "users/alice/a1/plan.pdf"}, FileMatch{Key: "team/handbook.md"})
	if limit < len(all) {
		all = all[:limit]
	}
	return all, nil
}

func TestSearchRanksDeeperWhenAccessRulesThinThePage(t *testing.T) {
	var limits []int
	gj := userFolderEngine(t, OptionSetFileContent(crowdedSearch{limits: &limits}))
	alice := context.WithValue(context.Background(), UserIDKey, "alice")
	if got := strings.Join(libraryKeys(t, gj, alice, `query { library(search: "plan", limit: 2) { key } }`), ","); got != "team/handbook.md,users/alice/a1/plan.pdf" {
		t.Fatalf("alice search = %q after ranking limits %v", got, limits)
	}
	if limits[0] != 2 || limits[len(limits)-1] > maxFilesystemSearchFetch {
		t.Fatalf("ranking limits = %v", limits)
	}
}
