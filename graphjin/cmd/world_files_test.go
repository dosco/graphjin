package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/dosco/graphjin/serv/v3"
)

func fileSourceSchema() gjeval.ImportedSchema {
	schema := cloneTestSchema()
	schema.FileSources = []string{"sla_policies"}
	return schema
}

// A cloned file source is served locally with documents this tool wrote. What
// crosses over is the source's name, which the catalog already publishes to
// every connected agent — never a file name and never a line of content.
func TestCloneServesFileSourcesWithItsOwnDocuments(t *testing.T) {
	world, _ := cloneWorldSpec(fileSourceSchema(), cloneOptions{Rows: 5, Seed: 3}, "Clone")
	if len(world.FileSources) != 1 {
		t.Fatalf("the file source was not carried over: %+v", world.FileSources)
	}
	source := world.FileSources[0]
	if source.Name != "sla_policies" || source.Root != filepath.Join("files", "sla_policies") {
		t.Fatalf("unexpected source: %+v", source)
	}
	if len(source.Files) != 3 {
		t.Fatalf("expected three documents, got %d", len(source.Files))
	}
	config := renderWorldConfig(world)
	for _, want := range []string{
		"- name: sla_policies", "kind: file", "backend: local",
		"root: " + source.Root, "read_only: true", "files.read: true", "files.write: false",
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("config missing %q:\n%s", want, config)
		}
	}
}

// A written standard an agent can rewrite is not a standard: a task graded
// against one could be passed by editing the answer.
func TestClonedDocumentsAreReadOnly(t *testing.T) {
	world, _ := cloneWorldSpec(fileSourceSchema(), cloneOptions{Rows: 5, Seed: 3}, "Clone")
	config := renderWorldConfig(world)
	for _, forbidden := range []string{"files.write: true", "files.delete: true"} {
		if strings.Contains(config, forbidden) {
			t.Fatalf("documents must not be writable:\n%s", config)
		}
	}
}

// A project with no file source must render exactly what it rendered before.
func TestWorldsWithoutFileSourcesRenderUnchanged(t *testing.T) {
	world, _ := cloneWorldSpec(cloneTestSchema(), cloneOptions{Rows: 5, Seed: 3}, "Clone")
	if strings.Contains(renderWorldConfig(world), "kind: file") {
		t.Fatal("a project with no documents grew a file source")
	}
}

func TestWorldWritesItsDocumentsToDisk(t *testing.T) {
	world, _ := cloneWorldSpec(fileSourceSchema(), cloneOptions{Rows: 5, Seed: 3}, "Clone")
	dir := t.TempDir()
	if err := writeWorld(world, dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"policy-overview.md", "operations-handbook.md", "reference-notes.md"} {
		path := filepath.Join(dir, "files", "sla_policies", name)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("document not written: %v", err)
		}
		if len(body) == 0 {
			t.Fatalf("%s is empty; a listing that returns nothing is not a document source", name)
		}
	}
}

// A delivery task only works where the artifact log and the watch runner are
// on. Both come from the runtime-mode defaults rather than from anything the
// clone writes, so this pins that a cloned project boots with them.
func TestClonedProjectBootsWithWatchesAndArtifactsOn(t *testing.T) {
	world, _ := cloneWorldSpec(fileSourceSchema(), cloneOptions{Rows: 5, Seed: 3}, "Clone")
	dir := t.TempDir()
	if err := writeWorld(world, dir); err != nil {
		t.Fatal(err)
	}
	config, err := serv.ReadInConfig(filepath.Join(dir, "dev"))
	if err != nil {
		t.Fatal(err)
	}
	if !config.Core.Artifacts.Enabled {
		t.Fatal("no artifact log: a watch has nowhere to record that it fired")
	}
	if !config.Core.Watches.Enabled {
		t.Fatal("watches are off: a delivery task would wait out its timeout every episode")
	}
}

// Authored documents are written into the source's own root, and never over
// something already there.
func TestAuthoredDocumentsArePlantedAndNeverOverwritten(t *testing.T) {
	world, _ := cloneWorldSpec(fileSourceSchema(), cloneOptions{Rows: 5, Seed: 3}, "Clone")
	dir := t.TempDir()
	if err := writeWorld(world, dir); err != nil {
		t.Fatal(err)
	}
	file := gjeval.AuthoredFile{
		FileRoot: "sla_policies", Key: "authored-policy-invoices.md",
		Contents: "# Test\n\nRequirement: 4 hours.\n",
	}
	written, err := writeAuthoredFiles(dir, gjeval.TargetDemo, []gjeval.AuthoredFile{file})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "files", "sla_policies", file.Key)
	if len(written) != 1 || written[0] != want {
		t.Fatalf("planted in the wrong place: %v", written)
	}
	body, err := os.ReadFile(want)
	if err != nil || !strings.Contains(string(body), "Requirement: 4 hours.") {
		t.Fatalf("the planted requirement is not on disk: %v %q", err, body)
	}
	// A second pass must refuse rather than silently change what an existing
	// task is graded against.
	if _, err := writeAuthoredFiles(dir, gjeval.TargetDemo, []gjeval.AuthoredFile{file}); err == nil {
		t.Fatal("overwriting an existing document was allowed")
	}
	// A source that is not configured is a named failure, not a stray file.
	_, err = writeAuthoredFiles(dir, gjeval.TargetDemo, []gjeval.AuthoredFile{
		{FileRoot: "handbooks", Key: "x.md", Contents: "x"},
	})
	if err == nil || !strings.Contains(err.Error(), "handbooks") {
		t.Fatalf("expected a named refusal, got %v", err)
	}
}
