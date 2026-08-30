package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

func TestProjectSkeletonCopiesConfigButNoRuntimeState(t *testing.T) {
	source := t.TempDir()
	write := func(relative, contents string) {
		t.Helper()
		path := filepath.Join(source, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("dev.yml", "app_name: test")
	write("queries/total.graphql", "query total { accounts { count_id } }")
	write("seed/app.js", "// seed")
	write("demo/manifest.json", `{"data_anchor":"2026-08-01"}`)
	write("demo/databases/app/graphjin_demo.db", "not really sqlite")
	write(".graphjin/artifacts.sqlite3", "not really sqlite")
	write(".graphjin-evals/runs/old.json", "{}")
	write("codesql/index.sqlite", "stale")

	destination := t.TempDir()
	if err := copyEvalProjectSkeleton(source, destination); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"dev.yml", "queries/total.graphql", "seed/app.js"} {
		if _, err := os.Stat(filepath.Join(destination, relative)); err != nil {
			t.Fatalf("configuration file %s was not copied: %v", relative, err)
		}
	}
	// A worker that inherited demo state would skip seeding entirely, because
	// provisioning only runs on a first boot, and would then serve rows dated
	// for whatever day the original was started on.
	for _, relative := range []string{"demo", ".graphjin", ".graphjin-evals", "codesql"} {
		if _, err := os.Stat(filepath.Join(destination, relative)); !os.IsNotExist(err) {
			t.Fatalf("runtime state %s leaked into the worker copy", relative)
		}
	}
}

func TestPoolRejectsNonDemoTargetAndBadSize(t *testing.T) {
	if _, err := newEvalInstancePool(context.Background(), evalEnvironment{}, gjeval.EnvSpec{Target: gjeval.TargetRemote}, 2); err == nil {
		t.Fatal("expected a remote target to be refused")
	}
	if _, err := newEvalInstancePool(context.Background(), evalEnvironment{}, gjeval.EnvSpec{Target: gjeval.TargetDemo}, 0); err == nil {
		t.Fatal("expected size 0 to be refused")
	}
}

// Episodes are graded against oracles resolved once for the whole run, so every
// worker has to serve the same rows. A pool that quietly mixed worlds would
// score correct answers as wrong and read as a model regression.
func TestPoolRefusesWorkersServingDifferentDatasets(t *testing.T) {
	pool := &evalInstancePool{instances: []gjeval.Instance{
		&gjeval.StaticInstance{Dataset: gjeval.DatasetFingerprint{DataAnchor: "2026-08-01", SeedManifestHash: "a", CatalogHash: "c"}},
		&gjeval.StaticInstance{Dataset: gjeval.DatasetFingerprint{DataAnchor: "2026-08-02", SeedManifestHash: "a", CatalogHash: "c"}},
	}}
	if err := pool.assertOneWorld(); err == nil {
		t.Fatal("expected mismatched datasets to be refused")
	}
	same := gjeval.DatasetFingerprint{DataAnchor: "2026-08-01", SeedManifestHash: "a", CatalogHash: "c"}
	agreeing := &evalInstancePool{instances: []gjeval.Instance{
		&gjeval.StaticInstance{Dataset: same}, &gjeval.StaticInstance{Dataset: same},
	}}
	if err := agreeing.assertOneWorld(); err != nil {
		t.Fatalf("identical datasets must be accepted: %v", err)
	}
}

func TestPoolLeasesEachInstanceToOneEpisodeAtATime(t *testing.T) {
	first := &gjeval.StaticInstance{URL: "http://one"}
	second := &gjeval.StaticInstance{URL: "http://two"}
	pool := &evalInstancePool{instances: []gjeval.Instance{first, second}, free: make(chan gjeval.Instance, 2)}
	pool.free <- first
	pool.free <- second

	a, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if a.BaseURL() == b.BaseURL() {
		t.Fatal("the same instance was leased twice at once")
	}
	// With everything leased, a third acquire must wait rather than hand out a
	// world someone else is writing to.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := pool.Acquire(ctx); err == nil {
		t.Fatal("expected acquire to block until an instance is free")
	}
	if err := pool.Release(a); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Acquire(context.Background()); err != nil {
		t.Fatalf("a released instance must be reusable: %v", err)
	}
}

// Two demo instances, each with its own project copy, must boot and serve at
// once and agree on their data. This is the claim the whole pool rests on: the
// command globals are touched only while provisioning, never while serving.
func TestPoolBootsIsolatedDemoInstancesThatAgree(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	project := t.TempDir()
	if err := extractDefaultDemo(project); err != nil {
		t.Fatal(err)
	}
	originalPath, originalConf, originalDB, originalOpened := cpath, conf, db, dbOpened
	defer func() {
		cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened
	}()
	t.Setenv("GO_ENV", "dev")

	client := &evalScriptClient{code: `await final({status:"blocked",answer:"not configured"});`}
	environment := evalEnvironment{ClientFactory: func(gjagent.Config) (ax.AIClient, error) { return client, nil }}
	pool, err := newEvalInstancePool(context.Background(), environment, gjeval.EnvSpec{
		Target: gjeval.TargetDemo, ConfigPath: project, Seed: 23, FreezeTime: "2026-08-01T12:00:00Z",
	}, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close() //nolint:errcheck

	if pool.Size() != 2 {
		t.Fatalf("expected 2 workers, got %d", pool.Size())
	}
	one, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	two, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if one.BaseURL() == two.BaseURL() {
		t.Fatal("both workers answered on the same address")
	}
	if !one.Fingerprint().Equal(two.Fingerprint()) {
		t.Fatalf("workers disagree on their data: %+v vs %+v", one.Fingerprint(), two.Fingerprint())
	}

	// Both must serve at the same time. Provisioning is serialized; answering
	// queries is not, and a pool that could not overlap would buy nothing.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, instance := range []gjeval.Instance{one, two} {
		wg.Add(1)
		go func(i int, instance gjeval.Instance) {
			defer wg.Done()
			_, errs[i] = askEvalAgent(t, instance, "How many accounts are there?")
		}(i, instance)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d failed to serve concurrently: %v", i, err)
		}
	}
}
