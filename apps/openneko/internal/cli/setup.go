package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/open-neko/neko/apps/openneko/internal/compose"
	"github.com/open-neko/neko/apps/openneko/internal/config"
	"github.com/open-neko/neko/apps/openneko/internal/plugin/marketplace"
	"github.com/open-neko/neko/apps/openneko/internal/preflight"
	"github.com/open-neko/neko/apps/openneko/internal/prompt"
	"github.com/open-neko/neko/apps/openneko/internal/setup"
	"github.com/open-neko/neko/apps/openneko/internal/ui"
)

func newSetupCmd() *cobra.Command {
	var (
		mode           string
		verbose        bool
		skipOnboarding bool
		webPort        int
		openShellPort  int
		webBindAddress string
		dockerSubnet   string

		// Headless onboarding overrides. Setting any credential flag opts the
		// run into headless mode (no prompts), so CI and the demo-install skill
		// can configure without a browser.
		adminPassword    string
		legacyBackend    string
		provider         string
		providerKey      string
		model            string
		dataURL          string
		researchProvider string
		researchKey      string
		noResearch       bool

		skipPlugins bool
		pluginsCSV  string
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Guided install: preflight, bring up the stack, then configure",
		Long: `Guided first-run install.

  1. Preflight — Docker daemon up, host supported, required ports free.
  2. Bring up the stack (same staged flow as ` + "`openneko start`" + `).
  3. Configure — admin password, data source, agent + provider, research.

Step 3 runs in the terminal, or pass --skip-onboarding (or just choose "browser"
at the prompt) to finish at the web UI. Credential flags
(--admin-password/--provider/--provider-key/…) run step 3 headless for CI.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateLegacyAgentBackend(legacyBackend); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			settings, err := prepareSetupInstallation(ctx, cmd, mode, setupHostOptions{
				webPort:             webPort,
				webPortChanged:      cmd.Flags().Changed("port"),
				openShellPort:       openShellPort,
				openShellChanged:    cmd.Flags().Changed("openshell-port"),
				webBindAddress:      webBindAddress,
				webBindChanged:      cmd.Flags().Changed("bind-address"),
				dockerSubnet:        dockerSubnet,
				dockerSubnetChanged: cmd.Flags().Changed("docker-subnet"),
			})
			if err != nil {
				return err
			}
			m := compose.Mode(settings.Mode)
			baseURL := webBaseURL()
			client := setup.NewClient(baseURL)
			alreadyUp := client.Ready(ctx)

			subtitle := string(m) + " mode"
			if settings.Instance != "" {
				subtitle = settings.Instance + " · " + subtitle
			}
			if alreadyUp {
				subtitle += " · stack already running"
			}
			ui.Banner(out, subtitle)

			// 1 + 2. Preflight then bring-up — skipped if the stack already
			// answers (re-running setup against a live install jumps straight
			// to configuration). Port checks only run for a fresh bring-up,
			// since a live OpenNeko legitimately holds those ports.
			if alreadyUp {
				ui.Info(out, "Existing OpenNeko detected — skipping preflight and bring-up.")
				// The wizard configures WHATEVER stack answers on this port.
				// `setup --mode demo` next to a live prod stack silently
				// configured prod with demo defaults; refuse the mismatch.
				project := runningWebProject(strconv.Itoa(settings.WebPort))
				if settings.Instance != "" && project == "" {
					return fmt.Errorf("%s is already answering, but it is not the web container for instance %q; choose another --port", baseURL, settings.Instance)
				}
				if project != "" {
					detectedMode, detectedInstance, ok := compose.IdentityFromProjectName(project)
					if project == "openneko" {
						detectedMode, detectedInstance, ok = compose.ModeProd, "", true
					}
					if !ok && settings.Instance != "" {
						return fmt.Errorf("the stack answering %s has unrecognized Compose project %q", baseURL, project)
					}
					if ok && (detectedMode != m || detectedInstance != settings.Instance) {
						return fmt.Errorf(
							"the stack answering %s is %q (instance %q, mode %s), not requested instance %q in mode %s",
							baseURL, project, detectedInstance, detectedMode, settings.Instance, m,
						)
					}
				}
			} else {
				if err := runPreflight(out); err != nil {
					return err
				}
				ui.Info(out, "Bringing up the stack…")
				if err := bringUpStack(ctx, cmd, m, bringUpOptions{detach: true, quiet: !verbose}); err != nil {
					return err
				}
				if err := ui.Spin("Waiting for the web app", func() error { return client.WaitReady(ctx, 120*time.Second) }); err != nil {
					return err
				}
			}
			ui.Success(out, "web app ready")

			// 3. Onboarding.
			interactive := prompt.IsInteractive()
			headless := adminPassword != "" || provider != "" || providerKey != "" ||
				legacyBackend != "" || model != "" || researchProvider != "" || researchKey != ""

			if skipOnboarding {
				ui.Info(out, "Stack is up. Finish setup in your browser:")
				fmt.Fprintln(out, "  "+baseURL)
				return nil
			}
			if !interactive && !headless {
				ui.Info(out, "No TTY and no setup flags — finish setup in your browser:")
				fmt.Fprintln(out, "  "+baseURL)
				return nil
			}

			cfg := setup.Config{
				Mode:             string(m),
				BaseURL:          baseURL,
				Headless:         headless || !interactive,
				AdminPassword:    adminPassword,
				Provider:         provider,
				ProviderKey:      providerKey,
				Model:            model,
				DataURL:          dataURL,
				ResearchProvider: researchProvider,
				ResearchKey:      researchKey,
				NoResearch:       noResearch,
			}
			outcome, runErr := setup.Run(ctx, client, out, cfg)

			// Persist the rotated password to the host config (the source the
			// CLI's own `neko` connection reads on later `start`/`migrate`
			// runs). The web wizard writes it only to the in-container config
			// volume, so without this the next host-side `neko` connection would
			// fall back to the stale bootstrap default. This must happen even
			// when a LATER wizard step failed — the remote rotation has already
			// committed by then, and dropping it here locked the host CLI out.
			// The gateway itself is unaffected by the rotation — it runs on its
			// dedicated `openshell` role (see ensureOpenShellGatewayRole), so no
			// gateway restart is needed.
			if outcome.PasswordSet != "" {
				if err := config.WriteLocalDatabasePasswords("", outcome.PasswordSet, outcome.PasswordSet); err != nil {
					ui.Info(out, "warning: couldn't persist the DB password to the host config: %v", err)
				}
			}
			if runErr != nil {
				return runErr
			}

			if outcome.Configured {
				if !skipPlugins {
					if err := offerPluginInstall(ctx, out, pluginsCSV, interactive && !cfg.Headless); err != nil {
						return err
					}
				}
				ui.CompletionBox(out,
					ui.OK()+" Setup complete.",
					"",
					"Next: open "+baseURL+"/onboarding",
					"      to describe your business.",
				)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "prod", "Stack mode: prod|dev|demo")
	cmd.Flags().IntVar(&webPort, "port", 0, "Host port for the web app (named instances auto-select when omitted)")
	cmd.Flags().IntVar(&openShellPort, "openshell-port", 0, "Loopback host port for the OpenShell gateway (named instances auto-select when omitted)")
	cmd.Flags().StringVar(&webBindAddress, "bind-address", "", "IPv4 address for the web listener (default 0.0.0.0; use 127.0.0.1 behind a reverse proxy)")
	cmd.Flags().StringVar(&dockerSubnet, "docker-subnet", "", "Private IPv4 /24 for this instance (named instances auto-select when omitted)")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Stream full image-pull output during bring-up")
	cmd.Flags().BoolVar(&skipOnboarding, "skip-onboarding", false, "Bring up the stack only; finish configuration in the browser")
	cmd.Flags().StringVar(&adminPassword, "admin-password", "", "Headless: admin database password")
	cmd.Flags().StringVar(&legacyBackend, "backend", "", "Deprecated: Ax is the only agent runtime")
	_ = cmd.Flags().MarkDeprecated("backend", "Ax is now the only agent runtime")
	cmd.Flags().StringVar(&provider, "provider", "", "Headless: primary model provider")
	cmd.Flags().StringVar(&providerKey, "provider-key", "", "Headless: primary provider API key")
	cmd.Flags().StringVar(&model, "model", "", "Headless: primary model (default: provider default)")
	cmd.Flags().StringVar(&dataURL, "data-url", "", "GraphJin base URL (default: per-mode)")
	cmd.Flags().StringVar(&researchProvider, "research-provider", "", "Headless: research provider")
	cmd.Flags().StringVar(&researchKey, "research-key", "", "Headless: research provider API key")
	cmd.Flags().BoolVar(&noResearch, "no-research", false, "Headless: leave industry research disabled")
	cmd.Flags().BoolVar(&skipPlugins, "skip-plugins", false, "Skip the optional first-party plugin step")
	cmd.Flags().StringVar(&pluginsCSV, "plugins", "", "Install these first-party plugins non-interactively (comma-separated)")
	return cmd
}

func validateLegacyAgentBackend(value string) error {
	switch strings.TrimSpace(value) {
	case "", "ax":
		return nil
	default:
		return fmt.Errorf("unsupported --backend %q: Ax is the only agent runtime", value)
	}
}

// offerPluginInstall lets the operator install (and configure) first-party
// plugins from the official marketplace as a final, optional setup step. Each
// install reuses `openneko install` via the binary itself, so it runs through
// the worker proxy AND its env-prompt — selecting a plugin prompts for and
// persists that plugin's API keys/tokens. csv (from --plugins) drives a
// non-interactive selection; otherwise the operator picks from a list.
func offerPluginInstall(ctx context.Context, out io.Writer, csv string, interactive bool) error {
	preselected := splitCSV(csv)
	if len(preselected) == 0 && !interactive {
		return nil
	}
	var mp *marketplace.Marketplace
	if err := ui.Spin("Loading the plugin marketplace", func() error {
		var e error
		mp, e = marketplace.NewClient().Fetch(ctx, marketplace.OfficialURL)
		return e
	}); err != nil {
		ui.Info(out, "Skipping plugins — couldn't reach the marketplace: %v", err)
		return nil
	}
	if mp == nil || len(mp.Plugins) == 0 {
		return nil
	}

	var chosen []string
	if len(preselected) > 0 {
		chosen = matchPlugins(preselected, mp.Plugins)
	} else {
		opts := make([]huh.Option[string], len(mp.Plugins))
		for i, p := range mp.Plugins {
			label := p.Name
			if p.Title != "" {
				label = p.Name + " — " + p.Title
			}
			opts[i] = huh.NewOption(label, p.Name)
		}
		form := huh.NewForm(huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Install first-party plugins?").
				Description("space toggles · enter confirms · each prompts for its own keys").
				Options(opts...).
				Value(&chosen),
		)).WithTheme(ui.Theme())
		if err := form.Run(); err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				return nil
			}
			return err
		}
	}
	if len(chosen) == 0 {
		return nil
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}
	for _, name := range chosen {
		ui.Info(out, "Installing %s…", name)
		ic := exec.CommandContext(ctx, self, "install", name)
		ic.Stdin = os.Stdin
		ic.Stdout = os.Stdout
		ic.Stderr = os.Stderr
		if err := ic.Run(); err != nil {
			ui.Failure(out, "%s install failed: %v (continuing)", name, err)
		}
	}
	return nil
}

func splitCSV(s string) []string {
	var out []string
	for t := range strings.SplitSeq(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// resolvePluginSelection turns a user reply ("all", "1,3", or names) into a
// list of plugin names.
func resolvePluginSelection(sel string, plugins []marketplace.Plugin) []string {
	sel = strings.TrimSpace(sel)
	if sel == "" {
		return nil
	}
	if strings.EqualFold(sel, "all") {
		names := make([]string, len(plugins))
		for i, p := range plugins {
			names[i] = p.Name
		}
		return names
	}
	return matchPlugins(splitCSV(sel), plugins)
}

// matchPlugins resolves tokens (1-based indices or exact names) against the
// catalog, de-duplicating and preserving order.
func matchPlugins(tokens []string, plugins []marketplace.Plugin) []string {
	var out []string
	seen := map[string]bool{}
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	for _, tok := range tokens {
		if n, err := strconv.Atoi(tok); err == nil {
			if n >= 1 && n <= len(plugins) {
				add(plugins[n-1].Name)
			}
			continue
		}
		for _, p := range plugins {
			if p.Name == tok {
				add(p.Name)
				break
			}
		}
	}
	return out
}

// webBaseURL resolves the local web app URL from the published port (matching
// status.go's probeWeb).
// runningWebProject returns the compose project of the running web
// container, or "" when none is found / docker is unreachable. Used to
// detect which stack actually owns the port setup is about to configure.
func runningWebProject(port string) string {
	out, err := exec.Command(
		"docker", "ps",
		"--filter", "status=running",
		"--filter", "label=com.docker.compose.service=web",
		"--format", `{{.Label "com.docker.compose.project"}}|{{.Ports}}`,
	).Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		project, ports, ok := strings.Cut(strings.TrimSpace(line), "|")
		if ok && project != "" && strings.Contains(ports, ":"+port+"->8080/tcp") {
			return project
		}
	}
	return ""
}

func webBaseURL() string {
	port := strings.TrimSpace(os.Getenv("OPENNEKO_PORT"))
	if port == "" {
		port = "3000"
	}
	host := strings.TrimSpace(os.Getenv("OPENNEKO_WEB_BIND_ADDRESS"))
	if host == "" || host == "0.0.0.0" {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// runPreflight runs the host readiness checks and prints them, returning a
// non-nil (exit-coded) error if any hard check fails. Host failure exits 3 to
// match root.go's code map; other failures exit 1.
func runPreflight(out io.Writer) error {
	fmt.Fprintln(out, ui.Heading("Preflight"))
	checks := []preflight.Result{preflight.Host(), preflight.Docker()}
	checks = append(checks, preflight.Ports(preflight.DefaultPorts)...)
	checks = append(checks, preflight.DuplicateBinary())

	code := 0
	for _, c := range checks {
		switch c.Level {
		case preflight.Pass:
			ui.Success(out, "%s: %s", c.Name, c.Detail)
		case preflight.Warn:
			ui.Info(out, "! %s: %s", c.Name, c.Detail)
			if c.Remediation != "" {
				ui.Info(out, "    %s", c.Remediation)
			}
		case preflight.Fail:
			ui.Failure(out, "%s: %s", c.Name, c.Detail)
			if c.Remediation != "" {
				ui.Info(out, "    %s", c.Remediation)
			}
			if c.Name == "host" {
				code = 3
			} else if code == 0 {
				code = 1
			}
		}
	}
	if code != 0 {
		return WithExit(code, fmt.Errorf("preflight failed — fix the above and re-run `openneko setup`"))
	}
	return nil
}
