package serv

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/pkg/errors"
)

type dir struct {
	path string
	cb   func()
}

// Dir is an additional directory to watch for changes. Directories are watched
// non-recursively.
//
// The second argument is the callback that to run when the directory changes.
// Use reload.ReExec(s) to restart the process.
func watchDir(path string, cb func()) dir { return dir{path, cb} } // nolint: golint

// Do reload the current process when its binary changes.
//
// The log function is used to display an informational startup message and
// errors. It works well with e.g. the standard log package or Logrus.
//
// The error return will only return initialisation errors. Once initialized it
// will use the log function to print errors, rather than return.
func startConfigWatcher(s *Service, additional ...dir) error {
	var watcher *fsnotify.Watcher
	var err error

	if watcher, err = fsnotify.NewWatcher(); err != nil {
		return fmt.Errorf("cannot setup watcher: %w", err)
	}
	defer watcher.Close() // nolint: errcheck

	if s.binSelf, err = self(); err != nil {
		return err
	}

	// Watch the directory, because a recompile renames the existing
	// file (rather than rewriting it), so we won't get events for that.
	var dirs []string

	for i := range additional {
		path, err := filepath.Abs(additional[i].path)
		if err != nil {
			return fmt.Errorf("cannot get absolute path to %q: %w",
				additional[i].path, err)
		}

		s, err := os.Stat(path)
		if err != nil {
			return errors.Wrap(err, "os.Stat")
		}
		if !s.IsDir() {
			return fmt.Errorf("not a directory: %q; can only watch directories",
				additional[i].path)
		}

		additional[i].path = path
		dirs = append(dirs, path)
	}

	for _, d := range dirs {
		if err := watcher.Add(d); err != nil {
			return fmt.Errorf("cannot add %q to watcher: %w", d, err)
		}
	}

	for {
		select {
		case err := <-watcher.Errors:
			// Standard logger doesn't have anything other than Print,
			// Panic, and Fatal :-/ Printf() is probably best.
			s.log.Infof("reload error: %v", err)
		case event := <-watcher.Events:
			// Ensure that we use the correct events, as they are not uniform across
			// platforms. See https://github.com/fsnotify/fsnotify/issues/74

			if !s.started || s.conf == nil {
				continue
			}

			ext := path.Ext(event.Name)
			if ext != ".json" && ext != ".toml" && ext != ".yaml" && ext != ".yml" {
				continue
			}

			if s.conf.Serv.Production {
				continue
			}

			if event.Op != fsnotify.Create && event.Op != fsnotify.Write {
				continue
			}

			// Wait for writes to finish.
			s.log.Infof("reloading, config file changed: %s", event.Name)
			time.Sleep(500 * time.Millisecond)
			reExec(s)()

			/*
				for _, a := range additional {
					if strings.HasPrefix(event.Name, a.path) {
						time.Sleep(500 * time.Millisecond)
						a.cb()
					}
				}
			*/
		}
	}
}

// Exec replaces the current process with a new copy of itself.
func reExec(s *Service) func() {
	return func() {
		argv := append([]string{s.binSelf}, os.Args[1:]...)
		if err := syscall.Exec(s.binSelf, argv, os.Environ()); err != nil {
			s.log.Fatalf("cannot restart: %s", err)
		}
	}
}

// Get location to executable.
func self() (string, error) {
	bin := os.Args[0]

	if filepath.IsAbs(bin) {
		return bin, nil
	}
	var err error
	if bin, err = os.Executable(); err != nil {
		return "", fmt.Errorf("cannot get path to binary %q (launch with absolute path): %w",
			os.Args[0], err)
	}
	return bin, nil
}
