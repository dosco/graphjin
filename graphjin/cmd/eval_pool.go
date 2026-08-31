package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// demoBootMu serializes demo provisioning.
//
// StartDemo is CLI code that reads and writes package-level command state
// (cpath, conf, db, the pinned data anchor). Serving does not: each service
// owns its config and its own database handles, and each instance answers on
// its own listener. So several demo instances can serve at once as long as
// only one is being provisioned or reset at a time.
var demoBootMu sync.Mutex

// evalInstancePool runs several identical demo instances so episodes can hold a
// world each rather than taking turns on one.
type evalInstancePool struct {
	instances []gjeval.Instance
	free      chan gjeval.Instance
	dirs      []string
	closeOnce sync.Once
}

// newEvalInstancePool boots size copies of the project at base.ConfigPath.
//
// Every worker is given a fresh copy of the project's configuration and no
// runtime state, then provisions its own demo. The pool refuses to return
// unless every worker reports the same dataset: episodes are compared against
// oracles resolved once for the run, so a worker whose rows differ would score
// correct answers as wrong, and it would look like a model regression.
// The environment is built per worker rather than shared, because some ways of
// driving an episode need something of their own in each world — a mailbox a
// trainer's completions arrive through, a recorder collecting the calls one
// client made. A worker serves one episode at a time, so per-worker state
// belongs to exactly one episode without any further bookkeeping.
func newEvalInstancePool(ctx context.Context, envFor func(worker int) evalEnvironment,
	base gjeval.EnvSpec, size int) (*evalInstancePool, error) {
	if size < 1 {
		return nil, fmt.Errorf("pool size %d must be at least 1", size)
	}
	if envFor == nil {
		return nil, errors.New("a pool needs an environment for each worker")
	}
	if base.Target != gjeval.TargetDemo {
		return nil, errors.New("a pooled evaluation environment currently requires the demo target")
	}
	source, err := filepath.Abs(base.ConfigPath)
	if err != nil {
		return nil, err
	}
	pool := &evalInstancePool{free: make(chan gjeval.Instance, size)}
	for worker := 0; worker < size; worker++ {
		dir, err := os.MkdirTemp("", fmt.Sprintf("graphjin-eval-pool-%02d-", worker))
		if err != nil {
			pool.closeAfterFailure(ctx)
			return nil, err
		}
		pool.dirs = append(pool.dirs, dir)
		if err := copyEvalProjectSkeleton(source, dir); err != nil {
			pool.closeAfterFailure(ctx)
			return nil, fmt.Errorf("prepare pool worker %d: %w", worker, err)
		}
		spec := base
		spec.ConfigPath = dir
		instance, err := envFor(worker).Start(ctx, spec)
		if err != nil {
			pool.closeAfterFailure(ctx)
			return nil, fmt.Errorf("start pool worker %d: %w", worker, err)
		}
		pool.instances = append(pool.instances, instance)
		pool.free <- instance
	}
	if err := pool.assertOneWorld(); err != nil {
		pool.closeAfterFailure(ctx)
		return nil, err
	}
	return pool, nil
}

// assertOneWorld refuses a pool whose workers do not agree on the data.
func (p *evalInstancePool) assertOneWorld() error {
	if len(p.instances) < 2 {
		return nil
	}
	first := p.instances[0].Fingerprint()
	for i, instance := range p.instances[1:] {
		other := instance.Fingerprint()
		if !first.Equal(other) {
			return fmt.Errorf("pool worker %d serves a different dataset than worker 0 (%+v vs %+v); "+
				"episodes graded against one world cannot be scored against another", i+1, other, first)
		}
	}
	return nil
}

func (p *evalInstancePool) Acquire(ctx context.Context) (gjeval.Instance, error) {
	select {
	case instance := <-p.free:
		return instance, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *evalInstancePool) Release(instance gjeval.Instance) error {
	if instance == nil {
		return nil
	}
	select {
	case p.free <- instance:
		return nil
	default:
		// The channel is sized to the pool, so this can only mean the instance
		// did not come from here.
		return errors.New("released an instance this pool does not own")
	}
}

func (p *evalInstancePool) Size() int { return len(p.instances) }

func (p *evalInstancePool) Close() error {
	var err error
	p.closeOnce.Do(func() {
		for _, instance := range p.instances {
			if closeErr := instance.Close(); closeErr != nil && err == nil {
				err = closeErr
			}
		}
		for _, dir := range p.dirs {
			_ = os.RemoveAll(dir)
		}
	})
	return err
}

func (p *evalInstancePool) closeAfterFailure(context.Context) {
	_ = p.Close()
}

// evalProjectRuntimeDirs are the directories that hold a booted project's own
// state rather than its configuration.
var evalProjectRuntimeDirs = map[string]bool{
	"demo":            true,
	".graphjin":       true,
	".graphjin-evals": true,
	"codesql":         true,
	"node_modules":    true,
}

// copyEvalProjectSkeleton copies a project's configuration and nothing it
// produced by running.
//
// A worker that inherited another instance's demo state would boot onto rows
// dated for a different day and skip seeding entirely, since provisioning only
// runs on a first boot. It has to look like a project nobody has started yet.
func copyEvalProjectSkeleton(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		top := strings.Split(relative, string(os.PathSeparator))[0]
		if evalProjectRuntimeDirs[top] {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		return copyEvalFile(path, target)
	})
}

// poolForRun adapts an optional pool to the runner's interface. A nil *pool
// must not be handed over as a non-nil interface, or the runner would try to
// lease from nothing.
func poolForRun(pool *evalInstancePool) gjeval.InstancePool {
	if pool == nil {
		return nil
	}
	return pool
}
