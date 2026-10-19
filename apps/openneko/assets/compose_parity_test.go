package assets

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type composeParityDocument struct {
	Services map[string]composeParityService `yaml:"services"`
	Volumes  map[string]any                  `yaml:"volumes"`
}

type composeParityService struct {
	Image       string                             `yaml:"image"`
	User        string                             `yaml:"user"`
	Entrypoint  []string                           `yaml:"entrypoint"`
	Restart     string                             `yaml:"restart"`
	ReadOnly    bool                               `yaml:"read_only"`
	CPUs        string                             `yaml:"cpus"`
	MemLimit    string                             `yaml:"mem_limit"`
	PidsLimit   int                                `yaml:"pids_limit"`
	NetworkMode string                             `yaml:"network_mode"`
	CapDrop     []string                           `yaml:"cap_drop"`
	Tmpfs       []string                           `yaml:"tmpfs"`
	Profiles    []string                           `yaml:"profiles"`
	DependsOn   map[string]composeParityDependency `yaml:"depends_on"`
	Environment map[string]any                     `yaml:"environment"`
	Volumes     []string                           `yaml:"volumes"`
	Ports       []string                           `yaml:"ports"`
	Command     []string                           `yaml:"command"`
	Healthcheck map[string]any                     `yaml:"healthcheck"`
}

func TestLibrarianIsVendoredBoundedAndRequired(t *testing.T) {
	root := repoRootForTest(t)
	sourceRaw, err := os.ReadFile(filepath.Join(root, "compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	packagedRaw, err := ComposeFS.ReadFile("compose/core.yml")
	if err != nil {
		t.Fatal(err)
	}

	for label, document := range map[string]composeParityDocument{
		"source":   loadComposeParityDocument(t, sourceRaw),
		"packaged": loadComposeParityDocument(t, packagedRaw),
	} {
		librarian, ok := document.Services["librarian"]
		if !ok {
			t.Fatalf("%s compose is missing librarian", label)
		}
		if !librarian.ReadOnly || librarian.CPUs != "${OPENNEKO_LIBRARIAN_CPUS:-1.0}" || librarian.MemLimit != "4g" || librarian.PidsLimit != 256 {
			t.Fatalf("%s librarian limits are incomplete: %+v", label, librarian)
		}
		if !reflect.DeepEqual(librarian.CapDrop, []string{"ALL"}) {
			t.Fatalf("%s librarian capability drop = %v", label, librarian.CapDrop)
		}
		if len(librarian.Tmpfs) != 1 || !strings.Contains(librarian.Tmpfs[0], "/tmp:size=512m") {
			t.Fatalf("%s librarian tmpfs = %v", label, librarian.Tmpfs)
		}
		for _, variable := range []string{"HF_HUB_OFFLINE", "TRANSFORMERS_OFFLINE", "DOCLING_ARTIFACTS_PATH"} {
			if _, ok := librarian.Environment[variable]; !ok {
				t.Fatalf("%s librarian is missing %s", label, variable)
			}
		}
		if dependency := document.Services["worker"].DependsOn["librarian"]; dependency.Condition != "service_healthy" {
			t.Fatalf("%s worker librarian dependency = %+v", label, dependency)
		}
		if !strings.Contains(fmt.Sprint(librarian.Healthcheck["test"]), "/health/ready") {
			t.Fatalf("%s librarian healthcheck does not use readiness", label)
		}
		if librarian.Environment["OPENNEKO_SERVICE_IDLE_TIMEOUT"] != "${OPENNEKO_LIBRARIAN_IDLE_TIMEOUT:-5m}" {
			t.Fatalf("%s librarian idle timeout is missing", label)
		}
		if !reflect.DeepEqual(librarian.Volumes, []string{"librarian-results:/var/lib/neko-librarian/results"}) {
			t.Fatalf("%s librarian results must use a disk volume: %v", label, librarian.Volumes)
		}
		if librarian.Environment["NEKO_LIBRARIAN_RESULT_TTL_SECONDS"] != "${OPENNEKO_LIBRARIAN_RESULT_TTL_SECONDS:-900}" || librarian.Environment["NEKO_LIBRARIAN_RESULT_MAX_BYTES"] != "${OPENNEKO_LIBRARIAN_RESULT_MAX_BYTES:-536870912}" {
			t.Fatalf("%s librarian result retention bounds are missing", label)
		}
		embedding := document.Services["embedding"]
		if !embedding.ReadOnly || embedding.CPUs != "${OPENNEKO_EMBEDDING_CPUS:-1.0}" || embedding.MemLimit != "${OPENNEKO_EMBEDDING_MEMORY:-1g}" || len(embedding.Ports) != 0 {
			t.Fatalf("%s embedding must be bounded and private: %+v", label, embedding)
		}
		if embedding.Environment["OPENNEKO_SERVICE_IDLE_TIMEOUT"] != "${OPENNEKO_EMBEDDING_IDLE_TIMEOUT:-5m}" || !strings.Contains(fmt.Sprint(embedding.Healthcheck["test"]), "/health/ready") {
			t.Fatalf("%s embedding lazy lifecycle configuration is missing", label)
		}
		if document.Services["worker"].DependsOn["embedding"].Condition != "service_healthy" {
			t.Fatalf("%s worker must wait for the embedding listener", label)
		}
		for _, consumer := range []string{"web", "worker"} {
			if document.Services[consumer].Environment["NEKO_EMBEDDING_URL"] != "http://embedding:5003" {
				t.Fatalf("%s %s embedding endpoint is missing", label, consumer)
			}
		}
	}

	packaged := loadComposeParityDocument(t, packagedRaw).Services["librarian"]
	if !strings.Contains(packaged.Image, "ghcr.io/open-neko/neko-librarian:") {
		t.Fatalf("packaged librarian image = %q", packaged.Image)
	}
}

func TestPackagedGraphJinReusesReleasedRuntime(t *testing.T) {
	coreRaw, err := ComposeFS.ReadFile("compose/core.yml")
	if err != nil {
		t.Fatal(err)
	}
	core := loadComposeParityDocument(t, coreRaw)
	graphjin, ok := core.Services["graphjin"]
	if !ok {
		t.Fatal("packaged core is missing graphjin")
	}
	if !strings.Contains(graphjin.Image, "ghcr.io/open-neko/records-graphjin:") {
		t.Fatalf("packaged graphjin image = %q, want the shared records-graphjin runtime", graphjin.Image)
	}
	// Config changes apply live through gj_config, so GraphJin runs directly.
	if want := []string{"/sbin/tini", "--", "graphjin"}; !reflect.DeepEqual(graphjin.Entrypoint, want) {
		t.Fatalf("packaged graphjin entrypoint = %v, want %v", graphjin.Entrypoint, want)
	}
	wantHealthcheck := []string{
		"CMD", "curl", "-sS", "-o", "/dev/null",
		"http://127.0.0.1:8080/api/v1/graphql",
	}
	if got := composeHealthcheckTest(t, "packaged", "graphjin", graphjin); !reflect.DeepEqual(got, wantHealthcheck) {
		t.Fatalf("packaged graphjin readiness check = %v, want %v", got, wantHealthcheck)
	}
	configInit, ok := core.Services["graphjin-config-init"]
	if !ok {
		t.Fatal("packaged core is missing graphjin-config-init")
	}
	assertGraphJinConfigInit(t, "packaged core", configInit)
	assertGraphJinConfigVolumeMatches(t, "packaged core", configInit, graphjin)

	demoRaw, err := ComposeFS.ReadFile("compose/demo.yml")
	if err != nil {
		t.Fatal(err)
	}
	demo := loadComposeParityDocument(t, demoRaw)
	demoConfigInit := demo.Services["graphjin-config-init"]
	assertGraphJinConfigInit(t, "packaged demo overlay", demoConfigInit)
	assertGraphJinConfigVolumeMatches(t, "packaged demo overlay", demoConfigInit, demo.Services["graphjin"])
	if strings.Contains(string(demoRaw), "dosco/graphjin") {
		t.Fatal("packaged demo restores the duplicate upstream GraphJin image")
	}
}

func TestGraphJinRunsWithoutTheRestartSupervisor(t *testing.T) {
	root := repoRootForTest(t)
	for _, name := range []string{"Dockerfile", "compose.yml", "compose.dev.yml", "compose.adventureworks.yml", "compose.graphjin-agent.yml"} {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "graphjin-supervisor") {
			t.Fatalf("%s still references the retired GraphJin restart supervisor", name)
		}
	}

	baseRaw, err := os.ReadFile(root + "/compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	base := loadComposeParityDocument(t, baseRaw)
	if strings.Contains(base.Services["graphjin"].Image, "dosco/graphjin") {
		t.Fatal("source graphjin uses the upstream image instead of the openneko build")
	}
	assertGraphJinConfigInit(t, "source core", base.Services["graphjin-config-init"])
	assertGraphJinConfigVolumeMatches(t, "source core", base.Services["graphjin-config-init"], base.Services["graphjin"])

	for _, overlayName := range []string{"compose.adventureworks.yml", "compose.graphjin-agent.yml"} {
		raw, err := os.ReadFile(root + "/" + overlayName)
		if err != nil {
			t.Fatal(err)
		}
		overlay := loadComposeParityDocument(t, raw)
		initializer := overlay.Services["graphjin-config-init"]
		if len(initializer.Command) != 0 {
			t.Fatalf("%s appends a command that the inherited initializer entrypoint would ignore", overlayName)
		}
		assertGraphJinConfigInit(t, overlayName, initializer)
	}
}

func assertGraphJinConfigInit(t *testing.T, label string, service composeParityService) {
	t.Helper()
	script := strings.Join(service.Entrypoint, "\n")
	for _, required := range []string{
		"chown -R 1001:999 /config",
		"find /config -type d -exec chmod 0700 {} +",
		"find /config -type f -exec chmod 0600 {} +",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("%s does not secure the GraphJin config volume: missing %q", label, required)
		}
	}
	if strings.Contains(script, "chmod -R a+rwX /config") {
		t.Fatalf("%s makes GraphJin secrets writable by every container user", label)
	}
}

func assertGraphJinConfigVolumeMatches(t *testing.T, label string, initializer, graphjin composeParityService) {
	t.Helper()
	initSource := composeMountSourceAt(initializer.Volumes, "/config")
	graphjinSource := composeMountSourceAt(graphjin.Volumes, "/config")
	if initSource == "" || graphjinSource == "" || initSource != graphjinSource {
		t.Fatalf("%s repairs GraphJin volume %q but serves volume %q", label, initSource, graphjinSource)
	}
}

func composeMountSourceAt(mounts []string, target string) string {
	for _, mount := range mounts {
		parts := strings.Split(mount, ":")
		if len(parts) >= 2 && parts[1] == target {
			return parts[0]
		}
	}
	return ""
}

func TestPackagedInitializerKeepsAnExistingConfig(t *testing.T) {
	for _, composeFile := range []string{"core.yml", "demo.yml"} {
		t.Run(composeFile, func(t *testing.T) {
			raw, err := ComposeFS.ReadFile("compose/" + composeFile)
			if err != nil {
				t.Fatal(err)
			}
			service := loadComposeParityDocument(t, raw).Services["graphjin-config-init"]
			script := strings.Join(service.Entrypoint[2:], "\n")

			configDir := t.TempDir()
			persistedConfig := filepath.Join(configDir, "agentic.yml")
			if err := os.WriteFile(persistedConfig, []byte("persisted: true\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			script = strings.ReplaceAll(script, "/config", configDir)
			// Docker Compose turns $$ into a literal $ before invoking the shell.
			script = strings.ReplaceAll(script, "$$", "$")
			cmd := exec.Command("/bin/sh", "-c", script)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("run %s initializer against existing volume: %v\n%s", composeFile, err, output)
			}
			persisted, err := os.ReadFile(persistedConfig)
			if err != nil {
				t.Fatal(err)
			}
			if string(persisted) != "persisted: true\n" {
				t.Fatalf("initializer replaced existing datasource config: %q", persisted)
			}
		})
	}
}

func TestPostReleaseSmokeGatesRetainedDemoUpgrades(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRootForTest(t), ".github", "workflows", "post-release-smoke.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var parsedWorkflow any
	if err := yaml.Unmarshal(raw, &parsedWorkflow); err != nil {
		t.Fatalf("post-release smoke is not valid YAML: %v", err)
	}
	workflow := string(raw)
	for _, required := range []string{
		"openneko start --mode demo --detach --pull never",
		"librarian_pdf_async=ok",
		"openneko upgrade --stack-only --mode demo",
		"{{.State.Health.Status}}",
		"authenticated_datasource_query=ok",
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("post-release smoke no longer gates retained demo upgrades: missing %q", required)
		}
	}
}

func TestProductionDeployRequiresAuthenticatedDatasourceQuery(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRootForTest(t), ".github", "workflows", "deploy.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var parsedWorkflow any
	if err := yaml.Unmarshal(raw, &parsedWorkflow); err != nil {
		t.Fatalf("production deploy is not valid YAML: %v", err)
	}
	workflow := string(raw)
	for _, required := range []string{
		"openneko upgrade --mode demo",
		"OPENNEKO_PORT=80 openneko status",
		"callGraphjinMcpTool",
		"authenticated_datasource_query=ok",
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("production deploy no longer verifies the datasource upgrade: missing %q", required)
		}
	}
	if strings.Contains(workflow, "OPENNEKO_PORT=80 openneko status || true") {
		t.Fatal("production deploy ignores an unhealthy upgraded stack")
	}
}

func TestVendoredAgentBinaryCannotBeOverridden(t *testing.T) {
	rootRaw, err := os.ReadFile("../../../compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	packagedRaw, err := ComposeFS.ReadFile("compose/core.yml")
	if err != nil {
		t.Fatal(err)
	}
	for label, document := range map[string]composeParityDocument{
		"source":   loadComposeParityDocument(t, rootRaw),
		"packaged": loadComposeParityDocument(t, packagedRaw),
	} {
		for _, serviceName := range []string{"web", "worker"} {
			if _, exists := document.Services[serviceName].Environment["OPENNEKO_AGENT_MODEL_BINARY"]; exists {
				t.Fatalf("%s %s exposes an override for the vendored agent executable", label, serviceName)
			}
		}
	}
}

func TestStorageReconciliationGatesEveryDatabaseConsumer(t *testing.T) {
	rootRaw, err := os.ReadFile("../../../compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	packagedRaw, err := ComposeFS.ReadFile("compose/core.yml")
	if err != nil {
		t.Fatal(err)
	}
	for label, document := range map[string]composeParityDocument{
		"source":   loadComposeParityDocument(t, rootRaw),
		"packaged": loadComposeParityDocument(t, packagedRaw),
	} {
		reconcile, ok := document.Services["storage-reconcile"]
		if !ok {
			t.Fatalf("%s compose is missing storage-reconcile", label)
		}
		if want := []string{"storage", "reconcile"}; !reflect.DeepEqual(reconcile.Command, want) {
			t.Fatalf("%s storage-reconcile command = %v, want %v", label, reconcile.Command, want)
		}
		for _, database := range []string{"neko-db", "records-db"} {
			if reconcile.DependsOn[database].Condition != "service_healthy" {
				t.Fatalf("%s storage-reconcile must wait for healthy %s", label, database)
			}
		}
		for _, key := range []string{
			"NEKO_PG_HOST", "NEKO_PG_DATABASE", "RECORDS_PG_HOST", "RECORDS_PG_DATABASE",
		} {
			if _, ok := reconcile.Environment[key]; !ok {
				t.Fatalf("%s storage-reconcile is missing %s", label, key)
			}
		}
		for _, consumer := range []string{
			"neko-migrate", "neko-backup", "records-graphjin", "records-watch-graphjin",
		} {
			if document.Services[consumer].DependsOn["storage-reconcile"].Condition != "service_completed_successfully" {
				t.Fatalf("%s %s can start before storage reconciliation", label, consumer)
			}
		}
	}
}

func TestDatabaseImagesEnforcePinnedGlibcStorageABI(t *testing.T) {
	root := repoRootForTest(t)
	dockerfile, err := os.ReadFile(root + "/Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	raw := string(dockerfile)
	for _, required := range []string{
		"FROM pgvector/pgvector:0.8.6-pg16-bookworm@sha256:ccc6e83d6e35e931dc7c5def2022729d5a6c370318d099181995567ff1fb4d6b AS postgres-runtime",
		`LABEL org.openneko.storage-contract="1"`,
		`test "$(getconf GNU_LIBC_VERSION)" = "glibc 2.36"`,
		"FROM postgres-runtime AS neko-db",
		"FROM postgres-runtime AS records-db",
	} {
		if !strings.Contains(raw, required) {
			t.Fatalf("Dockerfile does not enforce storage ABI line %q", required)
		}
	}
	if strings.Contains(raw, "FROM postgres:16-alpine AS postgres-runtime") {
		t.Fatal("stateful database runtime regressed to Alpine/musl")
	}
}

type composeParityDependency struct {
	Condition string `yaml:"condition"`
}

type composeParityInventory struct {
	Restart        string
	Profiles       []string
	Dependencies   []string
	Environment    []string
	Mounts         []string
	Command        []string
	HasHealthcheck bool
}

func loadComposeParityDocument(t *testing.T, raw []byte) composeParityDocument {
	t.Helper()
	var document composeParityDocument
	if err := yaml.Unmarshal(raw, &document); err != nil {
		t.Fatalf("parse compose YAML: %v", err)
	}
	return document
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func normalizeComposeMount(mount string) string {
	separator := strings.Index(mount, ":")
	if strings.HasPrefix(mount, "${") {
		if end := strings.Index(mount, "}"); end >= 0 {
			if offset := strings.Index(mount[end+1:], ":"); offset >= 0 {
				separator = end + 1 + offset
			}
		}
	}
	if separator < 0 {
		return mount
	}
	source := mount[:separator]
	if strings.HasPrefix(source, "${") && strings.HasSuffix(source, "}") {
		variable := strings.TrimSuffix(strings.TrimPrefix(source, "${"), "}")
		name, modifier, _ := strings.Cut(variable, ":")
		if name == "OPENNEKO_CONFIG_VOLUME" && strings.HasPrefix(modifier, "-") {
			source = strings.TrimPrefix(modifier, "-")
		} else {
			source = "$" + name
		}
	}
	return source + mount[separator:]
}

func composeParityInventoryFor(service composeParityService) composeParityInventory {
	dependencies := make([]string, 0, len(service.DependsOn))
	for name, dependency := range service.DependsOn {
		dependencies = append(dependencies, fmt.Sprintf("%s:%s", name, dependency.Condition))
	}
	sort.Strings(dependencies)
	mounts := make([]string, len(service.Volumes))
	for index, mount := range service.Volumes {
		mounts[index] = normalizeComposeMount(mount)
	}
	sort.Strings(mounts)
	profiles := append([]string(nil), service.Profiles...)
	sort.Strings(profiles)
	return composeParityInventory{
		Restart:        service.Restart,
		Profiles:       profiles,
		Dependencies:   dependencies,
		Environment:    sortedKeys(service.Environment),
		Mounts:         mounts,
		Command:        service.Command,
		HasHealthcheck: len(service.Healthcheck) > 0,
	}
}

func withoutMount(mounts []string, ignored string) []string {
	filtered := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		if mount != ignored {
			filtered = append(filtered, mount)
		}
	}
	return filtered
}

func composeHealthcheckTest(t *testing.T, label, serviceName string, service composeParityService) []string {
	t.Helper()
	raw, ok := service.Healthcheck["test"].([]any)
	if !ok {
		t.Fatalf("%s %s healthcheck test is not a command: %#v", label, serviceName, service.Healthcheck["test"])
	}
	command := make([]string, len(raw))
	for index, value := range raw {
		argument, ok := value.(string)
		if !ok {
			t.Fatalf("%s %s healthcheck argument %d is not a string: %#v", label, serviceName, index, value)
		}
		command[index] = argument
	}
	return command
}

func TestSourceAndPackagedComposeStateInventoryMatch(t *testing.T) {
	rootRaw, err := os.ReadFile("../../../compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	packagedRaw, err := ComposeFS.ReadFile("compose/core.yml")
	if err != nil {
		t.Fatal(err)
	}
	root := loadComposeParityDocument(t, rootRaw)
	packaged := loadComposeParityDocument(t, packagedRaw)

	if got, want := sortedKeys(root.Volumes), sortedKeys(packaged.Volumes); !reflect.DeepEqual(got, want) {
		t.Fatalf("named-volume inventory drifted:\nsource:   %v\npackaged: %v", got, want)
	}
	if got, want := sortedKeys(root.Services), sortedKeys(packaged.Services); !reflect.DeepEqual(got, want) {
		t.Fatalf("service inventory drifted:\nsource:   %v\npackaged: %v", got, want)
	}
	for _, name := range sortedKeys(root.Services) {
		got := composeParityInventoryFor(root.Services[name])
		want := composeParityInventoryFor(packaged.Services[name])
		if name == "graphjin-config-init" {
			// Source compose reads immutable seeds from the checkout. Packaged
			// compose reads the same paths baked into neko-worker, so it correctly
			// has no host bind mounts for these inputs.
			got.Mounts = withoutMount(
				got.Mounts,
				"./db/graphjin/customer.sources.example.yml:/seed/customer.sources.yml:ro",
			)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("service %s state/recovery inventory drifted:\nsource:   %#v\npackaged: %#v", name, got, want)
		}
	}
}

func TestDevelopmentDatabasesRecycleWALWithoutArchiving(t *testing.T) {
	root := repoRootForTest(t)
	sourceCore, err := os.ReadFile(filepath.Join(root, "compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	sourceDev, err := os.ReadFile(filepath.Join(root, "compose.dev.yml"))
	if err != nil {
		t.Fatal(err)
	}
	packagedCore, err := ComposeFS.ReadFile("compose/core.yml")
	if err != nil {
		t.Fatal(err)
	}
	packagedDev, err := ComposeFS.ReadFile("compose/dev.yml")
	if err != nil {
		t.Fatal(err)
	}
	for label, pair := range map[string]struct{ core, dev []byte }{
		"source":   {sourceCore, sourceDev},
		"packaged": {packagedCore, packagedDev},
	} {
		core := loadComposeParityDocument(t, pair.core)
		dev := loadComposeParityDocument(t, pair.dev)
		for _, name := range []string{"neko-db", "records-db"} {
			production := strings.Join(core.Services[name].Command, " ")
			development := strings.Join(dev.Services[name].Command, " ")
			if !strings.Contains(production, "archive_mode=on") || !strings.Contains(production, "archive_timeout=60s") {
				t.Errorf("%s %s lost its production archive policy: %s", label, name, production)
			}
			if !strings.Contains(development, "archive_mode=off") || strings.Contains(development, "archive_timeout") || strings.Contains(development, "archive_command") {
				t.Errorf("%s %s development archive policy is unsafe: %s", label, name, development)
			}
		}
	}
}

func TestRecordsGraphJinEndpointsRemainPrivate(t *testing.T) {
	rootRaw, err := os.ReadFile("../../../compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	packagedRaw, err := ComposeFS.ReadFile("compose/core.yml")
	if err != nil {
		t.Fatal(err)
	}
	for label, document := range map[string]composeParityDocument{
		"source":   loadComposeParityDocument(t, rootRaw),
		"packaged": loadComposeParityDocument(t, packagedRaw),
	} {
		wantHealthcheck := []string{
			"CMD", "curl", "-sS", "-o", "/dev/null",
			"http://127.0.0.1:8090/api/v1/graphql",
		}
		for _, serviceName := range []string{"records-graphjin", "records-watch-graphjin"} {
			service, ok := document.Services[serviceName]
			if !ok {
				t.Fatalf("%s compose is missing %s", label, serviceName)
			}
			if len(service.Ports) != 0 {
				t.Errorf("%s %s must not publish host ports: %v", label, serviceName, service.Ports)
			}
			if service.User != "1001:999" {
				t.Errorf("%s %s user = %q, want 1001:999", label, serviceName, service.User)
			}
			if got := composeHealthcheckTest(t, label, serviceName, service); !reflect.DeepEqual(got, wantHealthcheck) {
				t.Errorf("%s %s healthcheck must accept an authenticated-error HTTP response as ready:\ngot:  %v\nwant: %v", label, serviceName, got, wantHealthcheck)
			}
		}
	}
}

func TestCustomerGraphJinDoesNotOwnWorkerConfigAsRoot(t *testing.T) {
	rootRaw, err := os.ReadFile("../../../compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	packagedRaw, err := ComposeFS.ReadFile("compose/core.yml")
	if err != nil {
		t.Fatal(err)
	}
	for label, document := range map[string]composeParityDocument{
		"source":   loadComposeParityDocument(t, rootRaw),
		"packaged": loadComposeParityDocument(t, packagedRaw),
	} {
		if got := document.Services["graphjin"].User; got != "1001:999" {
			t.Errorf("%s graphjin user = %q, want 1001:999", label, got)
		}
	}
}

func TestPackagedDemoBootstrapsJWTBeforeServingWeb(t *testing.T) {
	raw, err := ComposeFS.ReadFile("compose/demo.yml")
	if err != nil {
		t.Fatal(err)
	}
	demo := loadComposeParityDocument(t, raw)

	seed := demo.Services["neko-adventureworks-seed"]
	if got := seed.Environment["ADVENTUREWORKS_AUTH_MODE"]; got != "jwt" {
		t.Fatalf("demo seed auth mode = %#v, want jwt", got)
	}
	if got := seed.Environment["ADVENTUREWORKS_RECONCILE_AUTH_MODE"]; got != "1" {
		t.Fatalf("demo auth reconciliation = %#v, want 1", got)
	}

	worker := demo.Services["worker"]
	if got := worker.DependsOn["neko-adventureworks-seed"].Condition; got != "service_completed_successfully" {
		t.Fatalf("worker demo-seed dependency = %q, want service_completed_successfully", got)
	}
	web := demo.Services["web"]
	if got := web.DependsOn["worker"].Condition; got != "service_healthy" {
		t.Fatalf("web worker dependency = %q, want service_healthy", got)
	}
	for _, serviceName := range []string{"web", "worker"} {
		if got := demo.Services[serviceName].Environment["OPENNEKO_STACK_MODE"]; got != "demo" {
			t.Fatalf("%s stack mode = %#v, want demo", serviceName, got)
		}
	}

	coreRaw, err := ComposeFS.ReadFile("compose/core.yml")
	if err != nil {
		t.Fatal(err)
	}
	prod := loadComposeParityDocument(t, coreRaw)
	for _, serviceName := range []string{"web", "worker"} {
		if got, exists := prod.Services[serviceName].Environment["OPENNEKO_STACK_MODE"]; exists {
			t.Fatalf("production %s unexpectedly sets OPENNEKO_STACK_MODE=%#v", serviceName, got)
		}
	}
}
