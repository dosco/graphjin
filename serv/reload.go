package serv

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/pkg/errors"
)

var binSelf string

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
func do(s *Service, log func(string, ...interface{}), additional ...dir) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return errors.Wrap(err, "cannot setup watcher")
	}
	defer watcher.Close() // nolint: errcheck

	binSelf, err = self()
	if err != nil {
		return err
	}

	// Watch the directory, because a recompile renames the existing
	// file (rather than rewriting it), so we won't get events for that.
	dirs := make([]string, len(additional)+1)
	dirs[0] = filepath.Dir(binSelf)

	for i := range additional {
		path, err := filepath.Abs(additional[i].path)
		if err != nil {
			return errors.Wrapf(err, "cannot get absolute path to %q: %v",
				additional[i].path, err)
		}

		s, err := os.Stat(path)
		if err != nil {
			return errors.Wrap(err, "os.Stat")
		}
		if !s.IsDir() {
			return errors.Errorf("not a directory: %q; can only watch directories",
				additional[i].path)
		}

		additional[i].path = path
		dirs[i+1] = path
	}

	done := make(chan bool)
	go func() {
		for {
			select {
			case err := <-watcher.Errors:
				// Standard logger doesn't have anything other than Print,
				// Panic, and Fatal :-/ Printf() is probably best.
				log("reload error: %v", err)
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

				log("reloading, config file changed: %s", event.Name)

				if event.Name == binSelf {
					// Wait for writes to finish.
					log("reloading, config file changed: %s", event.Name)
					time.Sleep(500 * time.Millisecond)
					reExec(s)()
				}

				for _, a := range additional {
					if strings.HasPrefix(event.Name, a.path) {
						time.Sleep(500 * time.Millisecond)
						a.cb()
					}
				}
			}
		}
	}()

	for _, d := range dirs {
		if err := watcher.Add(d); err != nil {
			return errors.Wrapf(err, "cannot add %q to watcher", d)
		}
	}

	<-done
	return nil
}

// Exec replaces the current process with a new copy of itself.
func reExec(s *Service) func() {
	return func() {
		err := syscall.Exec(binSelf, append([]string{binSelf}, os.Args[1:]...), os.Environ())
		if err != nil {
			s.log.Fatalf("cannot restart: %s", err)
		}
	}
}

// Get location to executable.
func self() (string, error) {
	bin := os.Args[0]

	if !filepath.IsAbs(bin) {
		var err error
		bin, err = os.Executable()
		if err != nil {
			return "", errors.Wrapf(err,
				"cannot get path to binary %q (launch with absolute path): %v",
				os.Args[0], err)
		}
	}
	return bin, nil
}
