package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func validPack() worldPackFile {
	return worldPackFile{
		AppName: "Sequencing Lab Operations", DomainSlug: "genome-sequencing",
		Entities: []worldPackEntity{
			{Table: "specimens", Label: "accession_code", Metric: "volume_ul", Date: "collected_at",
				Statuses: []string{"received", "quarantined", "released"}},
			{Table: "sequencing_runs", Label: "run_code", Metric: "read_count", Date: "started_at",
				Statuses: []string{"queued", "running", "failed", "complete"}, Follows: "specimens"},
		},
	}
}

func TestDescribedWorldBuildsFromItsPack(t *testing.T) {
	pack, err := validateWorldPack(validPack())
	if err != nil {
		t.Fatal(err)
	}
	if pack.Name != "genome-sequencing" || pack.AppName != "Sequencing Lab Operations" {
		t.Fatalf("unexpected pack: %+v", pack)
	}
	world := buildWorld(pack, 7, 0, nil, "")
	if len(world.Tables) != 2 {
		t.Fatalf("expected two tables, got %d", len(world.Tables))
	}
	if world.Tables[1].Parent != "specimens" {
		t.Fatalf("the relationship was lost: %+v", world.Tables[1])
	}
	ddl := renderWorldDDL(world)
	for _, want := range []string{"type specimens {", "type sequencing_runs {", "accession_code", "read_count", "@relation(type: specimens"} {
		if !strings.Contains(ddl, want) {
			t.Fatalf("DDL missing %q:\n%s", want, ddl)
		}
	}
}

// A described world is reproduced from its description, not from a domain name
// that was invented for it and matches no built-in vocabulary. Telling someone
// to run --domain genome-sequencing would send them to an error.
func TestDescribedWorldTellsYouHowToRebuildIt(t *testing.T) {
	pack, err := validateWorldPack(validPack())
	if err != nil {
		t.Fatal(err)
	}
	world := buildWorld(pack, 7, 0, nil, "")
	world.PackRef = worldPackFilename
	for _, rendered := range []string{renderWorldDDL(world), renderWorldReadme(world)} {
		if !strings.Contains(rendered, "--pack world-pack.json") {
			t.Fatalf("a described world must point at its description:\n%s", rendered)
		}
		if strings.Contains(rendered, "--domain genome-sequencing") {
			t.Fatalf("a described world must not claim a built-in domain:\n%s", rendered)
		}
	}
	// A built-in world keeps saying exactly what it always said.
	builtin := buildWorld(domainPacks[0], 7, 0, nil, "")
	if !strings.Contains(renderWorldDDL(builtin), "--domain logistics") {
		t.Fatal("a built-in world lost its regenerate line")
	}
}

func TestWorldPackValidationNamesWhatIsWrong(t *testing.T) {
	cases := map[string]struct {
		mutate func(*worldPackFile)
		want   string
	}{
		"no app name":      {func(p *worldPackFile) { p.AppName = "" }, "app_name"},
		"bad slug":         {func(p *worldPackFile) { p.DomainSlug = "Genome Sequencing" }, "domain_slug"},
		"too few entities": {func(p *worldPackFile) { p.Entities = p.Entities[:1] }, "between 2 and 8 entities"},
		"bad table name":   {func(p *worldPackFile) { p.Entities[0].Table = "Specimens" }, "lowercase snake_case"},
		"reserved prefix":  {func(p *worldPackFile) { p.Entities[0].Table = "gj_specimens" }, "reserved prefix"},
		"duplicate table":  {func(p *worldPackFile) { p.Entities[1].Table = "specimens" }, "appears twice"},
		"label collides with a built-in column": {
			func(p *worldPackFile) { p.Entities[0].Label = "status" }, "already a column"},
		"metric repeats the label": {
			func(p *worldPackFile) { p.Entities[0].Metric = p.Entities[0].Label }, "already a column"},
		"too few statuses": {
			func(p *worldPackFile) { p.Entities[0].Statuses = []string{"only_one"} }, "between 2 and 6 statuses"},
		"duplicate status": {
			func(p *worldPackFile) { p.Entities[0].Statuses = []string{"received", "received"} }, "appears twice"},
		"forward reference": {
			func(p *worldPackFile) { p.Entities[0].Follows = "sequencing_runs" }, "not one of the entities listed before it"},
		"unknown parent": {
			func(p *worldPackFile) { p.Entities[1].Follows = "freezers" }, "not one of the entities listed before it"},
	}
	for name, item := range cases {
		pack := validPack()
		item.mutate(&pack)
		_, err := validateWorldPack(pack)
		if err == nil {
			t.Fatalf("%s: expected a refusal", name)
		}
		if !strings.Contains(err.Error(), item.want) {
			t.Fatalf("%s: refused without saying %q: %v", name, item.want, err)
		}
	}
}

func TestSavedWorldPackRoundTrips(t *testing.T) {
	dir := t.TempDir()
	envelope := worldPackEnvelope{Described: "genome sequencing lab", AuthoredBy: "test/model", Pack: validPack()}
	if err := writeWorldPackFile(dir, worldPackFilename, envelope); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, worldPackFilename))
	if err != nil {
		t.Fatal(err)
	}
	var saved worldPackEnvelope
	if err := json.Unmarshal(body, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.SchemaVersion != worldPackSchemaVersion || saved.Described != "genome sequencing lab" {
		t.Fatalf("the description was not kept with the pack: %+v", saved)
	}
	loaded, err := loadWorldPack(filepath.Join(dir, worldPackFilename))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Described != "genome sequencing lab" || len(loaded.Pack.Entities) != 2 {
		t.Fatalf("round trip lost something: %+v", loaded)
	}
	// Rebuilding must not lose who wrote the vocabulary in the first place.
	if loaded.AuthoredBy != "test/model" {
		t.Fatalf("authorship was lost on reload: %q", loaded.AuthoredBy)
	}
	// The same description and seed must render the same world, or "reproducible"
	// means nothing.
	pack, err := validateWorldPack(loaded.Pack)
	if err != nil {
		t.Fatal(err)
	}
	first := buildWorld(pack, 7, 0, nil, "")
	second := buildWorld(pack, 7, 0, nil, "")
	if renderWorldSeed(first) != renderWorldSeed(second) || renderWorldDDL(first) != renderWorldDDL(second) {
		t.Fatal("the same description produced two different worlds")
	}
}

// Describing a world and naming a built-in domain are different requests, and
// silently honouring one of them would produce a world nobody asked for.
func TestWorldSourcesAreMutuallyExclusive(t *testing.T) {
	cases := map[string]worldPackRequest{
		"describe and pack":   {Describe: "a genome lab", PackPath: "world-pack.json"},
		"describe and domain": {Describe: "a genome lab", DomainSet: true, Domain: "retail"},
		"pack and domain":     {PackPath: "world-pack.json", DomainSet: true, Domain: "retail"},
	}
	for name, request := range cases {
		if _, _, err := resolveWorldPack(&cobra.Command{}, request); err == nil {
			t.Fatalf("%s: expected a refusal", name)
		}
	}
	// The ordinary path stays free of all this: no description, no spend.
	pack, envelope, err := resolveWorldPack(&cobra.Command{}, worldPackRequest{Domain: "retail"})
	if err != nil || envelope != nil || pack.Name != "retail" {
		t.Fatalf("a built-in domain must resolve without a model: %v %+v", err, pack)
	}
}

// A pack somebody wrote by hand is a reasonable thing to have.
func TestBareWorldPackLoads(t *testing.T) {
	dir := t.TempDir()
	body, err := json.Marshal(validPack())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "hand-written.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadWorldPack(path)
	if err != nil || loaded.Described != "" || len(loaded.Pack.Entities) != 2 {
		t.Fatalf("a bare pack must load: %v %+v", err, loaded)
	}
	if _, err := loadWorldPack(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("a missing pack must fail")
	}
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadWorldPack(path); err == nil {
		t.Fatal("an unreadable pack must fail")
	}
}
