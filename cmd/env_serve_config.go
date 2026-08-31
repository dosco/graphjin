package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

// Configuring a served environment from a distance.
//
// Flags are how a person at a terminal configures a server. A container is
// configured by whoever wrote its manifest, often on another machine and
// months earlier, so every `env serve` flag also answers to one GJ_ENV_<FLAG>
// variable. Flags win when they were actually passed, and a GJ_ENV_ variable
// nothing consumes is a startup error rather than a silence: a typo in an
// orchestrator's manifest is otherwise indistinguishable from a default, and
// the server comes up serving something nobody asked for.

const envServePrefix = "GJ_ENV_"

// envServeFlags are the flags a container may configure by environment. The
// list is explicit rather than every registered flag: --support-* already has
// its own GJ_SUPPORT_ namespace, and a flag reachable two ways is a flag with
// two answers.
var envServeFlags = []string{
	"path", "work-dir", "suite", "split", "side", "pool", "listen",
	"freeze-time", "data-anchor", "reward-profile", "allow-catalog-drift",
	"step", "step-timeout", "external", "external-timeout",
}

// envServeVariable names the variable that configures a flag.
func envServeVariable(flag string) string {
	return envServePrefix + strings.ToUpper(strings.ReplaceAll(flag, "-", "_"))
}

// applyEnvServeSettings overlays GJ_ENV_* onto the flags nobody passed.
//
// Setting flags rather than returning a struct means each value is parsed by
// the flag that owns it, so a duration stays a duration and a bad one fails
// closed naming both the variable and the value. Returns one line per applied
// variable for the startup banner.
func applyEnvServeSettings(flags *pflag.FlagSet, environ []string) ([]string, error) {
	present := map[string]string{}
	for _, entry := range environ {
		key, value, found := strings.Cut(entry, "=")
		if found && strings.HasPrefix(key, envServePrefix) {
			present[key] = value
		}
	}
	if len(present) == 0 {
		return nil, nil
	}
	var applied []string
	for _, name := range envServeFlags {
		variable := envServeVariable(name)
		value, ok := present[variable]
		if !ok {
			continue
		}
		delete(present, variable)
		if flags.Changed(name) {
			// The flag wins, but saying so out loud beats leaving an operator to
			// wonder why the variable they set had no effect.
			applied = append(applied, fmt.Sprintf("%s ignored (--%s was passed)", variable, name))
			continue
		}
		if err := flags.Set(name, value); err != nil {
			return nil, fmt.Errorf("%s=%q is not a valid --%s: %w", variable, value, name, err)
		}
		applied = append(applied, fmt.Sprintf("%s=%s", variable, value))
	}
	if len(present) > 0 {
		unknown := make([]string, 0, len(present))
		for key := range present {
			unknown = append(unknown, key)
		}
		sort.Strings(unknown)
		known := make([]string, 0, len(envServeFlags))
		for _, name := range envServeFlags {
			known = append(known, envServeVariable(name))
		}
		return nil, fmt.Errorf(
			"%s configures nothing; this server reads %s. The model is configured separately through "+
				"GJ_AGENT_* and GJ_SUPPORT_*",
			strings.Join(unknown, ", "), strings.Join(known, ", "))
	}
	return applied, nil
}

// enterEnvWorkDir makes the process run somewhere it is allowed to write.
//
// The built-in demo extracts to ./graphjin-demo and each worker copies a
// project into TMPDIR, so a server started in a read-only directory fails
// partway through provisioning with whatever error the failing write produced.
// A container gets one place to put all of it, checked before anything else
// starts.
func enterEnvWorkDir(dir string) error {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" {
		return nil
	}
	absolute, err := filepath.Abs(trimmed)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(absolute, 0o750); err != nil {
		return fmt.Errorf("--work-dir %s cannot be created: %w; mount a writable volume there", absolute, err)
	}
	probe := filepath.Join(absolute, ".graphjin-work-dir-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return fmt.Errorf(
			"--work-dir %s is not writable: %w; a container with a read-only root filesystem needs a volume "+
				"or --tmpfs mounted there", absolute, err)
	}
	_ = os.Remove(probe)
	return os.Chdir(absolute)
}

// resolveEnvAgentTimeout decides how long one episode's agent run may take.
//
// The stack default is 50 seconds, chosen for a person waiting on a request.
// An episode is not that, and every sibling path raises it to 300 through
// applyDemoAgentEnvDefaults — which `env serve` does not reach, so until now a
// served episode was cancelled at 50 seconds no matter what was configured.
// Step mode makes it starker: the trainer holds each completion for as long as
// it likes, and --step-timeout was an idle allowance the agent cancelled
// underneath.
//
// A deadline someone pinned is theirs to keep; returning 0 leaves it alone.
func resolveEnvAgentTimeout(pinned string, step bool, stepTimeout time.Duration) (int, error) {
	needed := 0
	if step {
		// One completion may take the whole idle allowance, and the episode
		// needs room for the rest of its calls afterwards.
		needed = int((stepTimeout + time.Minute).Seconds())
	}
	trimmed := strings.TrimSpace(pinned)
	if trimmed == "" {
		if needed > demoAgentTimeoutSeconds {
			return needed, nil
		}
		return demoAgentTimeoutSeconds, nil
	}
	seconds, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, fmt.Errorf("GJ_AGENT_TIMEOUT_SECONDS=%q is not a number of seconds", trimmed)
	}
	if effective := gjagent.EffectiveTimeoutSeconds(seconds); effective < needed {
		return 0, fmt.Errorf(
			"GJ_AGENT_TIMEOUT_SECONDS=%d is shorter than --step-timeout %s; the agent would cancel the "+
				"episode while the trainer was still inside its allowance. Raise it to at least %d, or lower "+
				"--step-timeout", effective, stepTimeout, needed)
	}
	return 0, nil
}
