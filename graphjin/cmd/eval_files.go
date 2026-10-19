package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/dosco/graphjin/serv/v3"
)

// Writing the documents an authored task is graded against.
//
// A task whose answer lives in a file is only verifiable once the file exists.
// The engine writes it rather than the model, and it writes it into the project
// being authored against — the same directory the instance already booted from,
// so the running server serves it without being restarted.

// evalProjectConfigName resolves which config file a project boots from,
// mirroring how the evaluation environment picks one. Reading a different file
// than the instance did would resolve document roots against the wrong source
// list.
func evalProjectConfigName(projectPath string, target gjeval.Target) string {
	if target == gjeval.TargetDemo {
		if _, err := os.Stat(filepath.Join(projectPath, "dev.yml")); err == nil {
			return "dev"
		}
	}
	return serv.GetConfigName()
}

// evalFileSourceRoots maps each file source to the directory on disk it serves.
//
// A relative root resolves against the config directory, which is where the
// engine resolves it too.
func evalFileSourceRoots(projectPath string, target gjeval.Target) (map[string]string, error) {
	config, err := serv.ReadInConfig(filepath.Join(projectPath, evalProjectConfigName(projectPath, target)))
	if err != nil {
		return nil, err
	}
	roots := map[string]string{}
	for _, source := range config.Core.Sources {
		if !strings.EqualFold(source.Kind, "file") || strings.TrimSpace(source.Root) == "" {
			continue
		}
		root := source.Root
		if !filepath.IsAbs(root) {
			root = filepath.Join(projectPath, root)
		}
		roots[source.Name] = root
	}
	return roots, nil
}

// writeAuthoredFiles plants the documents authored tasks are graded against.
//
// It refuses to overwrite anything already there. A document that exists is
// somebody else's — either a real one this project ships or one from an earlier
// authoring pass — and quietly rewriting it would change what an existing task
// grades against without anything saying so.
func writeAuthoredFiles(projectPath string, target gjeval.Target, files []gjeval.AuthoredFile) ([]string, error) {
	if len(files) == 0 {
		return nil, nil
	}
	roots, err := evalFileSourceRoots(projectPath, target)
	if err != nil {
		return nil, err
	}
	written := make([]string, 0, len(files))
	for _, file := range files {
		root, ok := roots[file.FileRoot]
		if !ok {
			return nil, fmt.Errorf("no file source named %q is configured in %s", file.FileRoot, projectPath)
		}
		path := filepath.Join(root, file.Key)
		if _, err := os.Stat(path); err == nil {
			return nil, fmt.Errorf("%s already exists; authoring will not overwrite a document another task may be graded against", path)
		}
		if err := os.MkdirAll(root, 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(file.Contents), 0o600); err != nil {
			return nil, err
		}
		written = append(written, path)
	}
	return written, nil
}
