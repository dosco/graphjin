package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/dosco/graphjin/serv/v3"
)

type evalEnvironment struct {
	ClientFactory gjagent.ClientFactory
	HTTPClient    *http.Client
	StatusOut     *os.File
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
	cloned.Agent.Enabled = true
	cloned.Agent.ReadOnly = true
	cloned.Agent.ReturnTrace = true
	cloned.WatchAndReload = false
	cloned.Core.Watches.Runner = "off"
	cloned.Core.Watches.EnrichmentWorkers = 0

	var runtime *DemoRuntime
	restoreDemoGlobals := func() {}
	if spec.Target == gjeval.TargetDemo {
		demoStatePath := filepath.Join(configPath, "demo")
		localStatePath := filepath.Join(configPath, ".graphjin")
		if _, demoErr := os.Stat(demoStatePath); os.IsNotExist(demoErr) {
			if _, localErr := os.Stat(localStatePath); localErr == nil {
				return nil, fmt.Errorf("refusing to provision a fresh demo over existing %s; start or reset the demo explicitly first", localStatePath)
			}
		}
		// StartDemo is existing CLI provisioning code and uses these command
		// globals. The eval command is single-shot, so there is no concurrent
		// command state to contend with.
		previousPath, previousConf, previousDB, previousOpened := cpath, conf, db, dbOpened
		restoreDemoGlobals = func() {
			cpath, conf, db, dbOpened = previousPath, previousConf, previousDB, previousOpened
		}
		cpath = configPath
		conf = &cloned
		runtime, err = StartDemo(ctx, []string{"sqlite"}, e.StatusOut)
		if err != nil {
			restoreDemoGlobals()
			return nil, err
		}
	}
	options := []serv.Option{serv.OptionSetLogOutput(os.Stderr)}
	if e.ClientFactory != nil {
		options = append(options, serv.OptionSetAgentClientFactory(e.ClientFactory))
	}
	if runtime != nil && len(runtime.Databases) != 0 {
		options = append(options,
			serv.OptionSetDatabases(runtime.Databases),
			serv.OptionSetRuntimeSchemaDDLDir(demoRuntimeSchemaDDLDir()),
		)
	}
	service, err := serv.NewGraphJinService(&cloned, options...)
	if err != nil {
		if runtime != nil {
			cleanupAll(ctx, runtime.Cleanups)
		}
		restoreDemoGlobals()
		return nil, err
	}
	mux := http.NewServeMux()
	if err := service.Attach(mux); err != nil {
		_ = service.Close()
		if runtime != nil {
			cleanupAll(ctx, runtime.Cleanups)
		}
		restoreDemoGlobals()
		return nil, err
	}
	server := httptest.NewServer(mux)
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
		return nil, err
	}
	dataset := gjeval.DatasetFingerprint{CatalogHash: snapshot.Fingerprint}
	if spec.Target == gjeval.TargetDemo {
		dataset.DataAnchor, dataset.SeedManifestHash = evalDemoManifestFingerprint(configPath)
	}
	closed := false
	closeFn := func() error {
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
		restoreDemoGlobals()
		return serviceErr
	}
	return &gjeval.StaticInstance{
		URL:            server.URL,
		RequestHeaders: headers,
		Dataset:        dataset,
		TargetLabel:    string(spec.Target),
		CloseFunc:      closeFn,
	}, nil
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
