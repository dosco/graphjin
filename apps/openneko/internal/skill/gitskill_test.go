package skill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGitSkillSpec(t *testing.T) {
	cases := []struct {
		in          string
		wantURL     string
		wantSubPath string
		wantErr     bool
	}{
		{"https://github.com/owner/repo", "https://github.com/owner/repo", "", false},
		{"https://github.com/owner/repo.git", "https://github.com/owner/repo", "", false},
		{"https://github.com/owner/repo#skills/apple/apple-notes", "https://github.com/owner/repo", "skills/apple/apple-notes", false},
		{"https://gitlab.com/group/repo", "https://gitlab.com/group/repo", "", false},
		{"https://codeberg.org/owner/repo", "https://codeberg.org/owner/repo", "", false},

		// Rejected.
		{"https://example.com/owner/repo", "", "", true}, // non-allowlisted host
		{"git@github.com:owner/repo", "", "", true},      // ssh
		{"http://github.com/owner/repo", "", "", true},   // http
		{"", "", "", true},
		{"https://github.com/owner/repo#../etc/passwd", "", "", true}, // path traversal
	}
	for _, c := range cases {
		gotURL, gotSub, err := parseGitSkillSpec(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseGitSkillSpec(%q) expected error, got %q/%q", c.in, gotURL, gotSub)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseGitSkillSpec(%q) unexpected error: %v", c.in, err)
			continue
		}
		if gotURL != c.wantURL || gotSub != c.wantSubPath {
			t.Errorf("parseGitSkillSpec(%q): got (%q,%q), want (%q,%q)", c.in, gotURL, gotSub, c.wantURL, c.wantSubPath)
		}
	}
}

func TestIsValidSkillDirName(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"pdf-processing", true},
		{"x", true},
		{"abc-123", true},
		{"", false},
		{"-leading", false},
		{"trailing-", false},
		{"double--hyphen", false},
		{"UPPER", false},
		{"under_score", false},
		{strings.Repeat("a", 65), false},
		{strings.Repeat("a", 64), true},
	}
	for _, c := range cases {
		if got := isValidSkillDirName(c.in); got != c.want {
			t.Errorf("isValidSkillDirName(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestRunGitSkill_HappyPath(t *testing.T) {
	skillsDir := t.TempDir()
	// Fake `git clone` that stages a skill folder at the clone target
	// dir, simulating what a real git clone would deposit.
	runner := func(_ context.Context, args []string, _ string) error {
		// args = ["clone", "--depth=1", repoURL, tmpDest]
		if len(args) < 4 {
			t.Fatalf("unexpected git args: %v", args)
		}
		dest := args[3]
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return err
		}
		// Pretend the clone contains a single root-level skill.
		skillMd := "---\nname: imported-skill\ndescription: From github.com/x/y.\n---\nbody."
		return os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte(skillMd), 0o644)
	}

	res, err := RunGitSkill(context.Background(), GitSkillOptions{
		Spec:             "https://github.com/x/y",
		SkillsInstallDir: skillsDir,
		GitRunner:        runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.SkillName != "imported-skill" {
		t.Errorf("SkillName: got %q want imported-skill", res.SkillName)
	}
	if res.SkillInstalledAt != filepath.Join(skillsDir, "imported-skill") {
		t.Errorf("SkillInstalledAt: got %q", res.SkillInstalledAt)
	}
	got, err := os.ReadFile(filepath.Join(res.SkillInstalledAt, "SKILL.md"))
	if err != nil {
		t.Fatalf("could not read copied SKILL.md: %v", err)
	}
	if !strings.Contains(string(got), "name: imported-skill") {
		t.Errorf("copied SKILL.md content mismatch: %s", got)
	}
}

func TestRunGitSkill_SubPathPicksRightFolder(t *testing.T) {
	skillsDir := t.TempDir()
	runner := func(_ context.Context, args []string, _ string) error {
		dest := args[3]
		// Stage a Hermes-like layout: skills live under skills/apple/.
		root := filepath.Join(dest, "skills", "apple", "apple-notes")
		if err := os.MkdirAll(root, 0o755); err != nil {
			return err
		}
		skillMd := "---\nname: apple-notes\ndescription: Manage Apple Notes.\nplatforms: [macos]\n---\nbody."
		return os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(skillMd), 0o644)
	}

	res, err := RunGitSkill(context.Background(), GitSkillOptions{
		Spec:             "https://github.com/NousResearch/hermes-agent#skills/apple/apple-notes",
		SkillsInstallDir: skillsDir,
		GitRunner:        runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.SkillName != "apple-notes" {
		t.Errorf("SkillName: got %q want apple-notes", res.SkillName)
	}
	if res.SourceSubPath != "skills/apple/apple-notes" {
		t.Errorf("SourceSubPath: got %q", res.SourceSubPath)
	}
	got, err := os.ReadFile(filepath.Join(res.SkillInstalledAt, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	// Hermes skill carries platforms: a Hermes top-level extension —
	// confirm we copy it through unchanged (tolerance for vendor fields).
	if !strings.Contains(string(got), "platforms: [macos]") {
		t.Errorf("Hermes-flavoured top-level field stripped: %s", got)
	}
}

func TestRunGitSkill_FailsWhenNoSkillMdAtPath(t *testing.T) {
	runner := func(_ context.Context, args []string, _ string) error {
		dest := args[3]
		// Clone but don't write a SKILL.md.
		return os.MkdirAll(dest, 0o755)
	}
	_, err := RunGitSkill(context.Background(), GitSkillOptions{
		Spec:             "https://github.com/x/empty-repo",
		SkillsInstallDir: t.TempDir(),
		GitRunner:        runner,
	})
	if err == nil || !strings.Contains(err.Error(), "SKILL.md") {
		t.Fatalf("expected SKILL.md-missing error, got %v", err)
	}
}

func TestRunGitSkill_FallsBackToPathBasenameWhenNameAbsent(t *testing.T) {
	skillsDir := t.TempDir()
	runner := func(_ context.Context, args []string, _ string) error {
		dest := args[3]
		root := filepath.Join(dest, "skills", "watchers")
		if err := os.MkdirAll(root, 0o755); err != nil {
			return err
		}
		// SKILL.md without a name: field — operator should still get
		// a directory keyed off the sub-path's last segment.
		skillMd := "---\ndescription: An import with no name field.\n---\nbody."
		return os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(skillMd), 0o644)
	}
	res, err := RunGitSkill(context.Background(), GitSkillOptions{
		Spec:             "https://github.com/x/repo#skills/watchers",
		SkillsInstallDir: skillsDir,
		GitRunner:        runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.SkillName != "watchers" {
		t.Errorf("expected fallback skill name 'watchers', got %q", res.SkillName)
	}
}

func TestRunGitSkill_RefusesInvalidSkillName(t *testing.T) {
	runner := func(_ context.Context, args []string, _ string) error {
		dest := args[3]
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return err
		}
		skillMd := "---\nname: BadName_With_Underscore\ndescription: x\n---\nbody."
		return os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte(skillMd), 0o644)
	}
	_, err := RunGitSkill(context.Background(), GitSkillOptions{
		Spec:             "https://github.com/x/y",
		SkillsInstallDir: t.TempDir(),
		GitRunner:        runner,
	})
	if err == nil || !strings.Contains(err.Error(), "not a valid skill identifier") {
		t.Fatalf("expected invalid-name error, got %v", err)
	}
}

func TestRunGitSkill_ReInstallReplacesPreviousCopy(t *testing.T) {
	skillsDir := t.TempDir()
	makeRunner := func(body string) func(context.Context, []string, string) error {
		return func(_ context.Context, args []string, _ string) error {
			dest := args[3]
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dest, "SKILL.md"),
				[]byte("---\nname: same-skill\ndescription: x\n---\n"+body), 0o644)
		}
	}
	if _, err := RunGitSkill(context.Background(), GitSkillOptions{
		Spec: "https://github.com/x/y", SkillsInstallDir: skillsDir, GitRunner: makeRunner("v1 body"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := RunGitSkill(context.Background(), GitSkillOptions{
		Spec: "https://github.com/x/y", SkillsInstallDir: skillsDir, GitRunner: makeRunner("v2 body"),
	}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(skillsDir, "same-skill", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "v2 body") || strings.Contains(string(got), "v1 body") {
		t.Errorf("re-install did not replace prior copy: %s", got)
	}
}
