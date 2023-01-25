package serv

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/kardianos/osext"
	"github.com/pkg/errors"
)

func startConfigWatcher(s1 *Service) error {
	var watcher *fsnotify.Watcher
	var err error

	binary, err := osext.Executable()
	if err != nil {
		return err
	}

	var paths []string
	if s1.cpath == "" || s1.cpath == "./" {
		paths = []string{"./config"}
	} else {
		paths = []string{s1.cpath}
	}

	if watcher, err = fsnotify.NewWatcher(); err != nil {
		return fmt.Errorf("cannot setup watcher: %w", err)
	}
	defer watcher.Close() // nolint:errcheck

	var dirs []string
	for _, p := range paths {
		path, err := filepath.Abs(p)
		if err != nil {
			return fmt.Errorf("cannot get absolute path to %q: %w", p, err)
		}

		s, err := os.Stat(path)
		if err != nil {
			return errors.Wrap(err, "os.Stat")
		}
		if !s.IsDir() {
			return fmt.Errorf("not a directory: %q; can only watch directories", p)
		}

		dirs = append(dirs, path)
	}

	for _, d := range dirs {
		if err := watcher.Add(d); err != nil {
			return fmt.Errorf("cannot add %q to watcher: %w", d, err)
		}
	}

	for {
		s := s1.Load().(*service)

		select {
		case err := <-watcher.Errors:
			// Standard logger doesn't have anything other than Print,
			// Panic, and Fatal :-/ Printf() is probably best.
			s.log.Infof("reload error: %v", err)
		case event := <-watcher.Events:
			// Ensure that we use the correct events, as they are not uniform across
			// platforms. See https://github.com/fsnotify/fsnotify/issues/74

			if s.conf == nil {
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

			// Check if new config is valid
			cf := s.conf.RelPath(GetConfigName())
			conf, err := readInConfig(cf, nil)
			if err != nil {
				s.log.Error(err)
				continue
			}

			// Check if new config works fine
			if _, err := NewGraphJinService(conf, s1.opt...); err != nil {
				s.log.Error(err)
				continue
			}

			// Wait for writes to finish.
			s.log.Infof("reloading, config file changed: %s", event.Name)
			time.Sleep(500 * time.Millisecond)

			if err := syscall.Exec(binary, os.Args, os.Environ()); err != nil {
				s.log.Fatal(err)
			}
		}
	}
}
