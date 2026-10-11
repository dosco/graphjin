package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/open-neko/neko/apps/openneko/assets"
	"github.com/open-neko/neko/apps/openneko/internal/compose"
	"github.com/open-neko/neko/apps/openneko/internal/instance"
	opennekoversion "github.com/open-neko/neko/apps/openneko/internal/version"
)

func newUpgradeCmd() *cobra.Command {
	var mode string
	var imageVersion string
	var noPrune bool
	var stackOnly bool
	var cliOnly bool

	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade the OpenNeko CLI and stack together",
		Long: `Resolve an exact OpenNeko release, update the local CLI, re-execute that
new CLI, then pull and recreate the current stack from the same release. This
keeps the CLI's embedded Compose definitions aligned with the service images.

The CLI honors its installation owner: Homebrew installations are upgraded
through Homebrew; standalone installations use checksum-verified release
artifacts and atomic replacement. Use --stack-only or --cli-only for recovery
and specialized deployment workflows.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()
			return runUpgrade(ctx, cmd, upgradeOptions{
				mode:         mode,
				imageVersion: imageVersion,
				noPrune:      noPrune,
				stackOnly:    stackOnly,
				cliOnly:      cliOnly,
			})
		},
	}
	cmd.Flags().StringVar(&mode, "mode", "auto", "Stack mode to upgrade: auto|prod|dev|demo")
	cmd.Flags().StringVar(&imageVersion, "version", "", "Exact OpenNeko release to install (default: latest stable; accepts 1.2.3 or v1.2.3)")
	cmd.Flags().BoolVar(&noPrune, "no-prune", false, "Skip immediate post-upgrade image pruning (unused old agent images are still reaped after 24 hours)")
	cmd.Flags().BoolVar(&stackOnly, "stack-only", false, "Upgrade only stack images; keep the current local CLI")
	cmd.Flags().BoolVar(&cliOnly, "cli-only", false, "Upgrade only the local CLI; do not change the stack")
	return cmd
}

type upgradeOptions struct {
	mode         string
	imageVersion string
	noPrune      bool
	stackOnly    bool
	cliOnly      bool
}

func runUpgrade(ctx context.Context, cmd *cobra.Command, opts upgradeOptions) error {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()
	if opts.stackOnly && opts.cliOnly {
		return errors.New("--stack-only and --cli-only are mutually exclusive")
	}
	if instance.Current() != "" && !opts.cliOnly {
		if _, ok, err := requireConfiguredNamedInstallation(); err != nil {
			return err
		} else if !ok {
			return fmt.Errorf("instance %q has not been configured; run `openneko --instance %s setup` first", instance.Current(), instance.Current())
		}
	}
	target, err := resolveUpgradeTarget(ctx, opts.imageVersion, opts.stackOnly)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Resolved OpenNeko upgrade target %s.\n", target)
	if !opts.stackOnly {
		if err := upgradeCLIAndReexec(ctx, target, os.Args[1:], out, errOut); err != nil {
			return err
		}
		if opts.cliOnly {
			fmt.Fprintf(out, "CLI upgrade complete at %s.\n", target)
			return nil
		}
	}

	sup := compose.New(assets.ComposeFS)
	m, defaulted, err := resolveUpgradeMode(ctx, opts.mode, sup)
	if err != nil {
		return err
	}
	if defaulted {
		fmt.Fprintln(errOut, "warning: no saved stack mode or existing OpenNeko Docker stack found; assuming prod. Re-run with --mode dev or --mode demo if this install used another mode.")
	}
	project, err := sup.ProjectName(m)
	if err != nil {
		return err
	}
	if err := configureBackupEnvironment(project); err != nil {
		return fmt.Errorf("configure backup repository: %w", err)
	}
	if err := configureOpenShellNetwork(m); err != nil {
		return err
	}

	previous, hadPrevious := os.LookupEnv("OPENNEKO_VERSION")
	if err := os.Setenv("OPENNEKO_VERSION", target); err != nil {
		return err
	}
	defer func() {
		if hadPrevious {
			_ = os.Setenv("OPENNEKO_VERSION", previous)
		} else {
			_ = os.Unsetenv("OPENNEKO_VERSION")
		}
	}()
	if err := configurePinnedLibrarianImage(target); err != nil {
		return err
	}
	if err := configureLibrarianCPULimit(runtime.NumCPU()); err != nil {
		return err
	}

	files, err := sup.Materialize(m)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Upgrading OpenNeko %s stack to image tag %s\n", m, target)
	reapBeforePull(ctx)
	fmt.Fprintln(out, "Pulling stack images...")
	if code, err := sup.Run(ctx, project, files, []string{"pull"}, os.Stdout, os.Stderr); err != nil {
		return err
	} else if code != 0 {
		return WithExit(code, nil)
	}

	for _, image := range extraUpgradeImageRefs(target) {
		fmt.Fprintf(out, "Pulling %s...\n", image)
		if err := sup.PullImage(ctx, image, os.Stdout, os.Stderr); err != nil {
			return err
		}
	}

	fmt.Fprintln(out, "Restarting stack with upgraded images...")
	if err := restartUpgradedStack(ctx, sup, project, files); err != nil {
		return err
	}

	if err := sup.WriteImageVersion(target); err != nil {
		return err
	}

	if !opts.noPrune {
		fmt.Fprintln(out, "Cleaning up old OpenNeko images...")
		if err := pruneOldOpenNekoImages(ctx, target, os.Stdout, os.Stderr); err != nil {
			fmt.Fprintf(errOut, "warning: old image cleanup failed: %v\n", err)
		}
		if err := pruneDanglingImages(ctx, os.Stdout, os.Stderr); err != nil {
			fmt.Fprintf(errOut, "warning: dangling image cleanup failed: %v\n", err)
		}
	}

	if opts.stackOnly {
		fmt.Fprintf(
			out,
			"Stack upgrade complete at %s; local CLI remains %s.\n",
			target,
			opennekoversion.Version,
		)
	} else {
		fmt.Fprintf(out, "Upgrade complete. OpenNeko CLI and stack now use %s.\n", target)
	}
	return nil
}

func restartUpgradedStack(ctx context.Context, sup *compose.Supervisor, project string, files []string) error {
	if err := configureOpenShellStateDir(); err != nil {
		return err
	}
	configureOpenShellDBURL()
	// Enter a real maintenance boundary before replacing either database image.
	// Without this, old worker/GraphJin containers can reconnect in the window
	// between Postgres becoming healthy and the new storage-reconcile one-shot
	// acquiring its locks. Stopping the whole project is intentionally simple:
	// no database consumer can be omitted as the service graph evolves.
	if code, err := sup.Run(ctx, project, files, []string{"stop"}, os.Stdout, os.Stderr); err != nil {
		return err
	} else if code != 0 {
		return WithExit(code, fmt.Errorf("failed to enter upgrade maintenance mode"))
	}
	// Force the one-shot init containers onto the current network too. Compose
	// otherwise reuses already-exited containers during the one-time migration
	// from the legacy dynamic <project>_default network, leaving them unable to
	// resolve services that have moved to <project>_runtime.
	code, err := sup.Run(ctx, project, files, upgradeStackUpArgs(), os.Stdout, os.Stderr)
	if err != nil {
		return err
	}
	if code != 0 {
		return WithExit(code, errors.New("upgraded stack failed its readiness checks"))
	}
	return nil
}

func upgradeStackUpArgs() []string {
	// An upgrade is not successful merely because Compose created containers.
	// Wait for every declared healthcheck so crash loops and unreachable data
	// planes fail before the new image marker is persisted or old images prune.
	return []string{
		"up", "-d", "--wait", "--wait-timeout", "180",
		"--pull", "never", "--remove-orphans", "--force-recreate",
	}
}

func resolveUpgradeMode(ctx context.Context, flag string, sup *compose.Supervisor) (compose.Mode, bool, error) {
	mode := strings.TrimSpace(flag)
	if mode != "" && mode != "auto" {
		switch compose.Mode(mode) {
		case compose.ModeProd, compose.ModeDev, compose.ModeDemo:
			if settings, ok, err := loadCurrentInstallation(); err != nil {
				return "", false, err
			} else if ok && instance.Current() != "" && settings.Mode != mode {
				return "", false, fmt.Errorf("instance %q is installed in %s mode, not %s", instance.Current(), settings.Mode, mode)
			}
			return compose.Mode(mode), false, nil
		default:
			return "", false, fmt.Errorf("--mode must be one of: auto, prod, dev, demo (got %q)", flag)
		}
	}
	if settings, ok, err := loadCurrentInstallation(); err != nil {
		return "", false, err
	} else if ok {
		return compose.Mode(settings.Mode), false, nil
	}
	project, err := sup.ProjectName("")
	if err != nil {
		return "", false, err
	}
	if detected, ok := compose.ModeFromProjectName(project); ok {
		return detected, false, nil
	}
	if detected, ok, err := detectExistingOpenNekoMode(ctx); err != nil {
		return "", false, err
	} else if ok {
		return detected, false, nil
	}
	return compose.ModeProd, true, nil
}

var listDockerComposeProjectNames = dockerComposeProjectNames

func detectExistingOpenNekoMode(ctx context.Context) (compose.Mode, bool, error) {
	running, err := listDockerComposeProjectNames(ctx, false)
	if err != nil {
		return "", false, fmt.Errorf("inspect running Docker compose projects: %w", err)
	}
	if mode, ok, err := modeFromExistingProjects(running); ok || err != nil {
		return mode, ok, err
	}

	all, err := listDockerComposeProjectNames(ctx, true)
	if err != nil {
		return "", false, fmt.Errorf("inspect Docker compose projects: %w", err)
	}
	return modeFromExistingProjects(all)
}

func dockerComposeProjectNames(ctx context.Context, all bool) ([]string, error) {
	args := []string{"ps"}
	if all {
		args = append(args, "-a")
	}
	args = append(args,
		"--filter", "label=com.docker.compose.project",
		"--format", `{{.Label "com.docker.compose.project"}}`,
	)
	out, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return nil, err
	}
	return uniqueLines(string(out)), nil
}

func modeFromExistingProjects(projects []string) (compose.Mode, bool, error) {
	matches := map[string]compose.Mode{}
	var named []string
	selected := instance.Current()
	for _, project := range projects {
		mode, projectInstance, ok := compose.IdentityFromProjectName(project)
		if !ok && project == "openneko" {
			mode, ok = compose.ModeProd, true
		}
		if !ok {
			continue
		}
		if selected != "" {
			if projectInstance == selected {
				matches[project] = mode
			}
			continue
		}
		if projectInstance != "" {
			named = append(named, project)
			continue
		}
		matches[project] = mode
	}
	if len(matches) == 0 && len(named) > 0 && selected == "" {
		sort.Strings(named)
		return "", false, fmt.Errorf("found named OpenNeko stacks (%s); re-run with --instance <name>", strings.Join(named, ", "))
	}
	if len(matches) == 0 {
		return "", false, nil
	}
	if len(matches) == 1 {
		for _, mode := range matches {
			return mode, true, nil
		}
	}
	selectedProjects := make([]string, 0, len(matches))
	for project := range matches {
		selectedProjects = append(selectedProjects, project)
	}
	sort.Strings(selectedProjects)
	if selected == "" {
		return "", false, fmt.Errorf("found multiple OpenNeko stacks (%s); re-run with --mode prod, --mode dev, or --mode demo", strings.Join(selectedProjects, ", "))
	}
	return "", false, fmt.Errorf("instance %q has multiple OpenNeko stack projects (%s); remove the obsolete project before upgrading", selected, strings.Join(selectedProjects, ", "))
}

func uniqueLines(s string) []string {
	return uniqueStrings(strings.Split(s, "\n"))
}

var semverishImageVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+.*$`)

func normalizeUpgradeImageVersion(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "latest"
	}
	if semverishImageVersion.MatchString(v) {
		return "v" + v
	}
	return v
}

func extraUpgradeImageRefs(version string) []string {
	return uniqueStrings([]string{
		agentImageRef(os.Getenv("OPENNEKO_AGENT_IMAGE"), version),
		pluginBaseImageRef(os.Getenv("OPENNEKO_PLUGIN_BASE_IMAGE"), version),
		supervisorImageRef(os.Getenv("OPENNEKO_SUPERVISOR_IMAGE"), version),
	})
}

func pluginBaseImageRef(override, version string) string {
	if override != "" {
		return override
	}
	return "ghcr.io/open-neko/plugin-base:" + version
}

func supervisorImageRef(override, version string) string {
	if override != "" {
		return override
	}
	return "ghcr.io/open-neko/openshell-supervisor:" + version
}

func pruneOldOpenNekoImages(ctx context.Context, targetTag string, stdout, stderr *os.File) error {
	cmd := exec.CommandContext(ctx, "docker", "image", "ls", "--format", "{{.Repository}}:{{.Tag}}")
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	for _, ref := range oldOpenNekoImageRefs(string(out), targetTag) {
		rm := exec.CommandContext(ctx, "docker", "image", "rm", ref)
		rm.Stdout = stdout
		rm.Stderr = stderr
		if err := rm.Run(); err != nil {
			fmt.Fprintf(stderr, "warning: kept old image %s (%v)\n", ref, err)
		}
	}
	return nil
}

func pruneDanglingImages(ctx context.Context, stdout, stderr *os.File) error {
	cmd := exec.CommandContext(ctx, "docker", "image", "prune", "-f")
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func oldOpenNekoImageRefs(imageListOutput, targetTag string) []string {
	repos := openNekoImageRepos()
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(imageListOutput, "\n") {
		ref := strings.TrimSpace(line)
		if ref == "" || seen[ref] {
			continue
		}
		repo, tag, ok := strings.Cut(ref, ":")
		if !ok || tag == "" || tag == "<none>" {
			continue
		}
		if !repos[repo] || tag == targetTag {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
	}
	return out
}

func openNekoImageRepos() map[string]bool {
	return map[string]bool{
		"ghcr.io/open-neko/neko-cli":    true,
		"ghcr.io/open-neko/neko-db":     true,
		"ghcr.io/open-neko/records-db":  true,
		"ghcr.io/open-neko/neko-backup": true,
		// Legacy image retained so the first upgrade after its removal prunes it.
		"ghcr.io/open-neko/records-storage-ops":  true,
		"ghcr.io/open-neko/neko-graphjin":        true,
		"ghcr.io/open-neko/records-graphjin":     true,
		"ghcr.io/open-neko/neko-web":             true,
		"ghcr.io/open-neko/neko-worker":          true,
		"ghcr.io/open-neko/neko-embedding":       true,
		"ghcr.io/open-neko/neko-librarian":       true,
		"ghcr.io/open-neko/agent":                true,
		"ghcr.io/open-neko/plugin-base":          true,
		"ghcr.io/open-neko/openshell-supervisor": true,
	}
}

func uniqueStrings(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range in {
		if s = strings.TrimSpace(s); s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
