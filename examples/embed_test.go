package examples

import (
	"io/fs"
	"strings"
	"testing"
)

// The embedded demo must contain the project and nothing else.
//
// `//go:embed all:saas-ops` also swept in demo/ and .graphjin/ — gitignored
// runtime state that exists only in a clone where somebody ran the demo. Two
// builds of the same commit then differed by whether the maintainer had, which
// is intolerable now that the binary's own sha256 is published as build
// identity. Extraction skipped those directories anyway, so this only ever
// added weight and non-reproducibility.
func TestEmbeddedDemoExcludesLocalRuntimeState(t *testing.T) {
	forbidden := []string{"saas-ops/demo", "saas-ops/.graphjin", "saas-ops/scripts"}
	err := fs.WalkDir(DefaultDemoFS, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		for _, prefix := range forbidden {
			if path == prefix || strings.HasPrefix(path, prefix+"/") {
				t.Errorf("%s is embedded; it is local runtime state and makes the binary depend on "+
					"whether the demo was ever run in this clone", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The other half: an explicit list can go stale when the demo gains a file, and
// the failure would otherwise be a demo that boots missing something. Every
// path here is one extraction or the eval boot actually needs.
func TestEmbeddedDemoCarriesEverythingTheDemoNeeds(t *testing.T) {
	required := []string{
		"saas-ops/dev.yml",
		"saas-ops/agentic.yml",
		"saas-ops/prod.yml",
		"saas-ops/.env.example",
		"saas-ops/seed/app.js",
		"saas-ops/seed/watches.yml",
	}
	for _, path := range required {
		if _, err := fs.Stat(DefaultDemoFS, path); err != nil {
			t.Errorf("%s is not embedded, so an extracted demo would be incomplete: %v", path, err)
		}
	}
	// Directories the project boots from; each must exist and carry files.
	for _, dir := range []string{
		"saas-ops/schema-ddl", "saas-ops/queries", "saas-ops/workflows",
		"saas-ops/files", "saas-ops/specs", "saas-ops/seed",
	} {
		entries, err := fs.ReadDir(DefaultDemoFS, dir)
		if err != nil {
			t.Errorf("%s is not embedded: %v", dir, err)
			continue
		}
		if len(entries) == 0 {
			t.Errorf("%s is embedded but empty", dir)
		}
	}
}
