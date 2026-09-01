package main

import (
	"strings"
	"testing"
)

// A mislabelled build must not quietly become the other image.
func TestImageRoleRefusesWhatItDoesNotKnow(t *testing.T) {
	for input, want := range map[string]string{
		"":         imageRoleServer,
		"server":   imageRoleServer,
		"env":      imageRoleEnv,
		" ENV ":    imageRoleEnv,
		"Server\n": imageRoleServer,
	} {
		got, err := resolveImageRole(input)
		if err != nil || got != want {
			t.Fatalf("role %q resolved to %q (%v), want %q", input, got, err, want)
		}
	}
	// Falling back to the server would serve a database where somebody expected
	// an environment, and surface as 404s from a healthy-looking container.
	for _, bad := range []string{"environment", "eval", "env-serve", "1"} {
		if _, err := resolveImageRole(bad); err == nil {
			t.Fatalf("role %q must be refused", bad)
		} else if !strings.Contains(err.Error(), bad) {
			t.Fatalf("the refusal must name the role it was given: %v", err)
		}
	}
}

// An environment image runs `env serve` with no arguments, and stays an
// ordinary GraphJin binary for anyone who passes some.
func TestImageRoleSuppliesItsOwnCommand(t *testing.T) {
	if got := imageRoleArgs(imageRoleEnv, nil); strings.Join(got, " ") != "env serve" {
		t.Fatalf("a bare environment image ran %v", got)
	}
	if got := imageRoleArgs(imageRoleEnv, []string{"version"}); strings.Join(got, " ") != "version" {
		t.Fatalf("an explicit command must win: %v", got)
	}
	if got := imageRoleArgs(imageRoleEnv, []string{"env", "health"}); strings.Join(got, " ") != "env health" {
		t.Fatalf("the healthcheck must still be reachable: %v", got)
	}
	if got := imageRoleArgs(imageRoleServer, nil); len(got) != 0 {
		t.Fatalf("the server image must be unchanged: %v", got)
	}
}

// The defaults that are right at a terminal and wrong in a container.
func TestEnvImageDefaultsAreContainerDefaults(t *testing.T) {
	if defaults := imageRoleDefaults(imageRoleServer); len(defaults) != 0 {
		t.Fatalf("the server image must keep every default it had: %v", defaults)
	}
	defaults := imageRoleDefaults(imageRoleEnv)
	if defaults["listen"] != "0.0.0.0:8090" {
		t.Fatalf("a container listening on loopback is a container nothing can reach: %q", defaults["listen"])
	}
	if defaults["suite"] != envSuiteEmbedded {
		t.Fatalf("suite = %q; an image has no eval/ directory to read", defaults["suite"])
	}
	if !strings.HasPrefix(defaults["work-dir"], "/tmp/") {
		t.Fatalf("work-dir = %q; a read-only root filesystem needs somewhere writable", defaults["work-dir"])
	}
	// And every one of them is a default, not a pin: applied at registration,
	// so a flag and GJ_ENV_* both still win in the ordinary order.
	previous := currentImageRole
	currentImageRole = imageRoleEnv
	defer func() { currentImageRole = previous }()

	cmd := envServeCmd()
	for flag, want := range defaults {
		if got := cmd.Flags().Lookup(flag).DefValue; got != want {
			t.Fatalf("--%s defaults to %q in an environment image, want %q", flag, got, want)
		}
		if cmd.Flags().Changed(flag) {
			t.Fatalf("--%s is marked as passed, so GJ_ENV_* could never override it", flag)
		}
	}
	if _, err := applyEnvServeSettings(cmd.Flags(), []string{"GJ_ENV_LISTEN=0.0.0.0:9999"}); err != nil {
		t.Fatal(err)
	}
	if got := cmd.Flags().Lookup("listen").Value.String(); got != "0.0.0.0:9999" {
		t.Fatalf("the environment must still outrank an image default, got %q", got)
	}
	// And the server image keeps the defaults it always had.
	currentImageRole = imageRoleServer
	if got := envServeCmd().Flags().Lookup("listen").DefValue; got != "127.0.0.1:8090" {
		t.Fatalf("the server build's --listen default moved to %q", got)
	}
}

// One image tag has to measure one thing forever. The demo seeds date-relative
// data against the wall clock, so an unpinned tag asks a different question
// every day; the build date is the one instant an image carries that never
// moves. Two toolchains stamp it in two formats.
func TestBuildDateBecomesAnInstantOrNothing(t *testing.T) {
	for input, want := range map[string]string{
		"2026-08-31T12:34:56Z":      "2026-08-31T12:34:56Z", // goreleaser
		"2026-08-31 08:34:56 -0400": "2026-08-31T12:34:56Z", // git log --format=%ci
		"2026-08-31T12:34:56+00:00": "2026-08-31T12:34:56Z",
		"2026-08-31 12:34:56 +0000": "2026-08-31T12:34:56Z",
	} {
		got, ok := buildDateInstant(input)
		if !ok || got != want {
			t.Fatalf("%q became %q (%v), want %q", input, got, ok, want)
		}
	}
	// A build with no date is not pinned, and says so rather than inventing an
	// instant — freeze_time_source is how a caller tells the two apart.
	for _, bad := range []string{"", "   ", "not-set", "2026-08-31", "yesterday"} {
		if got, ok := buildDateInstant(bad); ok {
			t.Fatalf("%q must not be read as a build date, got %q", bad, got)
		}
	}
}
