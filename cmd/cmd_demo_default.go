package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/dosco/graphjin/examples"
)

// demoDefaultPath is where the built-in demo project lives when --demo runs
// without --path. Delete the directory to re-extract a fresh copy; delete
// just <dir>/demo to reset demo data while keeping config edits.
const demoDefaultPath = "./graphjin-demo"

// resolveDemoPath picks the config directory for demo mode. With an explicit
// --path (or --config) the given directory is used as-is. Without one, the
// built-in saas-ops demo is extracted to ./graphjin-demo (first run)
// or reused from there, so a bare `graphjin serve --demo` boots a real,
// seeded example instead of an empty scaffold.
func resolveDemoPath(pathSet bool, out io.Writer) (string, error) {
	if pathSet {
		return cpath, nil
	}
	status := demoStatus{out: out}
	dir := demoDefaultPath
	shown := dir
	if abs, err := filepath.Abs(dir); err == nil {
		shown = abs
	}

	entries, err := os.ReadDir(dir)
	switch {
	case os.IsNotExist(err) || (err == nil && len(entries) == 0):
		if err := extractDefaultDemo(dir); err != nil {
			status.Emit("project", "failed", err.Error())
			return "", fmt.Errorf("failed to extract built-in demo to %s: %w", shown, err)
		}
		status.Emit("project", "created", fmt.Sprintf("built-in %s demo extracted to %s", examples.DefaultDemoRoot, shown))
	case err != nil:
		status.Emit("project", "failed", err.Error())
		return "", fmt.Errorf("cannot use %s for the built-in demo: %w", shown, err)
	case !hasDemoConfig(entries):
		err := fmt.Errorf("%s exists but has no GraphJin config; delete it to re-extract the built-in demo, or pass --path", shown)
		status.Emit("project", "failed", err.Error())
		return "", err
	default:
		status.Emit("project", "reused", fmt.Sprintf("%s (delete it for a fresh copy)", shown))
	}
	return dir, nil
}

// hasDemoConfig reports whether a directory listing contains one of the
// config files every extracted demo ships with.
func hasDemoConfig(entries []os.DirEntry) bool {
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		switch entry.Name() {
		case "dev.yml", "prod.yml", "agentic.yml":
			return true
		}
	}
	return false
}

// extractDefaultDemo writes the embedded default demo project into dstDir.
func extractDefaultDemo(dstDir string) error {
	src, err := fs.Sub(examples.DefaultDemoFS, examples.DefaultDemoRoot)
	if err != nil {
		return err
	}
	return fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." {
			return os.MkdirAll(dstDir, 0o755)
		}
		// scripts/ holds the repo smoke suite, which depends on the
		// surrounding examples/ tree; it is not part of the demo project.
		if d.IsDir() && p == "scripts" {
			return fs.SkipDir
		}
		dst := filepath.Join(dstDir, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
}
