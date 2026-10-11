package assets

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type composeServiceForTest struct {
	Ports       []string                     `yaml:"ports"`
	Expose      []string                     `yaml:"expose"`
	Environment map[string]string            `yaml:"environment"`
	DependsOn   map[string]composeDepForTest `yaml:"depends_on"`
	Volumes     []string                     `yaml:"volumes"`
}

func TestPackagedComposeUsesReadOnlyBackupKeyFiles(t *testing.T) {
	core, raw := readComposeForTest(t, "core.yml")
	if strings.Contains(raw, "OPENNEKO_BACKUP_CIPHER_PASS") {
		t.Fatal("packaged Compose must not interpolate the backup key into container environment metadata")
	}
	for _, name := range []string{"neko-db", "records-db", "neko-backup", "neko-restore"} {
		service := core.Services[name]
		if got := service.Environment["PGBACKREST_REPO1_CIPHER_PASS_FILE"]; got != "/run/secrets/openneko-backup-key" {
			t.Fatalf("%s backup key file = %q", name, got)
		}
		wantMount := "${OPENNEKO_BACKUP_KEY_FILE:?OPENNEKO_BACKUP_KEY_FILE is required}:/run/secrets/openneko-backup-key:ro"
		found := false
		for _, mount := range service.Volumes {
			if mount == wantMount {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s is missing read-only backup key mount", name)
		}
	}
}

type composeDepForTest struct {
	Condition string `yaml:"condition"`
}

type composeFileForTest struct {
	Services map[string]composeServiceForTest `yaml:"services"`
}

func readComposeForTest(t *testing.T, name string) (composeFileForTest, string) {
	t.Helper()
	raw, err := fs.ReadFile(ComposeFS, "compose/"+name)
	if err != nil {
		t.Fatal(err)
	}
	var doc composeFileForTest
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return doc, string(raw)
}

// repoRootForTest walks up from the test's working directory until it finds the
// directory holding the top-level compose.yml.
func repoRootForTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "compose.yml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repo root: no compose.yml found walking up from the test directory")
		}
		dir = parent
	}
}

// TestDevComposeExposesOnlyWebToHost guards the top-level (development) Compose
// files that run on the Linux host. web is the only service allowed to publish
// on all interfaces (0.0.0.0); every other host-published port must stay bound
// to the 127.0.0.1 loopback, so only web is reachable from the LAN. Globbing
// compose*.yml means any new overlay is covered automatically.
func TestDevComposeExposesOnlyWebToHost(t *testing.T) {
	const webPublish = "${OPENNEKO_WEB_BIND_ADDRESS:-0.0.0.0}:${OPENNEKO_PORT:-3000}:8080"

	files, err := filepath.Glob(filepath.Join(repoRootForTest(t), "compose*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no top-level compose*.yml files found")
	}

	sawWeb := false
	for _, path := range files {
		name := filepath.Base(path)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var doc composeFileForTest
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for service, spec := range doc.Services {
			for _, entry := range spec.Ports {
				if service == "web" {
					if entry != webPublish {
						t.Fatalf("%s: web publishes %q, want only %q", name, entry, webPublish)
					}
					sawWeb = true
					continue
				}
				if !strings.HasPrefix(entry, "127.0.0.1:") {
					t.Fatalf("%s: service %q publishes host port %q on a non-loopback interface; only web may bind the LAN", name, service, entry)
				}
			}
		}
	}
	if !sawWeb {
		t.Fatal("expected the web service to publish its host port in the top-level compose files")
	}
}

func TestPackagedComposeExposesOnlyWebToLAN(t *testing.T) {
	core, _ := readComposeForTest(t, "core.yml")
	for name, service := range core.Services {
		if name == "web" {
			want := "${OPENNEKO_WEB_BIND_ADDRESS:-0.0.0.0}:${OPENNEKO_PORT:-3000}:8080"
			if len(service.Ports) != 1 || service.Ports[0] != want {
				t.Fatalf("web ports = %v, want only %q", service.Ports, want)
			}
			continue
		}
		if len(service.Ports) != 0 {
			t.Fatalf("packaged service %s unexpectedly publishes host ports: %v", name, service.Ports)
		}
	}

	demo, _ := readComposeForTest(t, "demo.yml")
	for name, service := range demo.Services {
		if len(service.Ports) != 0 {
			t.Fatalf("demo.yml service %s unexpectedly publishes host ports: %v", name, service.Ports)
		}
	}

	openshell, _ := readComposeForTest(t, "openshell.yml")
	for name, service := range openshell.Services {
		if name == "openshell-gateway" {
			want := "127.0.0.1:${OPENSHELL_PORT:-18080}:${OPENSHELL_PORT:-18080}"
			if len(service.Ports) != 1 || service.Ports[0] != want {
				t.Fatalf("gateway ports = %v, want only loopback %q", service.Ports, want)
			}
			continue
		}
		if len(service.Ports) != 0 {
			t.Fatalf("openshell.yml service %s unexpectedly publishes host ports: %v", name, service.Ports)
		}
	}
}

func TestPackagedComposeKeepsRuntimeTrafficOnDockerNetwork(t *testing.T) {
	core, _ := readComposeForTest(t, "core.yml")
	for service, port := range map[string]string{
		"neko-db":       "5432",
		"records-db":    "5432",
		"graphjin":      "8080",
		"neko-graphjin": "8089",
	} {
		got := core.Services[service].Expose
		if len(got) != 1 || got[0] != port {
			t.Fatalf("%s expose = %v, want internal port %s", service, got, port)
		}
	}

	migrate := core.Services["neko-migrate"]
	if migrate.Environment["NEKO_PG_HOST"] != "neko-db" {
		t.Fatalf("migration host = %q, want Docker service neko-db", migrate.Environment["NEKO_PG_HOST"])
	}
	if _, ok := migrate.Environment["OPENNEKO_OPENSHELL_DB_PASSWORD"]; !ok {
		t.Fatal("migration service must receive the gateway-role password")
	}
	if migrate.Environment["OPENNEKO_REQUIRE_EXPLICIT_OPENSHELL_DB_PASSWORD"] != "1" {
		t.Fatal("migration service must fail closed when the gateway-role password is absent")
	}
	if core.Services["web"].DependsOn["worker"].Condition != "service_healthy" {
		t.Fatal("web must start after the worker control plane is healthy")
	}

	openshell, raw := readComposeForTest(t, "openshell.yml")
	gateway := openshell.Services["openshell-gateway"]
	if gateway.DependsOn["neko-migrate"].Condition != "service_completed_successfully" {
		t.Fatal("OpenShell gateway must wait for private database preparation")
	}
	ready := openshell.Services["openshell-ready"]
	if ready.DependsOn["openshell-gateway"].Condition != "service_started" {
		t.Fatal("OpenShell readiness probe must start after the gateway container")
	}
	for _, service := range []string{"worker", "web"} {
		env := openshell.Services[service].Environment
		if env["OPENNEKO_SANDBOX_SHARED_NETWORK"] != "1" {
			t.Fatalf("%s shared-network broker config is incomplete: %v", service, env)
		}
		if _, exposed := env["OPENNEKO_BROKER_HOST_ALIAS"]; exposed {
			t.Fatalf("%s must derive a private container IP instead of a host broker alias", service)
		}
		if openshell.Services[service].DependsOn["openshell-ready"].Condition != "service_completed_successfully" {
			t.Fatalf("%s must wait for a successful OpenShell control-plane probe", service)
		}
	}
	for _, required := range []string{
		`version = 2`,
		`compute_driver = "docker"`,
		`guest_tls_ca = "%s/pki/ca.crt"`,
		`signing_key_path = "%s/pki/jwt/signing.pem"`,
		`sandbox_runtime_image = "ghcr.io/nvidia/openshell/sandbox:%s"`,
		`supervisor_image = "%s"`,
		`"${OPENNEKO_SUPERVISOR_IMAGE:-ghcr.io/open-neko/openshell-supervisor:${OPENNEKO_VERSION:-latest}}"`,
		`grpc_endpoint = "https://127.0.0.1:%s"`,
		`OPENNEKO_COMPOSE_NETWORK: "${COMPOSE_PROJECT_NAME}_runtime"`,
		`OPENSHELL_GATEWAY_IP: "${OPENSHELL_GATEWAY_IP:-172.29.0.2}"`,
		`ipv4_address: "${OPENSHELL_GATEWAY_IP:-172.29.0.2}"`,
		`name: "${COMPOSE_PROJECT_NAME}_runtime"`,
		`subnet: "${OPENNEKO_DOCKER_SUBNET:-172.29.0.0/24}"`,
		`gateway: "${OPENNEKO_DOCKER_GATEWAY:-172.29.0.1}"`,
		`ip_range: "${OPENNEKO_DOCKER_IP_RANGE:-172.29.0.128/25}"`,
	} {
		if !strings.Contains(raw, required) {
			t.Fatalf("openshell.yml missing %q", required)
		}
	}
}
