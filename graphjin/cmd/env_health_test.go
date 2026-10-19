package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The Python client reads /health by name and by position. Everything added in
// v6 is additive; nothing that was there before may move.
func TestHealthKeepsItsExistingContract(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	server, stop := startEnvTestServer(t, `await final({status:"blocked",answer:"x"});`)
	defer stop()

	rec := httptest.NewRecorder()
	server.handleHealth(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	// Decoded into a struct of only the fields that existed before v6, with
	// their old names and old types.
	var legacy struct {
		Status  string `json:"status"`
		Workers int    `json:"workers"`
		Tasks   int    `json:"tasks"`
		Dataset struct {
			CatalogHash string `json:"catalog_hash"`
			DataAnchor  string `json:"data_anchor"`
		} `json:"dataset"`
		RewardVersion string `json:"reward_version"`
		RewardProfile string `json:"reward_profile"`
		Suite         struct {
			Version string `json:"version"`
			Seed    int64  `json:"seed"`
			Scale   int    `json:"scale"`
		} `json:"suite"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Status != "ready" || legacy.Workers != 1 || legacy.Tasks != 2 ||
		legacy.RewardVersion == "" || legacy.Dataset.CatalogHash == "" || legacy.Suite.Version == "" {
		t.Fatalf("a pre-v6 client must still read this document: %+v", legacy)
	}
}

// Everything a lab needs to know before running an episode: what binary, what
// suite, whether it matches the world, and how it may be driven.
func TestHealthSaysWhatThisEnvironmentIs(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	server, stop := startEnvTestServer(t, `await final({status:"blocked",answer:"x"});`)
	defer stop()
	server.suiteSource = "public"
	server.splitLabel = "auto:0.80"
	server.driveModes = envDriveModes(true, false)

	rec := httptest.NewRecorder()
	server.handleHealth(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	var health envHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}

	// A plain `go build` has no ldflags, so version and commit are honestly
	// absent — but what it does know it must report.
	if health.Build.Go == "" {
		t.Fatal("build must at least name the Go it was built with")
	}
	if health.Build.BinarySHA256 == "" {
		t.Fatal("two labs comparing numbers need to know they ran the same binary")
	}
	caps := health.Capabilities
	if strings.Join(caps.DriveModes, ",") != "episodes,step" {
		t.Fatalf("drive_modes = %v", caps.DriveModes)
	}
	if caps.SuiteSource != "public" || caps.Split != "auto:0.80" || caps.Side != "train" {
		t.Fatalf("capabilities do not describe the configuration: %+v", caps)
	}
	if caps.SuiteFingerprint == "" || caps.Pool != 1 {
		t.Fatalf("capabilities = %+v", caps)
	}
	if caps.DataAnchor == "" {
		t.Fatal("a caller cannot tell which day the world is dated for")
	}
	// Nothing to compare must not read as drift.
	if caps.CatalogMatch != nil {
		t.Fatalf("catalog_match = %v with no recorded catalog fingerprint", *caps.CatalogMatch)
	}
	if health.Capabilities.BootMS <= 0 {
		t.Fatal("boot_ms should report what the world cost to bring up")
	}
}

func TestDriveModesListOnlyWhatIsServed(t *testing.T) {
	for _, tc := range []struct {
		step, external bool
		want           string
	}{
		{false, false, "episodes"},
		{true, false, "episodes,step"},
		{false, true, "episodes,external"},
		{true, true, "episodes,step,external"},
	} {
		if got := strings.Join(envDriveModes(tc.step, tc.external), ","); got != tc.want {
			t.Fatalf("step=%v external=%v gave %q, want %q", tc.step, tc.external, got, tc.want)
		}
	}
}

// The image has no shell and no curl, so the probe has to be the binary. Exit
// zero must mean ready and nothing else.
func TestEnvHealthProbeAnswersOnlyForAReadyServer(t *testing.T) {
	ready := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ready","workers":2,"tasks":113}`))
	}))
	defer ready.Close()
	if _, err := fetchEnvHealth(t.Context(), ready.URL, 5*time.Second); err != nil {
		t.Fatalf("a ready server must pass: %v", err)
	}

	// A server that is up and not serving is exactly what a healthcheck exists
	// to catch, so a 200 is not on its own an answer.
	notReady := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"provisioning"}`))
	}))
	defer notReady.Close()
	if _, err := fetchEnvHealth(t.Context(), notReady.URL, 5*time.Second); err == nil ||
		!strings.Contains(err.Error(), "provisioning") {
		t.Fatalf("a server that is not ready must fail, naming what it said: %v", err)
	}

	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer missing.Close()
	if _, err := fetchEnvHealth(t.Context(), missing.URL, 5*time.Second); err == nil {
		t.Fatal("a 404 must fail")
	}
	notJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html>hello</html>`))
	}))
	defer notJSON.Close()
	if _, err := fetchEnvHealth(t.Context(), notJSON.URL, 5*time.Second); err == nil {
		t.Fatal("something that is not an environment must fail")
	}
	// Nothing listening at all: the container's first seconds.
	if _, err := fetchEnvHealth(t.Context(), "http://127.0.0.1:1", time.Second); err == nil {
		t.Fatal("a dial failure must fail")
	}
}

// A HEALTHCHECK that repeats the port goes stale the moment somebody changes
// GJ_ENV_LISTEN.
func TestEnvHealthFindsThePortItWasToldToServe(t *testing.T) {
	for listen, want := range map[string]string{
		"":                 "http://127.0.0.1:8090",
		"0.0.0.0:9000":     "http://127.0.0.1:9000",
		":7777":            "http://127.0.0.1:7777",
		"[::]:8091":        "http://127.0.0.1:8091",
		"not-an-address":   "http://127.0.0.1:8090",
		"127.0.0.1: 8090 ": "http://127.0.0.1:8090",
	} {
		got := defaultEnvHealthURL(func(key string) string {
			if key == "GJ_ENV_LISTEN" {
				return listen
			}
			return ""
		})
		if got != want {
			t.Fatalf("listen %q gave %q, want %q", listen, got, want)
		}
	}
}
