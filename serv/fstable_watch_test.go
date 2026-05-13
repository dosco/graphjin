package serv

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

func TestLocalFilesystemCacheWatcherInvalidatesOnFileChanges(t *testing.T) {
	root := t.TempDir()
	cache, err := NewMemoryCache(CachingConfig{TTL: 3600}, 100)
	if err != nil {
		t.Fatalf("memory cache: %v", err)
	}
	defer cache.Close() //nolint:errcheck

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := startLocalFilesystemCacheWatcher(ctx, cache, []localFSWatchRoot{{Name: "uploads", Root: root}}, nil); err != nil {
		t.Fatalf("start watcher: %v", err)
	}

	cases := []struct {
		name  string
		key   string
		setup func(string) error
		event func(string) error
	}{
		{
			name: "create",
			key:  "create.txt",
			event: func(path string) error {
				return os.WriteFile(path, []byte("one"), 0o644)
			},
		},
		{
			name: "update",
			key:  "update.txt",
			event: func(path string) error {
				if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
					return err
				}
				return os.WriteFile(path, []byte("two"), 0o644)
			},
		},
		{
			name: "rename",
			key:  "rename.txt",
			event: func(path string) error {
				tmp := path + ".tmp"
				if err := os.WriteFile(tmp, []byte("one"), 0o644); err != nil {
					return err
				}
				return os.Rename(tmp, path)
			},
		},
		{
			name: "delete",
			key:  "delete.txt",
			setup: func(path string) error {
				return os.WriteFile(path, []byte("one"), 0o644)
			},
			event: func(path string) error {
				return os.Remove(path)
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(root, tt.key)
			if tt.setup != nil {
				if err := tt.setup(path); err != nil {
					t.Fatalf("setup: %v", err)
				}
				time.Sleep(localFilesystemWatchDebounce + 100*time.Millisecond)
			}
			cacheKey := "listing-" + tt.name
			if err := cache.Set(context.Background(), cacheKey, []byte(`[]`), core.FilesystemPrefixRefs("uploads", ""), time.Now()); err != nil {
				t.Fatalf("cache set: %v", err)
			}
			if err := tt.event(path); err != nil {
				t.Fatalf("event: %v", err)
			}
			waitForCacheMiss(t, cache, cacheKey)
		})
	}
}

func TestLocalFilesystemWatchRootsEnabledInProduction(t *testing.T) {
	root := t.TempDir()
	s := &graphjinService{
		conf: &Config{
			Core: Core{
				Filesystems: []core.FilesystemConfig{{
					Name:    "uploads",
					Backend: "local",
					Root:    root,
				}},
			},
			Serv: Serv{Production: true},
		},
	}

	roots, err := s.localFilesystemWatchRoots()
	if err != nil {
		t.Fatalf("watch roots: %v", err)
	}
	if len(roots) != 1 || roots[0].Name != "uploads" {
		t.Fatalf("watch roots = %+v, want uploads root", roots)
	}
}

func TestLocalFilesystemWatchRootsSkipReadOnly(t *testing.T) {
	root := t.TempDir()
	s := &graphjinService{
		conf: &Config{
			Core: Core{
				Filesystems: []core.FilesystemConfig{{
					Name:     "uploads",
					Backend:  "local",
					Root:     root,
					ReadOnly: true,
				}},
			},
			Serv: Serv{Production: true},
		},
	}

	roots, err := s.localFilesystemWatchRoots()
	if err != nil {
		t.Fatalf("watch roots: %v", err)
	}
	if len(roots) != 0 {
		t.Fatalf("watch roots = %+v, want none for read-only filesystem", roots)
	}
}

func waitForCacheMiss(t *testing.T, cache *MemoryCache, key string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, found := cache.Get(context.Background(), key); !found {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("cache key %q was not invalidated", key)
}
