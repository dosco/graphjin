// Package roles runs the programs that the openneko binary carries besides
// the operator CLI. Every Go process in an install runs this one binary and
// picks its role from the name it runs under (a link named graphjin,
// ax-harness, lazy-service or serve) or from its first argument.
package roles

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dosco/graphjin/cmd/v3/cli"
	"github.com/open-neko/lazy-service/lazy"
	"github.com/open-neko/openneko/apps/ax-harness/harness"

	"github.com/open-neko/neko/apps/openneko/internal/version"
)

// Names of the roles, as link names and as first arguments.
const (
	GraphJin    = "graphjin"
	AxHarness   = "ax-harness"
	LazyService = "lazy-service"
	// Serve runs agent turns for the worker over a local socket; see serve.
	Serve = "serve"
)

// Run starts the role that args name and reports whether it did. args is
// os.Args. When it returns false, args is for the operator CLI. A role that
// ends the process exits it; one that returns has finished.
func Run(args []string) bool {
	name, rest, ok := Pick(args)
	if !ok {
		return false
	}
	switch name {
	case GraphJin:
		runGraphJin(rest)
	case AxHarness:
		os.Args = append([]string{AxHarness}, rest...)
		harness.Main()
	case LazyService:
		lazy.Main(rest)
	case Serve:
		runServe(rest)
	}
	return true
}

// Pick returns the role that args (os.Args) name and the arguments for it.
// ok is false when args are for the operator CLI.
func Pick(args []string) (name string, rest []string, ok bool) {
	if len(args) == 0 {
		return "", nil, false
	}
	name = strings.TrimSuffix(filepath.Base(args[0]), ".exe")
	rest = args[1:]
	if !isRole(name) {
		if len(rest) == 0 {
			return "", nil, false
		}
		name, rest = rest[0], rest[1:]
		// The operator CLI keeps `openneko graphjin import`.
		if name == GraphJin && len(rest) > 0 && rest[0] == "import" {
			return "", nil, false
		}
	}
	if !isRole(name) {
		return "", nil, false
	}
	return name, rest, true
}

func isRole(name string) bool {
	return name == GraphJin || name == AxHarness || name == LazyService || name == Serve
}

// runGraphJin runs the GraphJin CLI, which reads os.Args itself.
func runGraphJin(args []string) {
	os.Args = append([]string{GraphJin}, args...)
	cli.SetBuildInfo(version.GraphJinVersion, version.GraphJinCommit, "")
	cli.Cmd()
}
