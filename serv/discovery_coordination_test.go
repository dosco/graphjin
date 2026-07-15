package serv

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/afero"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap/zaptest"
	_ "modernc.org/sqlite"
)

func startRedisUnixForTest(t *testing.T) *redis.Client {
	t.Helper()
	binary, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server is not installed")
	}
	dir, err := os.MkdirTemp("/tmp", "gjr-")
	if err != nil {
		t.Skipf("create short Redis socket directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "redis.sock")
	cmd := exec.Command(binary,
		"--port", "0",
		"--unixsocket", socket,
		"--unixsocketperm", "700",
		"--save", "",
		"--appendonly", "no",
	)
	if err := cmd.Start(); err != nil {
		t.Skipf("start redis-server: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	client := redis.NewClient(&redis.Options{Network: "unix", Addr: socket})
	deadline := time.Now().Add(3 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		err = client.Ping(ctx).Err()
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			client.Close() //nolint:errcheck
			t.Skipf("redis-server did not become ready: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func newCoordinationTestService(t *testing.T, fs core.FS, db *sql.DB) *graphjinService {
	t.Helper()
	conf := &Config{Core: core.Config{DBType: "sqlite", DisableAllowList: true}, Serv: Serv{
		DiscoveryCache: DiscoveryCacheConfig{Path: ".graphjin/discovery", StartupWait: 3 * time.Second, RetainGenerations: 2},
	}}
	return &graphjinService{
		conf:   conf,
		dbs:    map[string]*sql.DB{core.DefaultDBName: db},
		fs:     fs,
		log:    zaptest.NewLogger(t).Sugar(),
		tracer: otel.Tracer("discovery-coordination-test"),
	}
}

func newCoordinationTestManager(t *testing.T, service *graphjinService, client *redis.Client, node, prefix string) *discoveryGenerationManager {
	t.Helper()
	fingerprint, err := discoveryConfigFingerprint(service.conf)
	if err != nil {
		t.Fatal(err)
	}
	return &discoveryGenerationManager{
		service: service, conf: service.conf.DiscoveryCache,
		base: service.conf.DiscoveryCache.Path, fingerprint: fingerprint,
		prefix: prefix, nodeID: node, redis: client,
	}
}

func TestDiscoveryRedisCoordinatesColdBuilderWarmFollowersAndFencing(t *testing.T) {
	client := startRedisUnixForTest(t)
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "coordination.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY, email TEXT)`); err != nil {
		t.Fatal(err)
	}
	fs := newAferoFS(afero.NewMemMapFs(), "/")
	serviceA := newCoordinationTestService(t, fs, db)
	serviceB := newCoordinationTestService(t, fs, db)
	prefix := "gj:test:discovery:" + strings.ReplaceAll(t.Name(), "/", ":")
	managerA := newCoordinationTestManager(t, serviceA, client, "node-a", prefix)
	managerB := newCoordinationTestManager(t, serviceB, client, "node-b", prefix)
	options := []core.Option{core.OptionSetFS(fs), core.OptionSetDBSchemaWatcherDisabled(true)}

	type result struct {
		dir string
		err error
	}
	results := make(chan result, 2)
	var start sync.WaitGroup
	start.Add(1)
	for _, item := range []struct {
		manager *discoveryGenerationManager
		service *graphjinService
	}{{managerA, serviceA}, {managerB, serviceB}} {
		go func(item struct {
			manager *discoveryGenerationManager
			service *graphjinService
		}) {
			start.Wait()
			dir, err := item.manager.InitialGeneration(context.Background(), &item.service.conf.Core, options)
			results <- result{dir: dir, err: err}
		}(item)
	}
	start.Done()
	first, second := <-results, <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("cold coordinated start: first=%v second=%v", first.err, second.err)
	}
	if first.dir != second.dir {
		t.Fatalf("followers selected different generations: %q != %q", first.dir, second.dir)
	}
	receipts, err := fs.List(filepath.Join(serviceA.conf.DiscoveryCache.Path, "activations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 1 {
		t.Fatalf("cold start activated %d generations, want one: %v", len(receipts), receipts)
	}

	warmStart := time.Now()
	managerC := newCoordinationTestManager(t, newCoordinationTestService(t, fs, db), client, "node-c", prefix)
	warmDir, err := managerC.InitialGeneration(context.Background(), &managerC.service.conf.Core, options)
	if err != nil || warmDir != first.dir {
		t.Fatalf("warm follower: dir=%q err=%v", warmDir, err)
	}
	if elapsed := time.Since(warmStart); elapsed > time.Second {
		t.Fatalf("warm follower took %s", elapsed)
	}

	stalePrefix := prefix + ":stale"
	staleA := newCoordinationTestManager(t, serviceA, client, "stale-a", stalePrefix)
	staleB := newCoordinationTestManager(t, serviceB, client, "stale-b", stalePrefix)
	leaseA, won, err := staleA.acquire(context.Background(), 40*time.Millisecond)
	if err != nil || !won {
		t.Fatalf("first stale lease: won=%v err=%v", won, err)
	}
	time.Sleep(80 * time.Millisecond)
	leaseB, won, err := staleB.acquire(context.Background(), time.Second)
	if err != nil || !won {
		t.Fatalf("replacement lease: won=%v err=%v", won, err)
	}
	manifestA := discoveryGenerationManifest{FormatVersion: discoveryGenerationFormatVersion, GenerationID: "stale-generation", Fingerprint: staleA.fingerprint}
	if err := staleA.activate(context.Background(), leaseA, manifestA); err == nil {
		t.Fatal("stale writer activation unexpectedly succeeded")
	}
	manifestB := discoveryGenerationManifest{FormatVersion: discoveryGenerationFormatVersion, GenerationID: "current-generation", Fingerprint: staleB.fingerprint}
	if err := staleB.activate(context.Background(), leaseB, manifestB); err != nil {
		t.Fatalf("current writer activation: %v", err)
	}
	active, err := staleB.redisActive(context.Background())
	if err != nil || active != manifestB.GenerationID {
		t.Fatalf("active generation=%q err=%v", active, err)
	}
}

func TestDiscoveryRedisOutageUsesValidFilesystemGenerationAndRejectsColdStampede(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "outage.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	fs := newAferoFS(afero.NewMemMapFs(), "/")
	service := newCoordinationTestService(t, fs, db)
	local := newCoordinationTestManager(t, service, nil, "local", "gj:test:local")
	options := []core.Option{core.OptionSetFS(fs), core.OptionSetDBSchemaWatcherDisabled(true)}
	dir, err := local.InitialGeneration(context.Background(), &service.conf.Core, options)
	if err != nil {
		t.Fatal(err)
	}
	outage := newCoordinationTestManager(t, service, nil, "outage", "gj:test:outage")
	outage.redisErr = errors.New("redis unavailable")
	staleDir, err := outage.InitialGeneration(context.Background(), &service.conf.Core, options)
	if err != nil || staleDir != dir || !outage.degraded {
		t.Fatalf("stale startup: dir=%q degraded=%v err=%v", staleDir, outage.degraded, err)
	}
	service.gj = &core.GraphJin{}
	if err := outage.RefreshNow(context.Background()); err == nil || !strings.Contains(err.Error(), "suspended") {
		t.Fatalf("Redis outage refresh error = %v, want suspended coordinated rebuild", err)
	}

	emptyFS := newAferoFS(afero.NewMemMapFs(), "/")
	coldService := newCoordinationTestService(t, emptyFS, db)
	coldService.conf.DiscoveryCache.StartupWait = time.Millisecond
	cold := newCoordinationTestManager(t, coldService, nil, "cold", "gj:test:cold")
	cold.redisErr = errors.New("redis unavailable")
	if _, err := cold.InitialGeneration(context.Background(), &coldService.conf.Core, []core.Option{core.OptionSetFS(emptyFS)}); err == nil {
		t.Fatal("cold startup without Redis or a filesystem generation unexpectedly succeeded")
	}
}

func TestSingleNodeDiscoveryAndSemanticWorkWithoutRedis(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "single-node.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY, email TEXT)`); err != nil {
		t.Fatal(err)
	}
	fs := newAferoFS(afero.NewMemMapFs(), "/")
	service := newCoordinationTestService(t, fs, db)
	service.conf.CatalogSearch.Semantic = SemanticCatalogSearchConfig{
		Enabled: true, Provider: "openai", EmbeddingModel: "fake", Dimensions: "tiny",
	}
	client := &deterministicEmbeddingClient{dimension: 128}
	service.semanticEmbedder = client
	manager, err := newDiscoveryGenerationManager(service)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if manager.redis != nil || manager.redisErr != nil {
		t.Fatalf("single-node coordinator unexpectedly requires Redis: client=%v err=%v", manager.redis != nil, manager.redisErr)
	}
	options := []core.Option{core.OptionSetFS(fs), core.OptionSetDBSchemaWatcherDisabled(true)}
	dir, err := manager.InitialGeneration(context.Background(), &service.conf.Core, options)
	if err != nil {
		t.Fatal(err)
	}
	service.gj, err = core.NewGraphJin(&service.conf.Core, db,
		core.OptionSetFS(fs),
		core.OptionSetDBSchemaWatcherDisabled(true),
		core.OptionSetRuntimeSchemaDDLDir(dir),
		core.OptionSetRuntimeSchemaCacheFirst(true),
		core.OptionSetRuntimeSchemaCacheRequired(true),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer service.gj.Close()
	service.discovery = manager
	initialID := manager.currentDiscoveryID()

	if _, err := db.Exec(`CREATE TABLE orders (id INTEGER PRIMARY KEY, customer_id INTEGER REFERENCES customers(id))`); err != nil {
		t.Fatal(err)
	}
	if err := manager.RefreshNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if manager.currentDiscoveryID() == initialID || !tableNamedForService(service.gj.GetTables(), "orders") {
		t.Fatalf("single-node refresh did not activate the new schema: initial=%q active=%q", initialID, manager.currentDiscoveryID())
	}

	semantic, err := newSemanticCatalogIndex(service)
	if err != nil {
		t.Fatal(err)
	}
	if semantic.redis != nil || semantic.redisErr != nil {
		t.Fatalf("single-node semantic index unexpectedly requires Redis: client=%v err=%v", semantic.redis != nil, semantic.redisErr)
	}
	semantic.ensureCurrent(context.Background())
	if semantic.current() == nil {
		t.Fatal("single-node semantic index was not built")
	}
	if calls, texts, _ := client.stats(); calls == 0 || texts == 0 {
		t.Fatalf("single-node semantic build made no embedding calls: calls=%d texts=%d", calls, texts)
	}

	client.reset()
	warmSemantic, err := newSemanticCatalogIndex(service)
	if err != nil {
		t.Fatal(err)
	}
	warmSemantic.Start()
	defer warmSemantic.Close()
	if warmSemantic.current() == nil {
		t.Fatal("single-node warm semantic startup did not load the filesystem index")
	}
	warmSemantic.ensureCurrent(context.Background())
	if calls, texts, _ := client.stats(); calls != 0 || texts != 0 {
		t.Fatalf("single-node warm startup embedded documents: calls=%d texts=%d", calls, texts)
	}
}

func TestDiscoveryUnchangedRefreshProducesNoNewGeneration(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "unchanged.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	fs := newAferoFS(afero.NewMemMapFs(), "/")
	service := newCoordinationTestService(t, fs, db)
	manager := newCoordinationTestManager(t, service, nil, "local", "gj:test:unchanged")
	options := []core.Option{core.OptionSetFS(fs), core.OptionSetDBSchemaWatcherDisabled(true)}
	dir, err := manager.InitialGeneration(context.Background(), &service.conf.Core, options)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := manager.buildGeneration(context.Background(), &service.conf.Core, options)
	if err != nil {
		t.Fatal(err)
	}
	if !candidate.unchanged || manager.generationDir(candidate.GenerationID) != dir {
		t.Fatalf("unchanged candidate created a new generation: %+v", candidate)
	}
	receipts, err := fs.List(filepath.Join(service.conf.DiscoveryCache.Path, "activations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 1 {
		t.Fatalf("unchanged refresh activated %d generations, want one", len(receipts))
	}
}

func TestDiscoveryConfigChangeRotatesCoordinatorAndActivatesGeneration(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "config-change.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	fs := newAferoFS(afero.NewMemMapFs(), "/")
	service := newCoordinationTestService(t, fs, db)
	manager := newCoordinationTestManager(t, service, nil, "local", "gj:test:config-change")
	options := []core.Option{core.OptionSetFS(fs), core.OptionSetDBSchemaWatcherDisabled(true)}
	dir, err := manager.InitialGeneration(context.Background(), &service.conf.Core, options)
	if err != nil {
		t.Fatal(err)
	}
	service.gj, err = core.NewGraphJin(&service.conf.Core, db,
		core.OptionSetFS(fs),
		core.OptionSetDBSchemaWatcherDisabled(true),
		core.OptionSetRuntimeSchemaDDLDir(dir),
		core.OptionSetRuntimeSchemaCacheFirst(true),
		core.OptionSetRuntimeSchemaCacheRequired(true),
	)
	if err != nil {
		t.Fatal(err)
	}
	service.discovery = manager
	oldFingerprint := manager.fingerprint
	service.conf.Core.Blocklist = []string{"audit_.*"}
	service.gj.Close()
	service.gj, err = core.NewGraphJin(&service.conf.Core, db,
		core.OptionSetFS(fs),
		core.OptionSetDBSchemaWatcherDisabled(true),
		core.OptionSetRuntimeSchemaDDLDir("staged-config-validation"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer service.gj.Close()
	stable := service.gj

	if err := service.reconfigureDiscoveryAfterConfigChange(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer service.discovery.Close()
	if service.gj != stable {
		t.Fatal("config-coordinated activation replaced the stable GraphJin pointer")
	}
	if service.discovery.fingerprint == oldFingerprint || service.discovery.currentDiscoveryID() == "" {
		t.Fatalf("coordinator did not rotate and activate: old=%s new=%s active=%q",
			oldFingerprint, service.discovery.fingerprint, service.discovery.currentDiscoveryID())
	}
	if !tableNamedForService(service.gj.GetTables(), "customers") {
		t.Fatal("config-coordinated cache activation lost the discovered schema")
	}
}

func tableNamedForService(tables []core.TableInfo, name string) bool {
	for _, table := range tables {
		if table.Name == name {
			return true
		}
	}
	return false
}

func TestDiscoveryActiveGenerationMissingFromSharedFilesystemDoesNotRebuildLocally(t *testing.T) {
	client := startRedisUnixForTest(t)
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "missing-shared.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	fs := newAferoFS(afero.NewMemMapFs(), "/")
	service := newCoordinationTestService(t, fs, db)
	service.conf.DiscoveryCache.StartupWait = 20 * time.Millisecond
	prefix := "gj:test:missing-shared:" + strings.ReplaceAll(t.Name(), "/", ":")
	manager := newCoordinationTestManager(t, service, client, "isolated-node", prefix)
	if err := client.Set(context.Background(), prefix+":active", "remote-generation", 0).Err(); err != nil {
		t.Fatal(err)
	}
	_, err = manager.InitialGeneration(context.Background(), &service.conf.Core, []core.Option{core.OptionSetFS(fs)})
	if err == nil || !strings.Contains(err.Error(), "not readable") {
		t.Fatalf("missing shared generation error = %v", err)
	}
	if entries, listErr := fs.List(filepath.Join(service.conf.DiscoveryCache.Path, "activations")); listErr == nil && len(entries) != 0 {
		t.Fatalf("isolated replica activated local generations: %v", entries)
	}
}

func TestSemanticRedisGenerationHandoffBuildsDocumentsOnce(t *testing.T) {
	client := startRedisUnixForTest(t)
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "semantic-handoff.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE metrics (id INTEGER PRIMARY KEY, revenue NUMERIC, recorded_at TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	fs := newAferoFS(afero.NewMemMapFs(), "/")
	coreConf := core.Config{DBType: "sqlite", DisableAllowList: true}
	gj, err := core.NewGraphJin(&coreConf, db, core.OptionSetFS(fs), core.OptionSetDBSchemaWatcherDisabled(true))
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()
	conf := &Config{Core: coreConf, Serv: Serv{
		DiscoveryCache: DiscoveryCacheConfig{Path: ".graphjin/discovery", RetainGenerations: 2},
		CatalogSearch: CatalogSearchConfig{Semantic: SemanticCatalogSearchConfig{
			Enabled: true, Provider: "fake", EmbeddingModel: "fake-v1", Dimensions: "tiny",
		}},
	}}
	embedder := &deterministicEmbeddingClient{dimension: 128}
	serviceA := &graphjinService{conf: conf, gj: gj, fs: fs, dbs: map[string]*sql.DB{core.DefaultDBName: db}, log: zaptest.NewLogger(t).Sugar(), semanticEmbedder: embedder}
	serviceB := &graphjinService{conf: conf, gj: gj, fs: fs, dbs: map[string]*sql.DB{core.DefaultDBName: db}, log: zaptest.NewLogger(t).Sugar(), semanticEmbedder: embedder}
	indexA, err := newSemanticCatalogIndex(serviceA)
	if err != nil {
		t.Fatal(err)
	}
	indexB, err := newSemanticCatalogIndex(serviceB)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "gj:test:semantic:" + strings.ReplaceAll(t.Name(), "/", ":")
	indexA.redis, indexA.prefix = client, prefix
	indexB.redis, indexB.prefix = client, prefix
	indexA.Start()
	indexB.Start()
	defer indexA.Close()
	defer indexB.Close()

	deadline := time.Now().Add(5 * time.Second)
	for {
		activeA, activeB := indexA.current(), indexB.current()
		if activeA != nil && activeB != nil && activeA.manifest.GenerationID == activeB.manifest.GenerationID {
			_, embeddedTexts, _ := embedder.stats()
			if embeddedTexts != activeA.manifest.DocumentCount {
				t.Fatalf("semantic handoff embedded %d documents for a %d-document index", embeddedTexts, activeA.manifest.DocumentCount)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("semantic generation was not handed to both followers: a=%v b=%v", activeA != nil, activeB != nil)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
