package host

import "testing"

// The whole stack runs as containers, so the requirement is Docker, not KVM,
// and amd64 macs are fine.

func TestDarwinArm64WithDocker(t *testing.T) {
	r := checkWith("darwin", "arm64", func() bool { return true })
	if !r.Supported {
		t.Fatal("darwin arm64 with docker should be supported")
	}
	if r.Triple != "darwin-arm64" {
		t.Fatalf("bad triple: %s", r.Triple)
	}
}

func TestDarwinAmd64WithDocker(t *testing.T) {
	r := checkWith("darwin", "amd64", func() bool { return true })
	if !r.Supported {
		t.Fatal("darwin amd64 with docker should be supported (no microsandbox arm64-only constraint)")
	}
}

func TestNoDocker(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		r := checkWith(goos, "amd64", func() bool { return false })
		if r.Supported {
			t.Fatalf("%s without docker should not be supported", goos)
		}
		if r.Reason == "" {
			t.Fatal("missing reason")
		}
	}
}

func TestLinuxArm64WithDocker(t *testing.T) {
	r := checkWith("linux", "arm64", func() bool { return true })
	if !r.Supported || r.Triple != "linux-arm64" {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestLinuxUnknownArch(t *testing.T) {
	r := checkWith("linux", "riscv64", func() bool { return true })
	if r.Supported {
		t.Fatal("unknown arch should not be supported")
	}
}

func TestWindows(t *testing.T) {
	r := checkWith("windows", "amd64", func() bool { return false })
	if r.Supported {
		t.Fatal("windows should not be supported")
	}
}
