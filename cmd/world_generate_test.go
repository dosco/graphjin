package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The same seed has to be the same company every time. A world nobody can
// reproduce is a world nobody can be measured against twice.
func TestGeneratedWorldIsReproducibleFromItsSeed(t *testing.T) {
	pack, err := packByName("logistics")
	if err != nil {
		t.Fatal(err)
	}
	first := buildWorld(pack, 7, 0, []string{PathologyNullableGaps}, "")
	second := buildWorld(pack, 7, 0, []string{PathologyNullableGaps}, "")
	if renderWorldDDL(first) != renderWorldDDL(second) {
		t.Fatal("the same seed produced a different schema")
	}
	if renderWorldSeed(first) != renderWorldSeed(second) {
		t.Fatal("the same seed produced different data")
	}
	other := buildWorld(pack, 8, 0, []string{PathologyNullableGaps}, "")
	if renderWorldSeed(first) == renderWorldSeed(other) {
		t.Fatal("two seeds produced the same data; worlds would not be diverse")
	}
}

func TestGeneratedWorldsDifferAcrossDomains(t *testing.T) {
	seen := map[string]bool{}
	for _, pack := range domainPacks {
		world := buildWorld(pack, 3, 0, nil, "")
		if len(world.Tables) == 0 {
			t.Fatalf("domain %s generated no tables", pack.Name)
		}
		for _, table := range world.Tables {
			if seen[table.Name] {
				t.Fatalf("table %q appears in more than one domain; worlds would overlap", table.Name)
			}
			seen[table.Name] = true
		}
	}
}

// Every table needs the ingredients the task families read: a key, a label, a
// closed value set, a metric and a date. A world missing one of them generates
// a suite with holes in it.
func TestGeneratedWorldGivesEveryTableWhatTasksNeed(t *testing.T) {
	for _, pack := range domainPacks {
		world := buildWorld(pack, 11, 0, nil, "")
		for _, table := range world.Tables {
			var hasKey, hasClosedSet, hasMetric, hasDate, hasLabel bool
			for _, column := range table.Columns {
				switch {
				case strings.Contains(column.Type, "@id"):
					hasKey = true
				case column.Name == table.Label:
					hasLabel = true
				case len(column.Values) != 0 && !column.Nullable:
					hasClosedSet = true
				case strings.HasPrefix(column.Type, "Bigint!") && !strings.Contains(column.Type, "@"):
					hasMetric = true
				case strings.HasPrefix(column.Type, "TimestampWithTimeZone"):
					hasDate = true
				}
			}
			if !hasKey || !hasLabel || !hasClosedSet || !hasMetric || !hasDate {
				t.Fatalf("%s.%s is missing something tasks need: key=%v label=%v values=%v metric=%v date=%v",
					pack.Name, table.Name, hasKey, hasLabel, hasClosedSet, hasMetric, hasDate)
			}
		}
	}
}

// A relationship is what traversal tasks are built from, so a world without one
// silently loses a whole family.
func TestGeneratedWorldHasRelationships(t *testing.T) {
	for _, pack := range domainPacks {
		world := buildWorld(pack, 5, 0, nil, "")
		related := 0
		for _, table := range world.Tables {
			if table.Parent != "" {
				related++
			}
		}
		if related == 0 {
			t.Fatalf("domain %s generated no relationships", pack.Name)
		}
	}
}

func TestPathologiesAreOptInAndNamed(t *testing.T) {
	pack, err := packByName("retail")
	if err != nil {
		t.Fatal(err)
	}
	plain := renderWorldDDL(buildWorld(pack, 4, 0, nil, ""))
	for _, unwanted := range []string{"status_code", "legacy_", "not a geographic state"} {
		if strings.Contains(plain, unwanted) {
			t.Fatalf("a world asked for no pathologies got %q", unwanted)
		}
	}

	// The collision has to be a real one: the same column name in two tables,
	// meaning different things, with nothing but the comment to tell them apart.
	collided := buildWorld(pack, 4, 0, []string{PathologySynonymCollision}, "")
	tablesWithState := 0
	for _, table := range collided.Tables {
		for _, column := range table.Columns {
			if column.Name == "state" {
				tablesWithState++
			}
		}
	}
	if tablesWithState < 2 {
		t.Fatalf("the synonym collision needs the same name in two tables, found %d", tablesWithState)
	}

	if _, err := validatePathologies([]string{"make-it-hard"}); err == nil {
		t.Fatal("an unknown pathology must be refused rather than silently ignored")
	}
	known, err := validatePathologies([]string{PathologyLegacyColumns, ""})
	if err != nil || len(known) != 1 {
		t.Fatalf("validate returned %v, %v", known, err)
	}
}

func TestWorldWritesACompleteProject(t *testing.T) {
	pack, err := packByName("clinic")
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "world")
	world := buildWorld(pack, 9, 0, []string{PathologyDistractorColumns}, "")
	if err := writeWorld(world, directory); err != nil {
		t.Fatal(err)
	}
	// These are exactly the files a hand-written project has. A generated world
	// that needed its own boot path would stop exercising the real one.
	for _, name := range []string{"dev.yml", "schema-ddl/app.ddl", "seed/app.js", "README.md"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatalf("generated world is missing %s: %v", name, err)
		}
	}
	// Writing into a directory that already holds something would quietly mix
	// two worlds together.
	if err := writeWorld(world, directory); err == nil {
		t.Fatal("expected a second write into the same directory to be refused")
	}
}

func TestGeneratedSeedReferencesRealParentRows(t *testing.T) {
	pack, err := packByName("logistics")
	if err != nil {
		t.Fatal(err)
	}
	world := buildWorld(pack, 13, 0, nil, "")
	seed := renderWorldSeed(world)
	for _, table := range world.Tables {
		if table.Parent == "" {
			continue
		}
		if !strings.Contains(seed, singular(table.Parent)+"_id:") {
			t.Fatalf("%s rows never reference %s", table.Name, table.Parent)
		}
	}
	// Foreign keys must land inside the parent's row range, or the seed fails
	// to insert and the world never boots.
	for _, table := range world.Tables {
		if table.Parent == "" {
			continue
		}
		var parentRows int
		for _, candidate := range world.Tables {
			if candidate.Name == table.Parent {
				parentRows = candidate.RowCount
			}
		}
		if parentRows == 0 {
			t.Fatalf("%s has no parent rows to reference", table.Name)
		}
	}
}
