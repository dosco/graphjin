package serv

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/fsnotify/fsnotify"
)

const localFilesystemWatchDebounce = 300 * time.Millisecond

type localFSWatchRoot struct {
	Name string
	Root string
}

type fsWatchLogger interface {
	Warnf(string, ...interface{})
}

func (s *graphjinService) startLocalFilesystemCacheWatchers() error {
	if s == nil || s.cache == nil || s.conf == nil {
		return nil
	}
	roots, err := s.localFilesystemWatchRoots()
	if err != nil {
		return err
	}
	if len(roots) == 0 {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := startLocalFilesystemCacheWatcher(ctx, s.cache, roots, s.log); err != nil {
		cancel()
		return err
	}
	s.addCloseFn(cancel)
	return nil
}

func (s *graphjinService) localFilesystemWatchRoots() ([]localFSWatchRoot, error) {
	if s == nil || s.conf == nil {
		return nil, nil
	}
	roots := make([]localFSWatchRoot, 0, len(s.conf.Core.Filesystems))
	for _, fc := range s.conf.Core.Filesystems {
		if fc.ReadOnly || !strings.EqualFold(fc.Backend, "local") || strings.TrimSpace(fc.Root) == "" {
			continue
		}
		root, err := filepath.Abs(fc.Root)
		if err != nil {
			return nil, fmt.Errorf("filesystem watcher %s: root: %w", fc.Name, err)
		}
		roots = append(roots, localFSWatchRoot{Name: fc.Name, Root: root})
	}
	return roots, nil
}

func (s *graphjinService) addCloseFn(fn func()) {
	if fn == nil {
		return
	}
	prev := s.closeFn
	if prev == nil {
		s.closeFn = fn
		return
	}
	s.closeFn = func() {
		fn()
		prev()
	}
}

func startLocalFilesystemCacheWatcher(
	ctx context.Context,
	cache ResponseCache,
	roots []localFSWatchRoot,
	log fsWatchLogger,
) error {
	if cache == nil || len(roots) == 0 {
		return nil
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("filesystem watcher: %w", err)
	}

	for i := range roots {
		root, err := filepath.Abs(roots[i].Root)
		if err != nil {
			watcher.Close() //nolint:errcheck
			return fmt.Errorf("filesystem watcher %s: root: %w", roots[i].Name, err)
		}
		roots[i].Root = root
		if err := addLocalFilesystemWatchDirs(watcher, root); err != nil {
			watcher.Close() //nolint:errcheck
			return fmt.Errorf("filesystem watcher %s: %w", roots[i].Name, err)
		}
	}

	go runLocalFilesystemCacheWatcher(ctx, watcher, cache, roots, log)
	return nil
}

func runLocalFilesystemCacheWatcher(
	ctx context.Context,
	watcher *fsnotify.Watcher,
	cache ResponseCache,
	roots []localFSWatchRoot,
	log fsWatchLogger,
) {
	defer watcher.Close() //nolint:errcheck

	pending := make(map[string]core.RowRef)
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}

	flush := func() {
		if len(pending) == 0 {
			return
		}
		refs := make([]core.RowRef, 0, len(pending))
		for _, ref := range pending {
			refs = append(refs, ref)
		}
		pending = make(map[string]core.RowRef)
		if err := cache.InvalidateRows(context.Background(), refs); err != nil && log != nil {
			log.Warnf("filesystem cache invalidation failed: %s", err)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case err := <-watcher.Errors:
			if err != nil && log != nil {
				log.Warnf("filesystem watcher error: %s", err)
			}
		case ev := <-watcher.Events:
			if ev.Name == "" || ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename|fsnotify.Chmod) == 0 {
				continue
			}
			root, ok := matchLocalFilesystemRoot(roots, ev.Name)
			if !ok {
				continue
			}
			if ev.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					_ = addLocalFilesystemWatchDirs(watcher, ev.Name)
				}
			}
			for _, ref := range localFilesystemEventRefs(root, ev.Name) {
				pending[ref.DependencyKey()] = ref
			}
			resetTimer(timer, localFilesystemWatchDebounce)
		case <-timer.C:
			flush()
		}
	}
}

func addLocalFilesystemWatchDirs(watcher *fsnotify.Watcher, root string) error {
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", root)
	}
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		return watcher.Add(path)
	})
}

func matchLocalFilesystemRoot(roots []localFSWatchRoot, path string) (localFSWatchRoot, bool) {
	clean := filepath.Clean(path)
	var best localFSWatchRoot
	for _, root := range roots {
		r := filepath.Clean(root.Root)
		if clean == r || strings.HasPrefix(clean, r+string(filepath.Separator)) {
			if len(r) > len(best.Root) {
				best = root
			}
		}
	}
	return best, best.Root != ""
}

func localFilesystemEventRefs(root localFSWatchRoot, path string) []core.RowRef {
	rel, err := filepath.Rel(root.Root, path)
	if err != nil || rel == "." {
		return core.FilesystemPrefixRefs(root.Name, "")
	}
	key := filepath.ToSlash(rel)
	refs := core.FilesystemKeyRefs(root.Name, key)
	refs = append(refs, core.FilesystemPrefixRefs(root.Name, key)...)
	return refs
}

func resetTimer(timer *time.Timer, d time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(d)
}
