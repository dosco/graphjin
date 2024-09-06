package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: go run script.go <version>")
		os.Exit(1)
	}

	newVersion := os.Args[1]
	versionRegex := regexp.MustCompile(`(github.com\/dosco\/graphjin\/[^\s]+) v[0-9]+\.[0-9]+\.[0-9]+`)
	replaceFormat := "$1 v" + newVersion

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Base(path) == "go.mod" {
			fmt.Println("Processing:", path)
			updateFile(path, versionRegex, replaceFormat)
		}
		return nil
	})
	if err != nil {
		fmt.Println("Error walking through files:", err)
		return
	}

	if err = gitCommands(newVersion); err != nil {
		fmt.Println("Error executing git commands:", err)
	}
}

func updateFile(filePath string, versionRegex *regexp.Regexp, replaceFormat string) {
	input, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Println("Failed to read file:", err)
		return
	}

	content := string(input)
	updatedContent := versionRegex.ReplaceAllString(content, replaceFormat)

	if updatedContent != content {
		err = os.WriteFile(filePath, []byte(updatedContent), 0644)
		if err != nil {
			fmt.Println("Failed to write updated content to file:", err)
		}
	}
}

func gitCommands(version string) error {
	steps := []struct {
		command string
		args    []string
	}{
		{"git", []string{"add", "."}},
		{"git", []string{"commit", "-m", fmt.Sprintf("Release v%s", version)}},
		{"git", []string{"tag", fmt.Sprintf("auth/v%s", version)}},
		{"git", []string{"tag", fmt.Sprintf("cmd/v%s", version)}},
		{"git", []string{"tag", fmt.Sprintf("conf/v%s", version)}},
		{"git", []string{"tag", fmt.Sprintf("core/v%s", version)}},
		{"git", []string{"tag", fmt.Sprintf("plugin/otel/v%s", version)}},
		{"git", []string{"tag", fmt.Sprintf("serv/v%s", version)}},
		{"git", []string{"tag", fmt.Sprintf("tests/v%s", version)}},
		{"git", []string{"tag", fmt.Sprintf("wasm/v%s", version)}},
		{"git", []string{"tag", fmt.Sprintf("v%s", version)}},
		{"git", []string{"push", "origin", "master"}},
		{"git", []string{"push", "--tags"}},
	}

	for _, step := range steps {
		cmd := exec.Command(step.command, step.args...)
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s %v failed: %w", step.command, step.args, err)
		}
	}

	return nil
}
