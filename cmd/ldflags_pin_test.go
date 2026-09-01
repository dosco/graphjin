package main

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The build identity is declared in three places and inherited by none of
// them: .goreleaser.yml's builds block, its kos block, and the Makefile. A
// binary missing one of them reports a version or commit it does not have —
// and /health now publishes exactly those values, so drift here becomes a lab
// comparing two runs it believes were the same build.
func TestEveryBuildStampsTheSameIdentity(t *testing.T) {
	release, err := os.ReadFile("../.goreleaser.yml")
	if err != nil {
		t.Skipf("no release configuration to check: %v", err)
	}
	makefile, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Skipf("no Makefile to check: %v", err)
	}

	blocks := ldflagBlocks(string(release))
	if len(blocks) < 3 {
		t.Fatalf("expected the builds block and both images to declare ldflags, found %d", len(blocks))
	}
	blocks = append(blocks, ldflagKeys(string(makefile)))

	baseline := blocks[0]
	if len(baseline) == 0 {
		t.Fatal("the builds block stamps nothing")
	}
	for _, required := range []string{"main.version", "main.commit", "main.date"} {
		if !contains(baseline, required) {
			t.Fatalf("%s is not stamped anywhere; /health would publish an empty field", required)
		}
	}
	for index, block := range blocks[1:] {
		missing := difference(baseline, block)
		if len(missing) != 0 {
			t.Fatalf("build site %d does not stamp %v, so a binary from it reports an identity it does not have",
				index+1, missing)
		}
	}

	// The environment image is the same build with one thing added: the role.
	// That is the whole difference, and it has to be there or the image serves
	// a database.
	role := false
	for _, block := range blocks {
		if contains(block, "main.imageRole") {
			role = true
		}
	}
	if !role {
		t.Fatal("no build stamps main.imageRole, so no image can be an environment")
	}
}

// ldflagBlocks returns the -X keys of each ldflags list in a YAML document.
func ldflagBlocks(document string) [][]string {
	var blocks [][]string
	var current []string
	inside := false
	for _, line := range strings.Split(document, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "ldflags:":
			if inside {
				blocks = append(blocks, current)
			}
			inside, current = true, nil
		case inside && strings.HasPrefix(trimmed, "- "):
			current = append(current, ldflagKeys(trimmed)...)
		case inside && trimmed != "":
			blocks = append(blocks, current)
			inside, current = false, nil
		}
	}
	if inside {
		blocks = append(blocks, current)
	}
	return blocks
}

var ldflagKeyPattern = regexp.MustCompile(`-X\s+"?([A-Za-z0-9_./\-]+)=`)

func ldflagKeys(text string) []string {
	var keys []string
	for _, match := range ldflagKeyPattern.FindAllStringSubmatch(text, -1) {
		keys = append(keys, match[1])
	}
	sort.Strings(keys)
	return keys
}

func contains(keys []string, want string) bool {
	for _, key := range keys {
		if key == want {
			return true
		}
	}
	return false
}

// difference returns the keys in want that block does not stamp, ignoring the
// role, which is what distinguishes the images rather than something they share.
func difference(want, block []string) []string {
	var missing []string
	for _, key := range want {
		if key != "main.imageRole" && !contains(block, key) {
			missing = append(missing, key)
		}
	}
	return missing
}
