// Command ax-harness runs one OpenNeko agent turn as the Ax backend.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/open-neko/openneko/apps/ax-harness/internal/agent"
	"github.com/open-neko/openneko/apps/ax-harness/internal/command"
	"github.com/open-neko/openneko/apps/ax-harness/internal/localtool"
	"github.com/open-neko/openneko/apps/ax-harness/internal/mcp"
)

// The bridge's ask tool ends the turn until the operator answers.
var bridgeEffects = map[string]string{"interaction_ask_user_question": "pause"}

func main() {
	if err := validateScope(os.Getenv("OPENNEKO_MCP_ORG_ID"), os.Getenv("OPENNEKO_MCP_THREAD_ID")); err != nil {
		fail(err)
	}
	var tools agent.Tools
	var closers []func() error
	if bridge := os.Getenv("OPENNEKO_MCP_BRIDGE"); bridge != "" {
		caps, closeBridge, err := mcp.Connect(context.Background(), bridgeCommand(bridge, os.Getenv("OPENNEKO_MCP_SERVERS"), os.Environ()), "mcp_neko_", bridgeEffects)
		if err != nil {
			fail(fmt.Errorf("OpenNeko bridge unavailable: %w", err))
		}
		tools.Capabilities = append(tools.Capabilities, caps...)
		closers = append(closers, closeBridge)
	}
	// Only the bridge may reach the broker.
	_ = os.Unsetenv("OPENNEKO_BROKER_URL")
	_ = os.Unsetenv("OPENNEKO_BROKER_TOKEN")
	workspace := os.Getenv("OPENNEKO_HARNESS_WORKSPACE_DIR")
	if workspace != "" {
		files, err := localtool.OpenFiles(workspace)
		if err != nil {
			fail(fmt.Errorf("file workspace unavailable: %w", err))
		}
		tools.Capabilities = append(tools.Capabilities, files.Capabilities()...)
		closers = append(closers, files.Close)
	}
	if enabled := os.Getenv("OPENNEKO_HARNESS_SHELL"); enabled != "" {
		if enabled != "1" || workspace == "" {
			fail(fmt.Errorf("the terminal needs OPENNEKO_HARNESS_SHELL=1 and OPENNEKO_HARNESS_WORKSPACE_DIR"))
		}
		shell, err := localtool.OpenShell(workspace, shellStripList(os.Getenv))
		if err != nil {
			fail(fmt.Errorf("terminal unavailable: %w", err))
		}
		if err := shell.SetLimits(intEnv("OPENNEKO_HARNESS_TERMINAL_TIMEOUT_SECONDS", 180), intEnv("OPENNEKO_HARNESS_TERMINAL_MAX_OUTPUT", 50_000)); err != nil {
			fail(err)
		}
		tools.Capabilities = append(tools.Capabilities, shell.Capability())
	}
	if hosts := os.Getenv("OPENNEKO_HARNESS_WEB_HOSTS"); hosts != "" {
		web, err := localtool.OpenWeb(hosts)
		if err != nil {
			fail(err)
		}
		tools.Capabilities = append(tools.Capabilities, web.Capability())
	}
	if dir := os.Getenv("OPENNEKO_HARNESS_UPLOADS_DIR"); dir != "" {
		uploads, err := localtool.OpenFiles(dir)
		if err != nil {
			fail(fmt.Errorf("upload workspace unavailable: %w", err))
		}
		tools.Capabilities = append(tools.Capabilities, uploads.UploadCapabilities()...)
		closers = append(closers, uploads.Close)
	}
	if enabled := os.Getenv("OPENNEKO_HARNESS_SKILLS_READ"); enabled != "" {
		if enabled != "1" {
			fail(fmt.Errorf("invalid skill capability binding"))
		}
		skills, err := localtool.OpenFiles(os.Getenv("OPENNEKO_MCP_SKILLS_ROOT"))
		if err != nil {
			fail(fmt.Errorf("skill workspace unavailable: %w", err))
		}
		if tools.Skills, err = skills.Skills(); err != nil {
			fail(fmt.Errorf("staged skills unavailable: %w", err))
		}
		closers = append(closers, skills.Close)
	}
	if child := os.Getenv("OPENNEKO_HARNESS_CHILD_TOOLS"); child != "" {
		tools.ChildTools = strings.Split(child, ",")
	}
	command.MainWithToolsAndCleanup(tools, func() error {
		var first error
		for _, closeTool := range closers {
			if err := closeTool(); err != nil && first == nil {
				first = err
			}
		}
		return first
	})
}

// bridgeCommand starts the bridge with a clean environment: PATH, the bridge
// settings, the broker binding and the egress proxy settings.
func bridgeCommand(bridge, servers string, environ []string) *exec.Cmd {
	args := []string{bridge}
	if strings.HasSuffix(bridge, ".ts") {
		args = append([]string{"--import", "tsx"}, args...)
	}
	if servers != "" {
		args = append(args, servers)
	}
	cmd := exec.Command("node", args...)
	cmd.Dir = filepath.Dir(bridge)
	proxies := map[string]bool{"ALL_PROXY": true, "HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true, "no_proxy": true, "NODE_USE_ENV_PROXY": true}
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if name == "PATH" || strings.HasPrefix(name, "OPENNEKO_MCP_") || name == "OPENNEKO_BROKER_URL" || name == "OPENNEKO_BROKER_TOKEN" || proxies[name] {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	return cmd
}

// shellStripList names the variables a terminal command must not inherit:
// the broker binding and every model credential.
func shellStripList(getenv func(string) string) []string {
	return append([]string{"OPENNEKO_BROKER_URL", "OPENNEKO_BROKER_TOKEN"}, command.KeyEnvNames(getenv)...)
}

// intEnv reads a host limit; an unset variable keeps the default and a malformed one fails the run.
func intEnv(name string, fallback int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		fail(fmt.Errorf("invalid %s", name))
	}
	return n
}

func validateScope(orgID, threadID string) error {
	valid := func(id string) bool {
		return id != "" && len(id) <= 128 && strings.TrimSpace(id) == id && !strings.ContainsAny(id, "\r\n\x00")
	}
	if !valid(orgID) || !valid(threadID) {
		return fmt.Errorf("missing or invalid OpenNeko admission scope")
	}
	return nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(2)
}
