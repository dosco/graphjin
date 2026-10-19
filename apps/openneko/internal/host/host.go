package host

import (
	"os/exec"
	"runtime"
)

type Result struct {
	Supported bool
	Triple    string
	Reason    string
}

// Check reports whether the OpenShell sandbox runtime is expected to run on
// this host (SEC9: OpenShell is the only runtime). The gateway and every
// sandbox run as containers, so the requirement is Docker — not KVM, which
// was a microsandbox-era constraint.
func Check() Result {
	return checkWith(runtime.GOOS, runtime.GOARCH, hasDocker)
}

// Platform reports OS/arch support independent of whether docker is present.
// preflight checks docker separately so it can give a sharper "daemon not
// running" remediation, so it wants the OS/arch verdict on its own.
func Platform() Result {
	return checkWith(runtime.GOOS, runtime.GOARCH, func() bool { return true })
}

func checkWith(goos, goarch string, docker func() bool) Result {
	triple := goos + "-" + goarch
	switch goos {
	case "darwin", "linux":
		if goarch != "arm64" && goarch != "amd64" {
			return Result{
				Supported: false,
				Triple:    triple,
				Reason:    goos + " " + goarch + " is not supported — the OpenShell sandbox images ship for amd64 and arm64.",
			}
		}
		if !docker() {
			return Result{
				Supported: false,
				Triple:    triple,
				Reason:    "docker not found on PATH — the OpenShell gateway and agent sandboxes run as containers.",
			}
		}
		return Result{Supported: true, Triple: triple}
	}
	return Result{
		Supported: false,
		Triple:    triple,
		Reason:    "Windows is not currently supported. WSL2 viability is being evaluated.",
	}
}

func hasDocker() bool {
	_, err := exec.LookPath("docker")
	return err == nil
}
