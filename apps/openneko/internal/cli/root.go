// Package cli wires the cobra command tree.
//
// Exit code reference:
//
//	0  ok
//	1  generic error
//	2  usage error (missing args, unknown flag)
//	3  host not supported for the sandbox runtime
package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/open-neko/neko/apps/openneko/assets"
	"github.com/open-neko/neko/apps/openneko/internal/compose"
	"github.com/open-neko/neko/apps/openneko/internal/dockerproxy"
	"github.com/open-neko/neko/apps/openneko/internal/instance"
	"github.com/open-neko/neko/apps/openneko/internal/version"
)

// MaybeProxyToWorker is used by worker-backed commands; if a worker container is
// running and --local wasn't passed, the command re-executes inside the
// worker via docker exec. Defined here so subcommand files can call it
// consistently. Returns (exitCode, true) when proxied, (0, false) when the
// caller should fall through to the local implementation. An optional handler
// lets host-file uploads stream to that same selected worker without copying.
func MaybeProxyToWorker(cmd *cobra.Command, run ...func(container string) int) (int, bool) {
	if local, _ := cmd.Flags().GetBool("local"); local {
		return 0, false
	}
	project := ""
	settings, ok, err := loadCurrentInstallation()
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "openneko: cannot read selected installation: %v\n", err)
		return 1, true
	}
	if ok {
		project = compose.ProjectNameForModeAndInstance(compose.Mode(settings.Mode), settings.Instance)
		if settings.Instance == "" {
			sup := compose.New(assets.ComposeFS)
			if saved, savedErr := sup.ProjectName(""); savedErr == nil && saved != "" {
				project = saved
			}
		}
	} else if instance.Current() != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "openneko: instance %q has not been configured; run `openneko --instance %s setup` first\n", instance.Current(), instance.Current())
		return 1, true
	}
	container, resolveErr := dockerproxy.ResolveRunningWorker(project)
	if resolveErr != nil {
		if project == "" && !errors.Is(resolveErr, dockerproxy.ErrAmbiguousWorkers) {
			// No packaged installation was selected. Preserve the source-dev
			// workflow, where worker-backed commands intentionally execute locally.
			return 0, false
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "openneko: cannot locate instance worker: %v\n", resolveErr)
		return 1, true
	}
	if container == "" {
		if project != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "openneko: worker for %s is not running; start the selected instance or pass --local\n", project)
			return 1, true
		}
		return 0, false
	}
	if project == "" {
		if name, named := namedInstanceFromWorkerContainer(container); named {
			fmt.Fprintf(cmd.ErrOrStderr(), "openneko: worker %s belongs to named instance %q; re-run with --instance %s\n", container, name, name)
			return 1, true
		}
	}
	if len(run) > 0 {
		return run[0](container), true
	}
	return dockerproxy.ProxyToWorker(container, os.Args[1:]), true
}

func namedInstanceFromWorkerContainer(container string) (string, bool) {
	const suffix = "-worker-1"
	project := strings.TrimSuffix(strings.TrimSpace(container), suffix)
	if project == container {
		return "", false
	}
	_, name, ok := compose.IdentityFromProjectName(project)
	return name, ok && name != ""
}

type exitErr struct {
	code int
	err  error
}

func (e *exitErr) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *exitErr) Unwrap() error { return e.err }

func WithExit(code int, err error) error {
	return &exitErr{code: code, err: err}
}

// ExitCodeFor returns the exit code carried by err, or 0 if no exitErr wraps it.
func ExitCodeFor(err error) int {
	var e *exitErr
	if errors.As(err, &e) {
		return e.code
	}
	return 0
}

func NewRoot() *cobra.Command {
	var selectedInstance string
	cmd := &cobra.Command{
		Use:   "openneko",
		Short: "OpenNeko operator CLI",
		Long: `openneko — supervises the OpenNeko stack.

Getting started: setup (guided install — preflight, bring-up, configure).
Solution packs: pack list, pack inspect, pack plan, pack install, pack status.
Skills and secrets: install (community skill from a git URL), secrets, doctor.
Stack ops:  instances, start, upgrade, stop, logs, status, backup, restore, storage, migrate, seed, reset.
GraphJin:   graphjin import.
Developer ops: eval.`,
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			return activateInstallation(selectedInstance)
		},
	}
	cmd.SetVersionTemplate("{{.Version}}\n")
	// Persistent flag: worker-backed commands (install, secrets, pack, ...)
	// auto-proxy into a running openneko-*-worker-1 container so the
	// brew-installed binary on the host acts on an operator-side docker
	// compose stack. --local forces host-side execution (use this for
	// source-build dev workflows that happen to have a compose stack
	// running alongside `pnpm dev`).
	cmd.PersistentFlags().Bool("local", false, "Force local execution; don't auto-proxy into a running worker container")
	cmd.PersistentFlags().StringVar(&selectedInstance, "instance", "", "Target a named OpenNeko installation")
	cmd.AddCommand(
		newSetupCmd(),
		newInstancesCmd(),
		newInstallCmd(),
		newDoctorCmd(),
		newSecretsCmd(),
		newPackCmd(),
		newVersionCmd(),
		newStartCmd(),
		newUpgradeCmd(),
		newStopCmd(),
		newStatusCmd(),
		newBackupCmd(),
		newRestoreCmd(),
		newStorageCmd(),
		newLogsCmd(),
		newMigrateCmd(),
		newSeedCmd(),
		newGraphjinCmd(),
		newResetCmd(),
	)
	return cmd
}
