// Package harness runs one OpenNeko agent turn as the Ax backend. The
// ax-harness binary and the openneko binary's ax-harness role both call Main.
package harness

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/open-neko/openneko/apps/ax-harness/internal/agent"
	"github.com/open-neko/openneko/apps/ax-harness/internal/catalog"
	"github.com/open-neko/openneko/apps/ax-harness/internal/command"
	"github.com/open-neko/openneko/apps/ax-harness/internal/localtool"
	"github.com/open-neko/openneko/apps/ax-harness/internal/mcp"
	"github.com/open-neko/openneko/apps/ax-harness/sandbox"
)

// The bridge's ask tool ends the turn until the operator answers.
var bridgeEffects = map[string]string{"interaction_ask_user_question": "pause"}

// Main runs one agent turn, reading the run from stdin and the environment.
// It exits the process when the turn ends.
func Main() {
	if len(os.Args) == 2 && os.Args[1] == "catalog" {
		exportCatalog()
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	environ := os.Environ()
	// Only the bridge may reach the broker.
	_ = os.Unsetenv("OPENNEKO_BROKER_URL")
	_ = os.Unsetenv("OPENNEKO_BROKER_TOKEN")
	code, err := Run(ctx, environ, os.Stdin, os.Stdout)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(code)
}

// Run runs one agent turn for a host that runs the harness in-process.
// environ takes the place of the process environment: it binds the run's
// scope, tools and model routes. Run reads the run specification from in,
// writes NDJSON events to out and returns the exit code the binary would.
// It reads no process state, so a host can run many turns at once.
func Run(ctx context.Context, environ []string, in io.Reader, out io.Writer) (int, error) {
	return RunTurn(ctx, Turn{Environ: environ, In: in, Out: out})
}

// Turn is one agent turn run in-process.
type Turn struct {
	// Environ binds the run's scope, tools and model routes. Model
	// credentials in it are ignored when Secrets is set.
	Environ []string
	// Secrets returns the model credential a variable names, from the host.
	// The run's tools and terminal never see these values in Environ.
	Secrets func(name string) string
	// Bridge carries the OpenNeko tools as MCP over a stream, in place of
	// the bridge process that OPENNEKO_MCP_BRIDGE names.
	Bridge io.ReadWriteCloser
	In     io.Reader
	Out    io.Writer
}

// RunTurn runs turn; see Run.
func RunTurn(ctx context.Context, turn Turn) (int, error) {
	environ := turn.Environ
	if turn.Secrets != nil {
		environ = withSecrets(environ, turn.Secrets)
	}
	in, out := turn.In, turn.Out
	getenv := func(name string) string { return envValue(environ, name) }
	if err := validateScope(getenv("OPENNEKO_MCP_ORG_ID"), getenv("OPENNEKO_MCP_THREAD_ID")); err != nil {
		return 2, err
	}
	var tools agent.Tools
	var closers []func() error
	closeAll := func() error {
		var first error
		for _, closeTool := range closers {
			if err := closeTool(); err != nil && first == nil {
				first = err
			}
		}
		return first
	}
	failed := func(err error) (int, error) {
		_ = closeAll()
		return 2, err
	}
	if turn.Bridge != nil {
		caps, closeBridge, err := mcp.ConnectStream(ctx, turn.Bridge, "mcp_neko_", bridgeEffects)
		if err != nil {
			return failed(fmt.Errorf("OpenNeko bridge unavailable: %w", err))
		}
		tools.Capabilities = append(tools.Capabilities, caps...)
		closers = append(closers, closeBridge)
	} else if bridge := getenv("OPENNEKO_MCP_BRIDGE"); bridge != "" {
		caps, closeBridge, err := mcp.Connect(ctx, bridgeCommand(bridge, getenv("OPENNEKO_MCP_SERVERS"), environ), "mcp_neko_", bridgeEffects)
		if err != nil {
			return failed(fmt.Errorf("OpenNeko bridge unavailable: %w", err))
		}
		tools.Capabilities = append(tools.Capabilities, caps...)
		closers = append(closers, closeBridge)
	}
	workspace := getenv("OPENNEKO_HARNESS_WORKSPACE_DIR")
	if workspace != "" {
		files, err := localtool.OpenFiles(workspace)
		if err != nil {
			return failed(fmt.Errorf("file workspace unavailable: %w", err))
		}
		tools.Capabilities = append(tools.Capabilities, files.Capabilities()...)
		closers = append(closers, files.Close)
	}
	if enabled := getenv("OPENNEKO_HARNESS_SHELL"); enabled != "" {
		if enabled != "1" || workspace == "" {
			return failed(fmt.Errorf("the terminal needs OPENNEKO_HARNESS_SHELL=1 and OPENNEKO_HARNESS_WORKSPACE_DIR"))
		}
		shell, err := localtool.NewShell(workspace, shellStripList(getenv))
		if err != nil {
			return failed(fmt.Errorf("terminal unavailable: %w", err))
		}
		shell.SetEnviron(environ)
		if socket := getenv("OPENNEKO_SANDBOX_SOCKET"); socket != "" {
			shell.SetExecutor(sandbox.NewClient(socket, workspace))
			closers = append(closers, shell.Close)
		}
		shell.SetSkillsRoot(getenv("OPENNEKO_MCP_SKILLS_ROOT"))
		timeout, err := intEnv(getenv, "OPENNEKO_HARNESS_TERMINAL_TIMEOUT_SECONDS", 180)
		if err != nil {
			return failed(err)
		}
		maxOutput, err := intEnv(getenv, "OPENNEKO_HARNESS_TERMINAL_MAX_OUTPUT", 50_000)
		if err != nil {
			return failed(err)
		}
		if err := shell.SetLimits(timeout, maxOutput); err != nil {
			return failed(err)
		}
		tools.Capabilities = append(tools.Capabilities, shell.Capability())
	}
	if hosts := getenv("OPENNEKO_HARNESS_WEB_HOSTS"); hosts != "" {
		web, err := localtool.OpenWeb(hosts)
		if err != nil {
			return failed(err)
		}
		tools.Capabilities = append(tools.Capabilities, web.Capability())
	}
	if dir := getenv("OPENNEKO_HARNESS_UPLOADS_DIR"); dir != "" {
		uploads, err := localtool.OpenFiles(dir)
		if err != nil {
			return failed(fmt.Errorf("upload workspace unavailable: %w", err))
		}
		tools.Capabilities = append(tools.Capabilities, uploads.UploadCapabilities()...)
		closers = append(closers, uploads.Close)
	}
	if enabled := getenv("OPENNEKO_HARNESS_SKILLS_READ"); enabled != "" {
		if enabled != "1" {
			return failed(fmt.Errorf("invalid skill capability binding"))
		}
		skills, err := localtool.OpenFiles(getenv("OPENNEKO_MCP_SKILLS_ROOT"))
		if err != nil {
			return failed(fmt.Errorf("skill workspace unavailable: %w", err))
		}
		closers = append(closers, skills.Close)
		if tools.Skills, err = skills.Skills(); err != nil {
			return failed(fmt.Errorf("staged skills unavailable: %w", err))
		}
	}
	if child := getenv("OPENNEKO_HARNESS_CHILD_TOOLS"); child != "" {
		tools.ChildTools = strings.Split(child, ",")
	}
	return command.Execute(ctx, getenv, in, out, tools, closeAll)
}

// withSecrets replaces every model credential variable in environ with the
// host's value, so a credential can come only from the host.
func withSecrets(environ []string, secrets func(string) string) []string {
	names := command.KeyEnvNames(func(name string) string { return envValue(environ, name) })
	held := map[string]bool{}
	for _, name := range names {
		if name != "HARNESS_MODEL_ROUTES" {
			held[name] = true
		}
	}
	out := make([]string, 0, len(environ)+len(held))
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if !held[name] {
			out = append(out, entry)
		}
	}
	for _, name := range names {
		if held[name] {
			if value := secrets(name); value != "" {
				out = append(out, name+"="+value)
			}
			delete(held, name)
		}
	}
	return out
}

func envValue(environ []string, name string) string {
	value := ""
	for _, entry := range environ {
		if key, v, ok := strings.Cut(entry, "="); ok && key == name {
			value = v
		}
	}
	return value
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
func intEnv(getenv func(string) string, name string, fallback int) (int, error) {
	raw := getenv(name)
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return n, nil
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

// exportCatalog writes the pinned Ax model prices for the host.
func exportCatalog() {
	version := catalog.Version()
	if version == "" {
		fail(fmt.Errorf("the Ax module version is not in the build information"))
	}
	out, err := catalog.Export(version)
	if err != nil {
		fail(err)
	}
	if _, err := os.Stdout.Write(out); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(2)
}
