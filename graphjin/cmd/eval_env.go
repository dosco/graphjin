package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/dosco/graphjin/serv/v3"
)

type evalEnvironment struct {
	ClientFactory gjagent.ClientFactory
	HTTPClient    *http.Client
	StatusOut     *os.File
	// MCPRecorder collects the tool calls made against this instance, so an
	// agent the harness does not host can still be graded on what it did rather
	// than only on what it said.
	MCPRecorder func(serv.MCPToolEvent)
}

func (e evalEnvironment) Start(ctx context.Context, spec gjeval.EnvSpec) (gjeval.Instance, error) {
	switch spec.Target {
	case gjeval.TargetRemote:
		return e.startRemote(ctx)
	case gjeval.TargetDemo, gjeval.TargetLocal:
		return e.startEmbedded(ctx, spec)
	default:
		return nil, fmt.Errorf("unsupported eval target %q", spec.Target)
	}
}

func (e evalEnvironment) startRemote(ctx context.Context) (gjeval.Instance, error) {
	clientConfig, err := LoadClientConfig()
	if err != nil {
		return nil, err
	}
	if clientConfig == nil || strings.TrimSpace(clientConfig.Server) == "" {
		return nil, fmt.Errorf("remote target is not configured; run `graphjin cli setup`")
	}
	token := strings.TrimSpace(os.Getenv("GRAPHJIN_EVAL_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(clientConfig.Token)
	}
	headers := map[string]string{}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	client := e.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	source := gjeval.HTTPCatalogSource{Client: client, BaseURL: clientConfig.Server, Headers: headers}
	snapshot, err := source.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	return &gjeval.StaticInstance{
		URL:            clientConfig.Server,
		RequestHeaders: headers,
		Dataset:        gjeval.DatasetFingerprint{CatalogHash: snapshot.Fingerprint},
		TargetLabel:    "remote",
	}, nil
}

func (e evalEnvironment) startEmbedded(ctx context.Context, spec gjeval.EnvSpec) (gjeval.Instance, error) {
	configPath, err := filepath.Abs(spec.ConfigPath)
	if err != nil {
		return nil, err
	}
	frozenNow, frozenOK, err := spec.FrozenTime()
	if err != nil {
		return nil, err
	}
	dataAnchor, err := spec.EffectiveDataAnchor()
	if err != nil {
		return nil, err
	}
	configName := serv.GetConfigName()
	if spec.Target == gjeval.TargetDemo {
		// Embedded demo evaluation intentionally uses the dev config's trusted
		// local identity and then forces the agent read-only. This avoids minting
		// or persisting credentials while preserving the same data/catalog.
		if _, statErr := os.Stat(filepath.Join(configPath, "dev.yml")); statErr == nil {
			configName = "dev"
		}
	}
	loaded, err := serv.ReadInConfig(filepath.Join(configPath, configName))
	if err != nil {
		return nil, err
	}
	cloned := *loaded
	configureEvalInstance(&cloned, spec)
	apiServer := startSaaSOpsEvalAPIMock(&cloned)

	var runtime *DemoRuntime
	restoreDemoGlobals := func() {}
	if spec.Target == gjeval.TargetDemo {
		demoStatePath := filepath.Join(configPath, "demo")
		localStatePath := filepath.Join(configPath, ".graphjin")
		if _, demoErr := os.Stat(demoStatePath); os.IsNotExist(demoErr) {
			if _, localErr := os.Stat(localStatePath); localErr == nil {
				closeEvalAPIServer(apiServer)
				return nil, fmt.Errorf("refusing to provision a fresh demo over existing %s; start or reset the demo explicitly first", localStatePath)
			}
		}
		// StartDemo is existing CLI provisioning code and uses these command
		// globals. Serving does not touch them, so several instances can serve
		// at once, but only one may be provisioned at a time.
		demoBootMu.Lock()
		defer demoBootMu.Unlock()
		previousPath, previousConf, previousDB, previousOpened := cpath, conf, db, dbOpened
		previousAnchor := demoPinnedDataAnchor
		restoreDemoGlobals = func() {
			cpath, conf, db, dbOpened = previousPath, previousConf, previousDB, previousOpened
			demoPinnedDataAnchor = previousAnchor
		}
		cpath = configPath
		conf = &cloned
		demoPinnedDataAnchor = dataAnchor
		runtime, err = StartDemo(ctx, []string{"sqlite"}, e.StatusOut)
		if err != nil {
			closeEvalAPIServer(apiServer)
			restoreDemoGlobals()
			return nil, err
		}
	}
	var diskSnapshot *evalSQLiteSnapshot
	if spec.Resettable && spec.Target != gjeval.TargetDemo {
		if runtime != nil {
			cleanupAll(ctx, runtime.Cleanups)
		}
		restoreDemoGlobals()
		closeEvalAPIServer(apiServer)
		return nil, errors.New("resettable evaluation currently requires the SQLite demo target")
	}
	handler := &evalSwappableHandler{}
	var service *serv.HttpService
	startService := func() error {
		fresh, readErr := serv.ReadInConfig(filepath.Join(configPath, configName))
		if readErr != nil {
			return readErr
		}
		configureEvalInstance(fresh, spec)
		applySaaSOpsEvalAPIBaseURL(fresh, apiServer)
		// Each embedded service owns its config for its entire lifetime, so one
		// service can never observe another's being rebuilt.
		//
		// This used to also be load-bearing against a shutdown race: Close
		// returned while workers were still running, and they would read a config
		// the next boot was writing. That is fixed where it belonged — Close now
		// waits for them — and the copy stays because a service owning its own
		// configuration is right regardless.
		serviceConfig := *fresh
		// The demo keeps its trusted local dev identity, but Eval suppresses
		// named-query learning so an episode cannot mutate benchmark identity.
		options := []serv.Option{serv.OptionSetLogOutput(os.Stderr), serv.OptionDisableQueryLearning()}
		if e.ClientFactory != nil {
			options = append(options, serv.OptionSetAgentClientFactory(e.ClientFactory))
		}
		if e.MCPRecorder != nil {
			options = append(options, serv.OptionSetMCPToolRecorder(e.MCPRecorder))
		}
		if frozenOK {
			// The agent is told what "today" is. Freezing the data without
			// freezing this leaves relative questions drifting against fixed rows.
			options = append(options, serv.OptionSetAgentNow(func() time.Time { return frozenNow }))
		}
		if runtime != nil && len(runtime.Databases) != 0 {
			options = append(options, serv.OptionSetDatabases(runtime.Databases), serv.OptionSetRuntimeSchemaDDLDir(demoRuntimeSchemaDDLDir()))
		}
		created, createErr := serv.NewGraphJinService(&serviceConfig, options...)
		if createErr != nil {
			return createErr
		}
		mux := http.NewServeMux()
		if attachErr := created.Attach(mux); attachErr != nil {
			_ = created.Close()
			return attachErr
		}
		service = created
		handler.Set(mux)
		return nil
	}
	if err := startService(); err != nil {
		if diskSnapshot != nil {
			_ = diskSnapshot.Close()
		}
		if runtime != nil {
			cleanupAll(ctx, runtime.Cleanups)
		}
		restoreDemoGlobals()
		closeEvalAPIServer(apiServer)
		return nil, err
	}
	if spec.Resettable {
		// The managed control-plane database is created by service startup, not
		// demo provisioning. Close once before copying so the baseline contains
		// both application state and a checkpointed artifact/watch/task store.
		if err := service.Close(); err != nil {
			if runtime != nil {
				cleanupAll(ctx, runtime.Cleanups)
			}
			restoreDemoGlobals()
			closeEvalAPIServer(apiServer)
			return nil, err
		}
		if runtime != nil {
			cleanupAll(ctx, runtime.Cleanups)
		}
		diskSnapshot, err = newEvalSQLiteSnapshot(configPath)
		if err != nil {
			if runtime != nil {
				cleanupAll(ctx, runtime.Cleanups)
			}
			restoreDemoGlobals()
			closeEvalAPIServer(apiServer)
			return nil, err
		}
		cpath = configPath
		conf = &cloned
		runtime, err = StartDemo(ctx, []string{"sqlite"}, e.StatusOut)
		if err != nil {
			_ = diskSnapshot.Close()
			restoreDemoGlobals()
			closeEvalAPIServer(apiServer)
			return nil, err
		}
		if err := startService(); err != nil {
			_ = diskSnapshot.Close()
			if runtime != nil {
				cleanupAll(ctx, runtime.Cleanups)
			}
			restoreDemoGlobals()
			closeEvalAPIServer(apiServer)
			return nil, err
		}
	}
	server := httptest.NewServer(handler)
	headers := map[string]string{}
	if cloned.Auth.Development {
		headers["X-User-ID"] = "graphjin-eval"
		headers["X-User-Role"] = "user"
	}
	if token := strings.TrimSpace(os.Getenv("GRAPHJIN_EVAL_TOKEN")); token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	client := e.HTTPClient
	if client == nil {
		client = server.Client()
	}
	source := gjeval.HTTPCatalogSource{Client: client, BaseURL: server.URL, Headers: headers}
	snapshot, err := source.Snapshot(ctx)
	if err != nil {
		server.Close()
		_ = service.Close()
		if runtime != nil {
			cleanupAll(ctx, runtime.Cleanups)
		}
		restoreDemoGlobals()
		closeEvalAPIServer(apiServer)
		return nil, err
	}
	dataset := gjeval.DatasetFingerprint{CatalogHash: snapshot.Fingerprint}
	if spec.Target == gjeval.TargetDemo {
		dataset.DataAnchor, dataset.SeedManifestHash = evalDemoManifestFingerprint(configPath)
	}
	closed := false
	var lifecycle sync.Mutex
	closeFn := func() error {
		lifecycle.Lock()
		defer lifecycle.Unlock()
		if closed {
			return nil
		}
		closed = true
		server.Close()
		serviceErr := service.Close()
		if runtime != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			cleanupAll(shutdownCtx, runtime.Cleanups)
			cancel()
		}
		if diskSnapshot != nil {
			_ = diskSnapshot.Close()
		}
		closeEvalAPIServer(apiServer)
		restoreDemoGlobals()
		return serviceErr
	}
	base := &gjeval.StaticInstance{
		URL:            server.URL,
		RequestHeaders: headers,
		Dataset:        dataset,
		TargetLabel:    string(spec.Target),
		CloseFunc:      closeFn,
	}
	if diskSnapshot == nil {
		return base, nil
	}
	resetFn := func(resetCtx context.Context) error {
		lifecycle.Lock()
		defer lifecycle.Unlock()
		if closed {
			return errors.New("evaluation instance is closed")
		}
		handler.Set(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "evaluation reset in progress", http.StatusServiceUnavailable)
		}))
		if service != nil {
			_ = service.Close()
		}
		if runtime != nil {
			cleanupAll(resetCtx, runtime.Cleanups)
		}
		if err := diskSnapshot.Restore(); err != nil {
			return err
		}
		// A reset re-provisions, so it contends for the same command globals a
		// boot does.
		demoBootMu.Lock()
		defer demoBootMu.Unlock()
		cpath = configPath
		conf = &cloned
		// A reset re-provisions the demo, which is where the date shift happens.
		// Without the anchor still pinned here, a reset that lands after a UTC
		// midnight would hand the next episode a world dated a day later than
		// every episode before it.
		demoPinnedDataAnchor = dataAnchor
		var restartErr error
		runtime, restartErr = StartDemo(resetCtx, []string{"sqlite"}, e.StatusOut)
		if restartErr != nil {
			return restartErr
		}
		return startService()
	}
	return &gjeval.ResettableStaticInstance{StaticInstance: base, ResetFunc: resetFn}, nil
}

func configureEvalInstance(config *serv.Config, spec gjeval.EnvSpec) {
	config.Agent.Enabled = true
	config.Agent.ReadOnly = !spec.Writable
	config.Agent.ReturnTrace = true
	// A sampling run pins how the model draws; a benchmark leaves it alone.
	if spec.Temperature != nil {
		config.Agent.Temperature = spec.Temperature
	}
	if spec.TopP != nil {
		config.Agent.TopP = spec.TopP
	}
	config.WatchAndReload = false
	if spec.Reactive {
		config.Core.Watches.Runner = "all"
		// Eval owns this isolated instance, so a one-second revision poll and a
		// short subscription poll make delivery tests deterministic without
		// weakening production defaults.
		config.Core.Artifacts.PollSeconds = 1
		config.SubsPollDuration = 100 * time.Millisecond
	} else {
		config.Core.Watches.Runner = "off"
	}
	config.Core.Watches.EnrichmentWorkers = 0

	// No path fixups here: relative source paths (file roots, openapi specs
	// dirs) resolve against config_path in the engine itself now.
}

func startSaaSOpsEvalAPIMock(config *serv.Config) *httptest.Server {
	if config == nil {
		return nil
	}
	hasAccountHealth := false
	for _, source := range config.Core.Sources {
		if source.Name == "account_health_api" {
			hasAccountHealth = true
			break
		}
	}
	if !hasAccountHealth {
		return nil
	}
	health := map[string]map[string]any{
		"1": {"account_id": 1, "health": "red", "executive_owner": "Avery Chen", "open_risk_count": 4},
		"2": {"account_id": 2, "health": "green", "executive_owner": "Jordan Lee", "open_risk_count": 0},
		"3": {"account_id": 3, "health": "yellow", "executive_owner": "Morgan Diaz", "open_risk_count": 1},
		"4": {"account_id": 4, "health": "red", "executive_owner": "Sam Rivera", "open_risk_count": 3},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/account-health/")
		value, ok := health[id]
		if !ok {
			http.Error(w, `{"error":"account not found"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(value)
	}))
	applySaaSOpsEvalAPIBaseURL(config, server)
	return server
}

func applySaaSOpsEvalAPIBaseURL(config *serv.Config, server *httptest.Server) {
	if config == nil || server == nil {
		return
	}
	for sourceIndex := range config.Core.Sources {
		source := &config.Core.Sources[sourceIndex]
		if source.Name != "account_health_api" {
			continue
		}
		for name, spec := range source.Specs {
			spec.BaseURL = server.URL
			source.Specs[name] = spec
		}
	}
}

func closeEvalAPIServer(server *httptest.Server) {
	if server != nil {
		server.Close()
	}
}

type evalSwappableHandler struct {
	mu      sync.RWMutex
	handler http.Handler
}

func (h *evalSwappableHandler) Set(handler http.Handler) {
	h.mu.Lock()
	h.handler = handler
	h.mu.Unlock()
}

func (h *evalSwappableHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	handler := h.handler
	h.mu.RUnlock()
	if handler == nil {
		http.Error(w, "evaluation service unavailable", http.StatusServiceUnavailable)
		return
	}
	handler.ServeHTTP(w, r)
}

type evalSQLiteSnapshot struct {
	dir   string
	files map[string]string
}

func newEvalSQLiteSnapshot(configPath string) (*evalSQLiteSnapshot, error) {
	dir, err := os.MkdirTemp("", "graphjin-eval-snapshot-")
	if err != nil {
		return nil, err
	}
	snapshot := &evalSQLiteSnapshot{dir: dir, files: map[string]string{}}
	candidates := []string{filepath.Join(configPath, ".graphjin", "artifacts.sqlite3")}
	_ = filepath.WalkDir(filepath.Join(configPath, "demo", "databases"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr == nil && !entry.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".db" || ext == ".sqlite" || ext == ".sqlite3" {
				candidates = append(candidates, path)
			}
		}
		return nil
	})
	for index, source := range candidates {
		if _, statErr := os.Stat(source); statErr != nil {
			continue
		}
		target := filepath.Join(dir, fmt.Sprintf("%03d%s", index, filepath.Ext(source)))
		if err := copyEvalFile(source, target); err != nil {
			_ = snapshot.Close()
			return nil, err
		}
		snapshot.files[source] = target
	}
	if len(snapshot.files) == 0 {
		_ = snapshot.Close()
		return nil, errors.New("resettable SQLite evaluation found no database files to snapshot")
	}
	return snapshot, nil
}

func (s *evalSQLiteSnapshot) Restore() error {
	for target, source := range s.files {
		for _, sidecar := range []string{"-wal", "-shm", "-journal"} {
			if err := os.Remove(target + sidecar); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		if err := copyEvalFile(source, target); err != nil {
			return err
		}
	}
	return nil
}

func (s *evalSQLiteSnapshot) Close() error {
	if s == nil || s.dir == "" {
		return nil
	}
	return os.RemoveAll(s.dir)
}

func copyEvalFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func evalDemoManifestFingerprint(configPath string) (anchor, fingerprint string) {
	data, err := os.ReadFile(filepath.Join(configPath, "demo", "manifest.json"))
	if err != nil {
		return "", ""
	}
	var manifest map[string]any
	if json.Unmarshal(data, &manifest) != nil {
		return "", ""
	}
	anchor, _ = manifest["data_anchor"].(string)
	delete(manifest, "data_anchor")
	delete(manifest, "created_at")
	delete(manifest, "updated_at")
	canonical, _ := json.Marshal(manifest)
	sum := sha256.Sum256(canonical)
	return anchor, hex.EncodeToString(sum[:16])
}
