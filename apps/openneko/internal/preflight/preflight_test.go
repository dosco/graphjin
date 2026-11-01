package preflight

import (
	"net"
	"strconv"
	"testing"
)

func TestDefaultPortsAreOnlyWeb(t *testing.T) {
	if len(DefaultPorts) != 1 ||
		DefaultPorts[0].Label != "web" || DefaultPorts[0].Def != 3000 ||
		DefaultPorts[0].BindEnvVar != "OPENNEKO_WEB_BIND_ADDRESS" {
		t.Fatalf("packaged host ports = %+v, want only web", DefaultPorts)
	}
}

func TestPortSpecBindAddressHonorsEnvironment(t *testing.T) {
	t.Setenv("OPENNEKO_TEST_BIND", "127.0.0.2")
	spec := PortSpec{BindEnvVar: "OPENNEKO_TEST_BIND", DefaultBind: "0.0.0.0"}
	if got := spec.BindAddress(); got != "127.0.0.2" {
		t.Fatalf("bind address = %q", got)
	}
}

func TestPortsFreeWhenUnbound(t *testing.T) {
	// Grab a free port from the OS, then release it so the spec sees it free.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	specs := []PortSpec{{Label: "test", EnvVar: "NOPE_UNSET", Def: port}}
	res := Ports(specs)
	if len(res) != 1 {
		t.Fatalf("want 1 result, got %d", len(res))
	}
	if res[0].Level != Pass {
		t.Fatalf("want Pass for free port %d, got %+v", port, res[0])
	}
}

func TestPortsFailWhenBound(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	specs := []PortSpec{{Label: "test", EnvVar: "OPENNEKO_TEST_PORT", Def: port}}
	res := Ports(specs)
	if res[0].Level != Fail {
		t.Fatalf("want Fail for bound port %d, got %+v", port, res[0])
	}
	if res[0].Remediation == "" {
		t.Fatal("expected a remediation naming the override env var")
	}
}

func TestPortSpecEnvOverride(t *testing.T) {
	t.Setenv("OPENNEKO_TEST_PORT_OVERRIDE", "55999")
	s := PortSpec{Label: "test", EnvVar: "OPENNEKO_TEST_PORT_OVERRIDE", Def: 3000}
	if got := s.Port(); got != 55999 {
		t.Fatalf("want override 55999, got %d", got)
	}
	// Junk override falls back to the default.
	t.Setenv("OPENNEKO_TEST_PORT_OVERRIDE", "not-a-number")
	if got := s.Port(); got != 3000 {
		t.Fatalf("want fallback 3000, got %d", got)
	}
}

func TestDuplicateBinary(t *testing.T) {
	// PATH openneko is this same binary → no false-positive nag.
	if r := duplicateBinaryWith("/usr/local/bin/openneko", "/usr/local/bin/openneko", nil); r.Level != Pass {
		t.Fatalf("same path should be Pass, got %+v", r)
	}
	// No openneko on PATH → nothing to warn about.
	if r := duplicateBinaryWith("/usr/local/bin/openneko", "", errNotFound); r.Level != Pass {
		t.Fatalf("lookup error should be Pass, got %+v", r)
	}
	// A different openneko shadows us → warn with remediation.
	r := duplicateBinaryWith("/opt/me/openneko", "/usr/bin/openneko", nil)
	if r.Level != Warn || r.Remediation == "" {
		t.Fatalf("different path should Warn with remediation, got %+v", r)
	}
}

var errNotFound = &lookErr{}

type lookErr struct{}

func (*lookErr) Error() string { return "not found" }

func TestResultOK(t *testing.T) {
	if !(Result{Level: Pass}).OK() || !(Result{Level: Warn}).OK() {
		t.Fatal("Pass and Warn should be OK")
	}
	if (Result{Level: Fail}).OK() {
		t.Fatal("Fail should not be OK")
	}
}

// Guard the loopback-join helper shape Ports relies on.
func TestJoinHostPortShape(t *testing.T) {
	if got := net.JoinHostPort("127.0.0.1", strconv.Itoa(8089)); got != "127.0.0.1:8089" {
		t.Fatalf("unexpected join: %s", got)
	}
}
