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
	Image       string                       `yaml:"image"`
	User        string                       `yaml:"user"`
	Command     []string                     `yaml:"command"`
	Ports       []string                     `yaml:"ports"`
	Expose      []string                     `yaml:"expose"`
	Environment map[string]string            `yaml:"environment"`
	DependsOn   map[string]composeDepForTest `yaml:"depends_on"`
	Volumes     []string                     `yaml:"volumes"`
	Networks    composeNetworksForTest       `yaml:"networks"`
	CapDrop     []string                     `yaml:"cap_drop"`
	CapAdd      []string                     `yaml:"cap_add"`
	SecurityOpt []string                     `yaml:"security_opt"`
}

// composeNetworksForTest reads a service's networks in either Compose form:
// a list of names, or a map of names to settings such as aliases.
type composeNetworksForTest []string

func (n *composeNetworksForTest) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.MappingNode {
		for i := 0; i < len(value.Content); i += 2 {
			*n = append(*n, value.Content[i].Value)
		}
		return nil
	}
	var names []string
	if err := value.Decode(&names); err != nil {
		return err
	}
	*n = names
	return nil
}

func TestPackagedComposeHasNoBackupService(t *testing.T) {
	core, raw := readComposeForTest(t, "core.yml")
	for _, retired := range []string{"OPENNEKO_BACKUP", "PGBACKREST", "pgbackrest", "archive_command"} {
		if strings.Contains(raw, retired) {
			t.Fatalf("packaged Compose still references retired backup setting %q", retired)
		}
	}
	for _, name := range []string{"records-db", "neko-backup", "neko-restore", "records-graphjin", "records-watch-graphjin"} {
		if _, exists := core.Services[name]; exists {
			t.Fatalf("packaged Compose still defines retired service %s", name)
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

}

func TestPackagedComposeKeepsRuntimeTrafficOnDockerNetwork(t *testing.T) {
	core, _ := readComposeForTest(t, "core.yml")
	for service, ports := range map[string]string{
		"neko-db": "5432",
		// GraphJin's API, and the subscriptions GraphJin.
		"openneko-serve": "8080,8089",
	} {
		if got := strings.Join(core.Services[service].Expose, ","); got != ports {
			t.Fatalf("%s expose = %v, want internal ports %s", service, got, ports)
		}
	}
	if _, exists := core.Services["neko-graphjin"]; exists {
		t.Fatal("packaged Compose still defines neko-graphjin; openneko-serve serves subscriptions")
	}
	for _, name := range []string{"web", "worker"} {
		if got := core.Services[name].Environment["OPENNEKO_GRAPHJIN_URL"]; got != "http://openneko-serve:8089" {
			t.Fatalf("%s OPENNEKO_GRAPHJIN_URL = %q, want the subscriptions GraphJin in openneko-serve", name, got)
		}
	}

	migrate := core.Services["neko-migrate"]
	if migrate.Environment["NEKO_PG_HOST"] != "neko-db" {
		t.Fatalf("migration host = %q, want Docker service neko-db", migrate.Environment["NEKO_PG_HOST"])
	}
	if core.Services["web"].DependsOn["worker"].Condition != "service_healthy" {
		t.Fatal("web must start after the worker control plane is healthy")
	}

}

// The agent runs no commands of its own: no service executes code for it,
// and openneko serve runs as a non-root user that shares sockets and run
// folders only with web and the worker.
func TestAgentRuntimeRunsNoCode(t *testing.T) {
	for _, file := range []string{"core.yml", "repo compose.yml"} {
		var doc composeFileForTest
		if file == "core.yml" {
			doc, _ = readComposeForTest(t, file)
		} else {
			raw, err := os.ReadFile(filepath.Join(repoRootForTest(t), "compose.yml"))
			if err != nil {
				t.Fatal(err)
			}
			if err := yaml.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
		}
		if _, ok := doc.Services["agent-sandbox"]; ok {
			t.Fatalf("%s still runs agent-sandbox", file)
		}
		for name, service := range doc.Services {
			for key := range service.Environment {
				if strings.HasPrefix(key, "OPENNEKO_SANDBOX_") {
					t.Fatalf("%s %s sets %s", file, name, key)
				}
			}
		}

		// openneko serve runs in the graphjin service.
		serve := doc.Services["openneko-serve"]
		if serve.User == "" || strings.HasPrefix(serve.User, "0") || strings.HasPrefix(serve.User, "root") {
			t.Fatalf("%s graphjin (openneko serve) user = %q, want a non-root user", file, serve.User)
		}
		if serve.Environment["OPENNEKO_SERVE_SOCKET"] != "/run/openneko/serve.sock" {
			t.Fatalf("%s graphjin (openneko serve) socket settings = %v", file, serve.Environment)
		}
		for _, name := range []string{"web", "worker"} {
			service := doc.Services[name]
			if service.Environment["OPENNEKO_SERVE_SOCKET"] != "/run/openneko/serve.sock" ||
				service.Environment["OPENNEKO_AGENT_RUNS_DIR"] != "/var/lib/openneko/agent-runs" ||
				service.Environment["OPENNEKO_AGENT_ENTRY"] != "/opt/openneko-agent/entry.js" {
				t.Fatalf("%s %s agent runtime settings = %v", file, name, service.Environment)
			}
			if _, ok := service.DependsOn["openneko-serve"]; !ok {
				t.Fatalf("%s %s does not wait for openneko serve (graphjin)", file, name)
			}
			if service.Environment["OPENNEKO_SERVE_GRAPHJIN"] != "1" {
				t.Fatalf("%s %s does not ask the hosted GraphJin in-process", file, name)
			}
			for _, want := range []string{"openneko-run:/run/openneko", "openneko-agent-runs:/var/lib/openneko/agent-runs"} {
				found := false
				for _, mount := range service.Volumes {
					found = found || mount == want
				}
				if !found {
					t.Fatalf("%s %s does not mount %s", file, name, want)
				}
			}
		}
	}
}
