package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
)

const (
	defaultMCPServerURL = "http://localhost:8080/"
	graphjinMCPName     = "graphjin"
	claudeMCPServerName = "GraphJin"
)

var (
	lookPathFn                = exec.LookPath
	commandContextFn          = exec.CommandContext
	resolveGraphJinPathForMCP = resolveGraphJinBinaryPath
)

type mcpInstallOptions struct {
	Client     string
	Scope      string
	Server     string
	Yes        bool
	ConfigPath string
}

type mcpInstallResolveInput struct {
	Client      string
	ClientSet   bool
	Scope       string
	ScopeSet    bool
	Server      string
	ServerSet   bool
	Yes         bool
	Interactive bool
	ForceClient string
	PromptFn    func(kind, prompt string, options []string, defaultValue string) (string, error)
}

type codexInstallPlan struct {
	UseCLI         bool
	AddArgs        []string
	ConfigPath     string
	ScopeSupported bool
}

type codexServerConfig struct {
	Command string   `toml:"command,omitempty"`
	Args    []string `toml:"args,omitempty"`
	URL     string   `toml:"url,omitempty"`
}

type mcpInstallCommandConfig struct {
	Use         string
	Short       string
	Long        string
	ForceClient string
	HideClient  bool
}

func mcpInstallCmd() *cobra.Command {
	return newMCPInstallCommand(mcpInstallCommandConfig{
		Use:   "install",
		Short: "Guided MCP setup for Claude Code and OpenAI Codex",
		Long: `Install GraphJin MCP integration for Claude Code, OpenAI Codex, or both.

Defaults:
  client: codex
  scope:  project
  server: http://localhost:8080/

When run in an interactive terminal, this command asks guided questions unless --yes is used.`,
	})
}

func newMCPInstallCommand(cfg mcpInstallCommandConfig) *cobra.Command {
	var client string
	var scope string
	var server string
	var yes bool

	c := &cobra.Command{
		Use:   cfg.Use,
		Short: cfg.Short,
		Long:  cfg.Long,
		Run: func(cmd *cobra.Command, args []string) {
			absConfigPath, err := filepath.Abs(cpath)
			if err != nil {
				log.Fatalf("failed to get absolute config path: %s", err)
			}

			interactive := isInteractiveTTY() && !yes
			promptFn := promptChoiceFn(nil)
			if interactive {
				promptFn = promptChoiceFn(newPromptIO(cmd.InOrStdin(), cmd.OutOrStdout()))
			}

			opts, err := resolveInstallOptions(mcpInstallResolveInput{
				Client:      client,
				ClientSet:   cmd.Flags().Changed("client"),
				Scope:       scope,
				ScopeSet:    cmd.Flags().Changed("scope"),
				Server:      server,
				ServerSet:   cmd.Flags().Changed("server"),
				Yes:         yes,
				Interactive: interactive,
				ForceClient: cfg.ForceClient,
				PromptFn:    promptFn,
			})
			if err != nil {
				log.Fatalf("%s", err)
			}

			opts.ConfigPath = absConfigPath

			if err := validateInstallPrereqs(opts); err != nil {
				log.Fatalf("%s", err)
			}

			var codexPlan codexInstallPlan
			if usesCodex(opts.Client) {
				codexPlan, err = buildCodexInstallPlan(cmd, opts)
				if err != nil {
					log.Fatalf("failed to build codex install plan: %s", err)
				}
			}

			printResolvedInstallOptions(cmd.OutOrStdout(), opts)
			printInstallPlan(cmd.OutOrStdout(), opts, codexPlan)

			if interactive {
				ok, err := promptConfirm(newPromptIO(cmd.InOrStdin(), cmd.OutOrStdout()),
					"Proceed with MCP install?", false)
				if err != nil {
					log.Fatalf("failed to read confirmation: %s", err)
				}
				if !ok {
					log.Infof("Aborted")
					return
				}
			}

			if usesClaude(opts.Client) {
				if err := runClaudeInstall(cmd, opts); err != nil {
					log.Fatalf("Claude install failed: %s", err)
				}
			}

			if usesCodex(opts.Client) {
				if err := runCodexInstall(cmd, opts, codexPlan); err != nil {
					log.Fatalf("Codex install failed: %s", err)
				}
			}

			fmt.Fprintln(cmd.OutOrStdout(), "MCP install complete.")
		},
	}

	c.Flags().StringVar(&client, "client", "", "Target client: claude, codex, or both")
	c.Flags().StringVar(&scope, "scope", "", "Install scope: project, global, or local")
	c.Flags().StringVar(&server, "server", "", "HTTP MCP server URL (default http://localhost:8080/)")
	c.Flags().BoolVar(&yes, "yes", false, "Skip interactive prompts and confirmation")

	if cfg.HideClient {
		c.Flags().MarkHidden("client") //nolint:errcheck
	}

	return c
}

func resolveInstallOptions(in mcpInstallResolveInput) (mcpInstallOptions, error) {
	var opts mcpInstallOptions
	opts.Yes = in.Yes

	clientValue := in.Client
	if in.ForceClient != "" {
		clientValue = in.ForceClient
	} else if !in.ClientSet {
		if in.Interactive && in.PromptFn != nil {
			v, err := in.PromptFn(
				"client",
				"Select MCP target client",
				[]string{"codex", "claude", "both"},
				"codex",
			)
			if err != nil {
				return opts, err
			}
			clientValue = v
		} else {
			clientValue = "codex"
		}
	}

	scopeValue := in.Scope
	if !in.ScopeSet {
		if in.Interactive && in.PromptFn != nil {
			v, err := in.PromptFn(
				"scope",
				"Select install scope",
				[]string{"project", "global", "local"},
				"project",
			)
			if err != nil {
				return opts, err
			}
			scopeValue = v
		} else {
			scopeValue = "project"
		}
	}

	serverValue := in.Server
	if !in.ServerSet {
		serverValue = defaultMCPServerURL
	}
	if serverValue == "" {
		serverValue = defaultMCPServerURL
	}

	client, err := normalizeInstallClient(clientValue)
	if err != nil {
		return opts, err
	}

	scope, err := normalizeInstallScope(scopeValue)
	if err != nil {
		return opts, err
	}

	if _, err := url.ParseRequestURI(serverValue); err != nil {
		return opts, fmt.Errorf("invalid --server %q: %w", serverValue, err)
	}

	opts.Client = client
	opts.Scope = scope
	opts.Server = serverValue

	return opts, nil
}

func normalizeInstallClient(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "claude", "codex", "both":
		return v, nil
	default:
		return "", fmt.Errorf("invalid --client %q (valid: claude, codex, both)", v)
	}
}

func normalizeInstallScope(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "project", "global", "local":
		return v, nil
	case "user":
		return "global", nil
	default:
		return "", fmt.Errorf("invalid --scope %q (valid: project, global, local)", v)
	}
}

func validateInstallPrereqs(opts mcpInstallOptions) error {
	if usesClaude(opts.Client) {
		if _, err := lookPathFn("claude"); err != nil {
			return errors.New("Claude CLI not found in PATH. Install Claude Code CLI or use --client codex")
		}
	}

	if usesCodex(opts.Client) {
		if _, err := lookPathFn("codex"); err != nil {
			return errors.New("Codex CLI not found in PATH. Install OpenAI Codex CLI or use --client claude")
		}
	}

	return nil
}

func buildCodexInstallPlan(cmd *cobra.Command, opts mcpInstallOptions) (codexInstallPlan, error) {
	supportsScope, err := detectCodexScopeSupport(cmd)
	if err != nil {
		// For compatibility with older CLIs or unusual output, continue with no-scope fallback.
		log.Infof("Codex scope detection failed, using compatibility mode: %s", err)
		supportsScope = false
	}

	wd, err := os.Getwd()
	if err != nil {
		return codexInstallPlan{}, err
	}

	if supportsScope {
		return codexInstallPlan{
			UseCLI:         true,
			ScopeSupported: supportsScope,
			AddArgs:        buildCodexAddArgs(opts, supportsScope),
		}, nil
	}

	targetPath, err := codexConfigTargetPath(opts.Scope, wd)
	if err != nil {
		return codexInstallPlan{}, err
	}

	return codexInstallPlan{
		UseCLI:         false,
		ScopeSupported: supportsScope,
		ConfigPath:     targetPath,
	}, nil
}

func detectCodexScopeSupport(cmd *cobra.Command) (bool, error) {
	out, err := runExternalCommandOutput(cmd, "codex", "mcp", "add", "--help")
	if err != nil && strings.TrimSpace(out) == "" {
		return false, err
	}
	return codexHelpHasScope(out), nil
}

func codexHelpHasScope(helpText string) bool {
	return strings.Contains(helpText, "--scope")
}

func buildCodexAddArgs(opts mcpInstallOptions, includeScope bool) []string {
	args := []string{"mcp", "add", graphjinMCPName}
	if includeScope {
		args = append(args, "--scope", codexScopeValue(opts.Scope))
	}

	args = append(args, "--url", opts.Server)
	return args
}

func codexScopeValue(scope string) string {
	switch scope {
	case "global":
		return "user"
	default:
		return scope
	}
}

func codexConfigTargetPath(scope, wd string) (string, error) {
	switch scope {
	case "project", "local":
		return filepath.Join(wd, ".codex", "config.toml"), nil
	case "global":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".codex", "config.toml"), nil
	default:
		return "", fmt.Errorf("unsupported fallback scope %q", scope)
	}
}

func runClaudeInstall(cmd *cobra.Command, opts mcpInstallOptions) error {
	return runClaudeMCPAddInstall(cmd, opts)
}

func runClaudeMCPAddInstall(cmd *cobra.Command, opts mcpInstallOptions) error {
	graphjinPath, err := resolveGraphJinPathForMCP()
	if err != nil {
		return err
	}

	claudeScope := normalizeClaudeScope(opts.Scope)
	// Best effort: remove existing config to allow deterministic updates.
	_ = runExternalCommand(cmd, "claude", "mcp", "remove", "--scope", claudeScope, claudeMCPServerName)

	addArgs := []string{"mcp", "add", "--scope", claudeScope, claudeMCPServerName, "--", graphjinPath}
	addArgs = append(addArgs, buildClaudeMCPServerArgs(opts)...)
	return runExternalCommand(cmd, "claude", addArgs...)
}

func resolveGraphJinBinaryPath() (string, error) {
	if p, err := lookPathFn("graphjin"); err == nil {
		return p, nil
	}

	p, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to locate graphjin executable: %w", err)
	}
	return p, nil
}

func buildClaudeMCPServerArgs(opts mcpInstallOptions) []string {
	return []string{"mcp", "--server", opts.Server}
}

func normalizeClaudeScope(scope string) string {
	switch scope {
	case "global":
		return "user"
	default:
		return scope
	}
}

func runCodexInstall(cmd *cobra.Command, opts mcpInstallOptions, plan codexInstallPlan) error {
	if plan.UseCLI {
		return runExternalCommand(cmd, "codex", plan.AddArgs...)
	}

	entry := codexServerConfigFromOptions(opts)
	return writeCodexConfig(plan.ConfigPath, graphjinMCPName, entry)
}

func codexServerConfigFromOptions(opts mcpInstallOptions) codexServerConfig {
	return codexServerConfig{URL: opts.Server}
}

func graphjinCommandForMCP() string {
	p, err := resolveGraphJinPathForMCP()
	if err != nil || p == "" {
		return "graphjin"
	}
	return p
}

func writeCodexConfig(path, serverName string, cfg codexServerConfig) error {
	var current []byte
	if b, err := os.ReadFile(path); err == nil {
		current = b
	} else if !os.IsNotExist(err) {
		return err
	}

	updated, err := upsertCodexConfig(current, serverName, cfg)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, updated, 0o600)
}

func upsertCodexConfig(data []byte, serverName string, cfg codexServerConfig) ([]byte, error) {
	root := map[string]any{}
	if len(bytes.TrimSpace(data)) > 0 {
		if err := toml.Unmarshal(data, &root); err != nil {
			return nil, err
		}
	}

	mcpServers := toStringAnyMap(root["mcp_servers"])
	if mcpServers == nil {
		mcpServers = map[string]any{}
	}

	server := map[string]any{}
	if cfg.Command != "" {
		server["command"] = cfg.Command
	}
	if len(cfg.Args) != 0 {
		server["args"] = cfg.Args
	}
	if cfg.URL != "" {
		server["url"] = cfg.URL
	}
	mcpServers[serverName] = server
	root["mcp_servers"] = mcpServers

	return toml.Marshal(root)
}

func toStringAnyMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func usesClaude(client string) bool {
	return client == "claude" || client == "both"
}

func usesCodex(client string) bool {
	return client == "codex" || client == "both"
}

func printResolvedInstallOptions(w io.Writer, opts mcpInstallOptions) {
	fmt.Fprintf(w, "Resolved options:\n")
	fmt.Fprintf(w, "  client: %s\n", opts.Client)
	fmt.Fprintf(w, "  scope:  %s\n", opts.Scope)
	fmt.Fprintf(w, "  server: %s\n", opts.Server)
	fmt.Fprintf(w, "\n")
}

func printInstallPlan(w io.Writer, opts mcpInstallOptions, codexPlan codexInstallPlan) {
	fmt.Fprintf(w, "Planned actions:\n")

	if usesClaude(opts.Client) {
		claudeScope := normalizeClaudeScope(opts.Scope)
		fmt.Fprintf(w, "  - claude mcp add --scope %s %s -- graphjin %s\n", claudeScope, claudeMCPServerName, strings.Join(buildClaudeMCPServerArgs(opts), " "))
	}

	if usesCodex(opts.Client) {
		if codexPlan.UseCLI {
			fmt.Fprintf(w, "  - codex %s\n", strings.Join(codexPlan.AddArgs, " "))
		} else {
			fmt.Fprintf(w, "  - update %s (set mcp_servers.%s)\n", codexPlan.ConfigPath, graphjinMCPName)
		}
	}

	fmt.Fprintf(w, "\n")
}

func runExternalCommand(cmd *cobra.Command, name string, args ...string) error {
	c := commandContextFn(cmd.Context(), name, args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func runExternalCommandOutput(cmd *cobra.Command, name string, args ...string) (string, error) {
	c := commandContextFn(cmd.Context(), name, args...)
	b, err := c.CombinedOutput()
	return string(b), err
}

type promptIO struct {
	in  *bufio.Reader
	out io.Writer
}

func newPromptIO(in io.Reader, out io.Writer) *promptIO {
	return &promptIO{
		in:  bufio.NewReader(in),
		out: out,
	}
}

func promptChoiceFn(pio *promptIO) func(kind, prompt string, options []string, defaultValue string) (string, error) {
	if pio == nil {
		return nil
	}

	return func(kind, prompt string, options []string, defaultValue string) (string, error) {
		return promptChoice(pio, prompt, options, defaultValue)
	}
}

func promptChoice(pio *promptIO, prompt string, options []string, defaultValue string) (string, error) {
	if len(options) == 0 {
		return "", errors.New("prompt options cannot be empty")
	}

	var defaultIndex int
	for i, option := range options {
		if option == defaultValue {
			defaultIndex = i
			break
		}
	}

	for {
		fmt.Fprintf(pio.out, "%s\n", prompt)
		for i, option := range options {
			marker := " "
			if i == defaultIndex {
				marker = "*"
			}
			fmt.Fprintf(pio.out, "  %d) [%s] %s\n", i+1, marker, option)
		}
		fmt.Fprintf(pio.out, "Select option (default %d): ", defaultIndex+1)

		line, err := pio.in.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return options[defaultIndex], nil
		}

		if index, err := strconv.Atoi(line); err == nil {
			if index > 0 && index <= len(options) {
				return options[index-1], nil
			}
		}

		for _, option := range options {
			if strings.EqualFold(line, option) {
				return option, nil
			}
		}

		fmt.Fprintln(pio.out, "Invalid selection, try again.")
	}
}

func promptConfirm(pio *promptIO, prompt string, defaultYes bool) (bool, error) {
	defaultLabel := "y/N"
	if defaultYes {
		defaultLabel = "Y/n"
	}

	fmt.Fprintf(pio.out, "%s [%s]: ", prompt, defaultLabel)
	line, err := pio.in.ReadString('\n')
	if err != nil {
		return false, err
	}
	line = strings.ToLower(strings.TrimSpace(line))
	if line == "" {
		return defaultYes, nil
	}
	return line == "y" || line == "yes", nil
}

func isInteractiveTTY() bool {
	si, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	so, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (si.Mode()&os.ModeCharDevice) != 0 && (so.Mode()&os.ModeCharDevice) != 0
}
