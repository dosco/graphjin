//go:build cgo

package codesql

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

func (idx *indexer) Watch(ctx context.Context) error {
	w, err := idx.newWatcher()
	if err != nil {
		return err
	}
	defer w.Close() //nolint:errcheck
	return idx.watch(ctx, w)
}

func (idx *indexer) newWatcher() (*fsnotify.Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := idx.addWatchDirs(w); err != nil {
		w.Close() //nolint:errcheck
		return nil, err
	}
	return w, nil
}

func (idx *indexer) watch(ctx context.Context, w *fsnotify.Watcher) error {
	pending := make(map[string]struct{})
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	flush := func() {
		if len(pending) == 0 {
			return
		}
		pending = make(map[string]struct{})
		_, _ = idx.Reconcile(context.Background())
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-w.Errors:
			if err != nil {
				return err
			}
		case ev := <-w.Events:
			if ev.Name == "" || shouldIgnorePath(idx.root, ev.Name) {
				continue
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			if info, err := os.Stat(ev.Name); err == nil && info.IsDir() && ev.Op&fsnotify.Create != 0 {
				_ = filepath.WalkDir(ev.Name, func(path string, d os.DirEntry, err error) error {
					if err != nil || !d.IsDir() {
						return nil
					}
					if shouldSkipDir(d.Name()) {
						return filepath.SkipDir
					}
					_ = w.Add(path)
					return nil
				})
			}
			pending[ev.Name] = struct{}{}
			timer.Reset(300 * time.Millisecond)
		case <-timer.C:
			flush()
		}
	}
}

func (idx *indexer) addWatchDirs(w *fsnotify.Watcher) error {
	return filepath.WalkDir(idx.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if shouldIgnorePath(idx.root, path) || shouldSkipDir(d.Name()) {
			return filepath.SkipDir
		}
		return w.Add(path)
	})
}

func shouldIgnorePath(root, path string) bool {
	clean := filepath.Clean(path)
	if strings.HasPrefix(clean, filepath.Join(root, "config", "codesql")) {
		return true
	}
	base := filepath.Base(clean)
	return strings.HasSuffix(base, ".sqlite") || strings.HasSuffix(base, ".sqlite-wal") || strings.HasSuffix(base, ".sqlite-shm")
}
