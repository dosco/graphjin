package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackUploadReadsHostArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.zip")
	if err := os.WriteFile(path, []byte("ZIP fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := runPackCommand(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/admin/packs/upload" || r.Header.Get("Content-Type") != "application/zip" || string(body) != "ZIP fixture" {
			t.Fatal("archive transport changed")
		}
		io.WriteString(w, `{"packId":"service-health","version":"0.1.0"}`)
	}), "upload", path)
	if err != nil || !strings.Contains(output, "Nothing was installed") {
		t.Fatalf("%s: %v", output, err)
	}
}

func TestPackUploadStreamsHostFileToSelectedWorker(t *testing.T) {
	directory := t.TempDir()
	archive := filepath.Join(directory, "host-only.zip")
	if err := os.WriteFile(archive, []byte("host ZIP bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
if [ "$1" = "ps" ]; then
  echo openneko-pack-upload-worker-1
else
  printf '%s\n' "$@" > "$PACK_TEST_CAPTURE/args"
  cat > "$PACK_TEST_CAPTURE/body"
  echo '{"packId":"service-health","version":"0.1.0"}'
fi
`
	if err := os.WriteFile(filepath.Join(directory, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PACK_TEST_CAPTURE", directory)
	t.Setenv("XDG_CONFIG_HOME", directory)
	t.Setenv("OPENNEKO_PROXIED", "")
	command := newPackCmd()
	command.SetArgs([]string{"upload", archive})
	var output bytes.Buffer
	command.SetOut(&output)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(filepath.Join(directory, "args"))
	body, _ := os.ReadFile(filepath.Join(directory, "body"))
	if !strings.Contains(string(args), "exec\n-i\nopenneko-pack-upload-worker-1\ncurl") || strings.Contains(string(args), archive) || string(body) != "host ZIP bytes" {
		t.Fatalf("host transport was not streamed: %s", args)
	}
}

func TestPackCustomReviewAndApproval(t *testing.T) {
	for _, approve := range []bool{false, true} {
		t.Run(fmt.Sprint(approve), func(t *testing.T) {
			applies := 0
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					io.WriteString(w, `{"manifest":{"metadata":{"version":"0.1.0"},"inputs":[{"key":"count","type":"integer"},{"key":"active","type":"boolean"},{"key":"choice","type":"enum","values":[1,2]}]}}`)
					return
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				inputs := body["inputs"].(map[string]any)
				if inputs["count"] != float64(7) || inputs["active"] != true || inputs["choice"] != float64(2) || body["version"] != "0.1.0" {
					t.Fatalf("bad declared configuration: %#v", body)
				}
				if body["secretRefs"].(map[string]any)["token"] != "stored_token" || body["dataSourceId"] != "source-1" {
					t.Fatal("bindings were not forwarded")
				}
				if strings.HasSuffix(r.URL.Path, "/review") {
					io.WriteString(w, `{"reviewHash":"exact-approval","plan":{"entries":[]}}`)
					return
				}
				applies++
				if body["reviewHash"] != "exact-approval" {
					t.Fatal("install did not use reviewed approval")
				}
				io.WriteString(w, `{"packId":"service-health","version":"0.1.0","status":"installed"}`)
			})
			args := []string{"install", "service-health", "--input", "count=7", "--input", "active=true", "--input", "choice=2", "--secret-ref", "token=stored_token", "--source-id", "source-1"}
			if approve {
				args = append(args, "--yes")
			}
			_, err := runPackCommand(t, handler, args...)
			if approve && (err != nil || applies != 1) {
				t.Fatalf("approved install failed: %v", err)
			}
			if !approve && (err == nil || applies != 0) {
				t.Fatal("unapproved pack was applied")
			}
		})
	}
}

func runPackCommand(t *testing.T, handler http.Handler, args ...string) (string, error) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	t.Setenv("WORKER_ADMIN_URL", server.URL)
	t.Setenv("OPENNEKO_PROXIED", "1")
	command := newPackCmd()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs(args)
	err := command.Execute()
	return output.String(), err
}

func TestPackListText(t *testing.T) {
	output, err := runPackCommand(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/packs" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"packs":[{"id":"magento","name":"Magento","version":"0.1.0","installed":false}]}`)
	}), "list")
	if err != nil {
		t.Fatal(err)
	}
	if output != "magento\tMagento\t0.1.0\tavailable\n" {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestPackInstallUsesSecretRefsAndConciseOutput(t *testing.T) {
	output, err := runPackCommand(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/packs/magento/install" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Inputs     map[string]any    `json:"inputs"`
			Secrets    map[string]string `json:"secrets"`
			SecretRefs map[string]string `json:"secretRefs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Secrets) != 0 {
			t.Fatalf("plaintext secrets unexpectedly submitted: %#v", body.Secrets)
		}
		if body.SecretRefs["database.analytics_username"] != "ANALYTICS_USER" || body.SecretRefs["database.analytics_password"] != "ANALYTICS_PASSWORD" {
			t.Fatalf("secret refs missing: %#v", body.SecretRefs)
		}
		if body.Inputs["database.host"] != "mariadb" {
			t.Fatalf("database host not submitted: %#v", body.Inputs)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"packId":"magento","version":"0.1.0","status":"installed","readiness":{"analytics":{"status":"ready","reason":null},"operator":{"status":"blocked","reason":"graphjin_version_unsupported"}},"installedAt":"2026-08-20T00:00:00Z","lastError":null}`)
	}), "install", "magento",
		"--non-interactive",
		"--base-url", "https://store.example.com",
		"--database-host", "mariadb",
		"--database-name", "magento",
		"--analytics-username-ref", "ANALYTICS_USER",
		"--analytics-password-ref", "ANALYTICS_PASSWORD",
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"installed magento@0.1.0", "analytics: ready", "write actions: optional — available after the required GraphJin update"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q in %q", want, output)
		}
	}
	for _, unwanted := range []string{"metric cards", "workflows", "watchers", "Will install"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("normal install output should not contain %q: %q", unwanted, output)
		}
	}
}

func TestPackInstallNonInteractiveRequiresReferences(t *testing.T) {
	output, err := runPackCommand(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("request should not be sent")
	}), "install", "magento",
		"--non-interactive",
		"--base-url", "https://store.example.com",
		"--database-host", "mariadb",
		"--database-name", "magento",
	)
	if err == nil || !strings.Contains(err.Error(), "analytics credentials are required") {
		t.Fatalf("expected credential-reference error, output=%q err=%v", output, err)
	}
}

func TestPackAPIErrorIsActionable(t *testing.T) {
	_, err := runPackCommand(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"read-only grant check failed"}`)
	}), "status", "magento")
	if err == nil || !strings.Contains(err.Error(), "read-only grant check failed (HTTP 400)") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPackDoctorReportsOptionalWriteActionsSeparately(t *testing.T) {
	output, err := runPackCommand(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/packs/magento/doctor" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"packId":"magento","status":"ready","checks":[{"id":"analytics","status":"ready","detail":"SELECT-only grants verified"},{"id":"operator","status":"optional","detail":"Write actions are not enabled; analytics and automations are healthy."}]}`)
	}), "doctor", "magento")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"magento: ready", "analytics: ready", "write actions: optional"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q in %q", want, output)
		}
	}
}

func TestPackManageShowsOnePageOverviewAndCommonTasks(t *testing.T) {
	output, err := runPackCommand(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/admin/packs/magento/status":
			_, _ = io.WriteString(w, `{"packId":"magento","version":"0.1.0","status":"installed","readiness":{"analytics":{"status":"ready","reason":null}},"lastError":null}`)
		case "/admin/packs/magento/doctor":
			_, _ = io.WriteString(w, `{"packId":"magento","status":"degraded","checks":[{"id":"analytics","status":"ready","detail":"SELECT-only grants verified"}]}`)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}), "manage", "magento")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"magento@0.1.0: installed",
		"Health checks:",
		"analytics: ready",
		"openneko pack configure magento",
		"openneko pack upgrade magento",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q in %q", want, output)
		}
	}
}

func TestPackConfigureUsesStoredReferences(t *testing.T) {
	_, err := runPackCommand(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/packs/magento/configure" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			SecretRefs map[string]string `json:"secretRefs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.SecretRefs["magento.integration_token"] != "MAGENTO_TOKEN" {
			t.Fatalf("integration token reference missing: %#v", body.SecretRefs)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"packId":"magento","version":"0.1.0","status":"installed","readiness":{"analytics":{"status":"ready","reason":null},"operator":{"status":"blocked","reason":"graphjin_version_unsupported"}}}`)
	}), "configure", "magento", "--integration-token-ref", "MAGENTO_TOKEN")
	if err != nil {
		t.Fatal(err)
	}
}

func TestPackUninstallRetainsHistoryAndData(t *testing.T) {
	output, err := runPackCommand(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/packs/magento/uninstall" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			IdempotencyKey string `json:"idempotencyKey"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.IdempotencyKey == "" {
			t.Fatal("uninstall request omitted idempotency key")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"packId":"magento","version":"0.1.0","status":"removed","readiness":{},"installedAt":"2026-08-20T00:00:00Z","lastError":null}`)
	}), "uninstall", "magento")
	if err != nil {
		t.Fatal(err)
	}
	if output != "uninstalled magento@0.1.0 (history and data retained)\n" {
		t.Fatalf("unexpected output: %q", output)
	}
}
