// Package skill installs community skills from a git URL
// (`openneko install <git-url>`) and reads the deployment install policy
// that gates that path.
package skill

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitSkillOptions configures a single `openneko install <git-url>` call.
//
// Spec forms accepted:
//
//   - https://github.com/owner/repo
//   - https://github.com/owner/repo#path/to/skill
//   - https://gitlab.com/owner/repo[#sub/path]
//   - https://codeberg.org/owner/repo[#sub/path]
//
// The fragment after `#` is an optional sub-path inside the repo —
// useful when a single repo carries many skills (e.g. a catalog that
// ships dozens under `skills/`). The skill root (the directory containing
// SKILL.md) must live at that sub-path; the loader doesn't search.
type GitSkillOptions struct {
	Spec             string
	SkillsInstallDir string
	// Override `git` invocation (tests).
	GitRunner func(ctx context.Context, args []string, cwd string) error
	// Override the destination dir resolver (tests).
	ResolveSkillsDir func(override string) string
}

// GitSkillResult is what a successful git-URL install returns.
type GitSkillResult struct {
	SkillName        string
	SkillInstalledAt string
	SourceURL        string
	SourceSubPath    string
}

// RunGitSkill clones the given git URL into a temp dir, validates the
// SKILL.md at root (or under the optional sub-path), and copies the
// skill folder under SkillsInstallDir/<skill-name>/. Git-URL installs
// are skill-only and don't contribute action handlers.
//
// The caller (the CLI's install command) is responsible for verifying
// the deployment install policy permits this path before invoking.
func RunGitSkill(ctx context.Context, opts GitSkillOptions) (*GitSkillResult, error) {
	repoURL, subPath, err := parseGitSkillSpec(opts.Spec)
	if err != nil {
		return nil, err
	}

	tmp, err := os.MkdirTemp("", "openneko-skill-clone-")
	if err != nil {
		return nil, fmt.Errorf("install: temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	runner := opts.GitRunner
	if runner == nil {
		runner = realGit
	}
	// `git clone --depth=1` keeps the operator's network spend small;
	// we don't need history for a skill folder.
	if err := runner(ctx, []string{"clone", "--depth=1", repoURL, tmp}, ""); err != nil {
		return nil, fmt.Errorf("install: git clone failed: %w", err)
	}

	skillRoot := tmp
	if subPath != "" {
		skillRoot = filepath.Join(tmp, filepath.FromSlash(subPath))
	}
	skillMd := filepath.Join(skillRoot, "SKILL.md")
	if _, err := os.Stat(skillMd); err != nil {
		hint := "SKILL.md must be at the repo root, or use the #<path/to/skill> fragment to point at a sub-directory"
		return nil, fmt.Errorf("install: %s not found in clone — %s", skillMd, hint)
	}

	skillName, err := readSkillFrontmatterName(skillMd)
	if err != nil {
		return nil, fmt.Errorf("install: failed to read SKILL.md: %w", err)
	}
	if skillName == "" {
		// Spec requires a `name:` field. Fall back to the repo basename
		// rather than failing hard — operator gets a usable directory
		// name without us throwing on a malformed skill.
		skillName = repoBasenameFromURL(repoURL)
		if subPath != "" {
			// Use the last segment of the sub-path; far more
			// recognizable than the repo name for skills inside a
			// multi-skill monorepo.
			parts := strings.Split(strings.Trim(subPath, "/"), "/")
			if len(parts) > 0 && parts[len(parts)-1] != "" {
				skillName = parts[len(parts)-1]
			}
		}
	}
	if !isValidSkillDirName(skillName) {
		return nil, fmt.Errorf("install: SKILL.md `name: %q` is not a valid skill identifier (lowercase letters, digits, hyphens only)", skillName)
	}

	resolve := opts.ResolveSkillsDir
	if resolve == nil {
		resolve = resolveSkillsInstallDir
	}
	dest := filepath.Join(resolve(opts.SkillsInstallDir), skillName)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return nil, err
	}
	// Re-installs replace; we never merge directories. The previous
	// copy is removed so stale supporting files don't survive across
	// upstream renames.
	_ = os.RemoveAll(dest)
	if err := copyDir(skillRoot, dest); err != nil {
		return nil, fmt.Errorf("install: copy skill into place: %w", err)
	}
	return &GitSkillResult{
		SkillName:        skillName,
		SkillInstalledAt: dest,
		SourceURL:        repoURL,
		SourceSubPath:    subPath,
	}, nil
}

// parseGitSkillSpec splits a `<url>[#<subpath>]` spec. Only http/https
// schemes against well-known forge hosts are accepted.
func parseGitSkillSpec(spec string) (repoURL, subPath string, err error) {
	if spec == "" {
		return "", "", errors.New("install: spec is empty")
	}
	urlPart, sub, _ := strings.Cut(spec, "#")
	parsed, err := url.Parse(urlPart)
	if err != nil {
		return "", "", fmt.Errorf("install: invalid git URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return "", "", fmt.Errorf("install: git URLs must use https (got %q)", parsed.Scheme)
	}
	switch parsed.Host {
	case "github.com", "gitlab.com", "codeberg.org":
		// ok
	default:
		return "", "", fmt.Errorf("install: git URLs must point at github.com, gitlab.com, or codeberg.org (got %q)", parsed.Host)
	}
	// Trim any trailing .git so the basename derivation below is clean.
	urlPart = strings.TrimSuffix(urlPart, ".git")
	if sub != "" {
		sub = strings.Trim(sub, "/")
	}
	if strings.Contains(sub, "..") {
		return "", "", errors.New("install: sub-path cannot contain ..")
	}
	return urlPart, sub, nil
}

func realGit(ctx context.Context, args []string, _ string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func repoBasenameFromURL(repoURL string) string {
	parsed, err := url.Parse(repoURL)
	if err != nil || parsed.Path == "" {
		return "imported-skill"
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) == 0 {
		return "imported-skill"
	}
	return parts[len(parts)-1]
}

// isValidSkillDirName mirrors the agentskills.io spec name constraint:
// lowercase alphanumerics + hyphens, no leading/trailing hyphen, no
// consecutive hyphens, max 64 chars.
func isValidSkillDirName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	if name[0] == '-' || name[len(name)-1] == '-' {
		return false
	}
	if strings.Contains(name, "--") {
		return false
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
			return false
		}
	}
	return true
}

func resolveSkillsInstallDir(override string) string {
	if override != "" {
		return override
	}
	if v := os.Getenv("OPENNEKO_SKILLS_DIR"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		home = os.TempDir()
	}
	return filepath.Join(home, ".openneko", "skills")
}

// readSkillFrontmatterName extracts the `name:` field from a
// SKILL.md's YAML frontmatter — the absolute minimum the install
// path needs. Mirrors extractNameFromFrontmatter on the TS side.
func readSkillFrontmatterName(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(raw), "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return "", nil
	}
	for i := 1; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		if line == "---" {
			return "", nil
		}
		const prefix = "name:"
		if strings.HasPrefix(line, prefix) {
			val := strings.TrimSpace(line[len(prefix):])
			val = strings.Trim(val, `"' `)
			return val, nil
		}
	}
	return "", nil
}

// copyDir mirrors a directory tree. Filesystem-only, no symlinks.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}
