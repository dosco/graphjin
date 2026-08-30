package eval

import (
	"strings"
	"testing"
)

// The registry is ordered oldest-first because Generate dedupes structurally on
// a first-wins basis. If a newer family were ever listed ahead of an older one,
// its copy of a colliding task would win and the published content ID for that
// behavior would change underneath the frozen suite.
func TestCandidateFamilyRegistryIsOrderedOldestFirst(t *testing.T) {
	if len(candidateFamilies) < 2 {
		t.Fatalf("expected at least the two founding families, got %d", len(candidateFamilies))
	}
	seen := map[string]bool{}
	highest := ""
	for _, family := range candidateFamilies {
		if family.Name == "" {
			t.Fatal("family has no name")
		}
		if seen[family.Name] {
			t.Fatalf("duplicate family name %q", family.Name)
		}
		seen[family.Name] = true
		if family.Generate == nil {
			t.Fatalf("family %q has no generator", family.Name)
		}
		if family.SinceVersion == "" {
			t.Fatalf("family %q has no since_version", family.Name)
		}
		if family.SinceVersion < highest {
			t.Fatalf("family %q (since %s) is listed after a newer family (since %s); registry order must be oldest-first",
				family.Name, family.SinceVersion, highest)
		}
		highest = family.SinceVersion
	}
	if candidateFamilies[0].Name != "catalog-core" {
		t.Fatalf("catalog-core must lead the registry, got %q", candidateFamilies[0].Name)
	}
}

func TestSelectedFamiliesDefaultsToEveryFamilyInOrder(t *testing.T) {
	families, err := selectedFamilies(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(families) != len(candidateFamilies) {
		t.Fatalf("expected all %d families, got %d", len(candidateFamilies), len(families))
	}
	for i := range families {
		if families[i].Name != candidateFamilies[i].Name {
			t.Fatalf("family %d: expected %q, got %q", i, candidateFamilies[i].Name, families[i].Name)
		}
	}
}

func TestSelectedFamiliesFiltersAndKeepsRegistryOrder(t *testing.T) {
	// Request them reversed: selection must still emit registry order, because
	// dedupe precedence depends on that order rather than on the caller's.
	families, err := selectedFamilies([]string{"deeporg-reference", "catalog-core"})
	if err != nil {
		t.Fatal(err)
	}
	if len(families) != 2 {
		t.Fatalf("expected 2 families, got %d", len(families))
	}
	if families[0].Name != "catalog-core" || families[1].Name != "deeporg-reference" {
		t.Fatalf("selection did not preserve registry order: %q, %q", families[0].Name, families[1].Name)
	}
}

func TestSelectedFamiliesRejectsUnknownName(t *testing.T) {
	_, err := selectedFamilies([]string{"catalog-core", "no-such-family"})
	if err == nil {
		t.Fatal("expected an error for an unknown family")
	}
	if !strings.Contains(err.Error(), "no-such-family") {
		t.Fatalf("error should name the unknown family, got %v", err)
	}
	if strings.Contains(err.Error(), "catalog-core") {
		t.Fatalf("error should not name the known family, got %v", err)
	}
}

// A caller that filters to one family must get that family only. This is what
// the content-ID stability guard relies on: replaying only the v12 families has
// to reproduce the v12 candidate set, with no newer family contributing.
func TestSelectedFamiliesSingleFamilyExcludesOthers(t *testing.T) {
	families, err := selectedFamilies([]string{"catalog-core"})
	if err != nil {
		t.Fatal(err)
	}
	if len(families) != 1 || families[0].Name != "catalog-core" {
		t.Fatalf("expected only catalog-core, got %+v", familyNames(families))
	}
}

func familyNames(families []candidateFamily) []string {
	names := make([]string, 0, len(families))
	for _, family := range families {
		names = append(names, family.Name)
	}
	return names
}
