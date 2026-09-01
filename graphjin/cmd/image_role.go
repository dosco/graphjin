package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// What this binary is for.
//
// One source tree ships two images: a database server and an agent
// environment. ko, which builds them, sets the entrypoint to the binary and
// exposes no way to give it arguments — goreleaser's ko block has no
// entrypoint, cmd, or args field, and `ko build` only offers user, labels and
// annotations. So the image cannot say what it is; the binary has to.
//
// It is set the one way ko does expose, an ldflag, which turns out better than
// arguments would have been: the defaults become testable Go, they are visible
// in /health, and they stay overridable by flag and environment variable in the
// ordinary precedence order. A plain `go build` sets nothing and behaves
// exactly as it always has.
var imageRole string

// currentImageRole is the validated role, resolved once at startup. Tests and
// a plain `go build` leave it at the historical behaviour.
var currentImageRole = imageRoleServer

const (
	// imageRoleServer is the historical image: the database server.
	imageRoleServer = "server"
	// imageRoleEnv is the agent environment: `env serve`, listening off
	// loopback, ready with no files mounted.
	imageRoleEnv = "env"
)

// resolveImageRole refuses a role it does not know.
//
// Falling back to the server would mean a mislabelled build silently serving a
// database where somebody expected an environment — the failure would surface
// as a training loop that gets 404s from a healthy-looking container.
func resolveImageRole(role string) (string, error) {
	switch trimmed := strings.ToLower(strings.TrimSpace(role)); trimmed {
	case "":
		return imageRoleServer, nil
	case imageRoleServer, imageRoleEnv:
		return trimmed, nil
	default:
		return "", fmt.Errorf(
			"this binary was built with main.imageRole=%q, which is not a role it knows (%s or %s)",
			role, imageRoleServer, imageRoleEnv)
	}
}

// imageRoleArgs supplies the command line an image's role implies.
//
// Anything explicit wins: `docker run <image> version` runs version, so a
// role-built image stays a normal GraphJin binary for anyone who needs one.
func imageRoleArgs(role string, args []string) []string {
	if len(args) != 0 || role != imageRoleEnv {
		return args
	}
	return []string{"env", "serve"}
}

// imageRoleDefaults are the flag defaults a role changes, each still
// overridable by --flag and GJ_ENV_*.
//
// A container that listens on loopback is a container nothing can reach, and
// one that writes to its working directory is one that fails on a read-only
// root filesystem. Both defaults are right for a person at a terminal and wrong
// for an image, which is what a role is for.
func imageRoleDefaults(role string) map[string]string {
	if role != imageRoleEnv {
		return nil
	}
	defaults := map[string]string{
		"listen":   "0.0.0.0:8090",
		"work-dir": "/tmp/graphjin-env",
		"suite":    envSuiteEmbedded,
	}
	// One image tag should measure one thing forever. The demo seeds
	// date-relative data against the wall clock, so without a pinned instant
	// the same tag asks a different question every day it is run. The build
	// date is the one instant an image carries that never moves.
	if frozen, ok := buildDateInstant(date); ok {
		defaults["freeze-time"] = frozen
	}
	return defaults
}

// buildDateInstant reads the build date into an RFC3339 instant.
//
// Two toolchains stamp it: goreleaser writes RFC3339, and the Makefile writes
// `git log -1 --format=%ci`. Anything else — including the empty string a plain
// `go build` leaves — is not a date, and an environment that is not pinned says
// so rather than inventing an instant.
func buildDateInstant(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02 15:04:05 -0700", // git log --format=%ci
		"2006-01-02T15:04:05Z0700",
	} {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed.UTC().Format(time.RFC3339), true
		}
	}
	return "", false
}

// applyImageRole adjusts the process to what this binary was built to be.
func applyImageRole() error {
	role, err := resolveImageRole(imageRole)
	if err != nil {
		return err
	}
	currentImageRole = role
	os.Args = append(os.Args[:1], imageRoleArgs(role, os.Args[1:])...)
	return nil
}
