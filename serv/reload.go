// Package reload offers lightweight automatic reloading of running processes.
//
// After initialisation with reload.Do() any changes to the binary will
// restart the process.
//
// Example:
//
//    go func() {
//        err := reload.Do(log.Printf)
//        if err != nil {
//            panic(err)
//        }
//    }()
//
// A list of additional directories to watch can be added:
//
//    go func() {
//        err := reload.Do(log.Printf, reload.Dir("tpl", reloadTpl)
//        if err != nil {
//            panic(err)
//        }
//    }()
//
// Note that this package won't prevent race conditions (e.g. when assigning to
// a global templates variable). You'll need to use sync.RWMutex yourself.
package serv // import "github.com/teamwork/reload"

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
// Use reload.ReExec() to restart the process.
func Dir(path string, cb func()) dir { return dir{path, cb} } // nolint: golint

// Do reload the current process when its binary changes.
//
// The log function is used to display an informational startup message and
// errors. It works well with e.g. the standard log package or Logrus.
//
// The error return will only return initialisation errors. Once initialized it
// will use the log function to print errors, rather than return.
func Do(log func(string, ...interface{}), additional ...dir) error {
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
				// Ensure that we use the correct events, as they are not uniform accross
				// platforms. See https://github.com/fsnotify/fsnotify/issues/74
				var trigger bool
				switch runtime.GOOS {
				case "darwin", "freebsd", "openbsd", "netbsd", "dragonfly":
					trigger = event.Op&fsnotify.Create == fsnotify.Create
				case "linux":
					trigger = event.Op&fsnotify.Write == fsnotify.Write
				default:
					trigger = event.Op&fsnotify.Create == fsnotify.Create
					log("reload: untested GOOS %q; this package may not work correctly", runtime.GOOS)
				}

				if !trigger {
					continue
				}

				if event.Name == binSelf {
					// Wait for writes to finish.
					time.Sleep(100 * time.Millisecond)
					ReExec()
				}

				for _, a := range additional {
					if strings.HasPrefix(event.Name, a.path) {
						time.Sleep(100 * time.Millisecond)
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

	add := ""
	if len(additional) > 0 {
		reldirs := make([]string, len(dirs)-1)
		for i := range dirs[1:] {
			reldirs[i] = relpath(dirs[i+1])
		}
		add = fmt.Sprintf(" (additional dirs: %s)", strings.Join(reldirs, ", "))
	}
	log("restarting %q when it changes%s", relpath(binSelf), add)
	<-done
	return nil
}

// Exec replaces the current process with a new copy of itself.
func ReExec() {
	err := syscall.Exec(binSelf, append([]string{binSelf}, os.Args[1:]...), os.Environ())
	if err != nil {
		logger.Fatal().Err(err).Msg("cannot restart")
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

// Get path relative to cwd
func relpath(p string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return p
	}

	if strings.HasPrefix(p, cwd) {
		return "./" + strings.TrimLeft(p[len(cwd):], "/")
	}

	return p
}
