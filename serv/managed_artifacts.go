package serv

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dosco/graphjin/core/v3"
)

const managedArtifactRelativePath = ".graphjin/artifacts.sqlite3"

func (s *graphjinService) initManagedArtifactStore() error {
	if s == nil || s.conf == nil || !s.conf.managedArtifactStore || !s.conf.Core.Artifacts.Enabled {
		return nil
	}
	name := allocateRuntimeDatabaseName(internalArtifactDatabaseBase, &s.conf.Core, s.runtimeCore, s.dbs)

	base, err := s.basePath()
	if err != nil {
		return fmt.Errorf("managed artifact store: resolve config path: %w", err)
	}
	path := filepath.Join(base, filepath.FromSlash(managedArtifactRelativePath))
	if err := prepareManagedArtifactPath(path); err != nil {
		return fmt.Errorf("managed artifact store %s: %w", path, err)
	}

	ctx := context.Background()
	db, err := openManagedArtifactSQLite(ctx, path)
	if err != nil {
		return fmt.Errorf("managed artifact store %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		db.Close() //nolint:errcheck
		return fmt.Errorf("managed artifact store %s: set permissions: %w", path, err)
	}

	if s.runtimeCore == nil {
		runtime := cloneCoreConfig(s.conf.Core)
		s.runtimeCore = &runtime
	}
	if s.runtimeCore.Databases == nil {
		s.runtimeCore.Databases = make(map[string]core.DatabaseConfig)
	}
	s.runtimeCore.Databases[name] = core.DatabaseConfig{
		Type:         "sqlite",
		Path:         path,
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	}
	s.runtimeCore.Artifacts.Source = name
	s.dbs[name] = db
	s.managedArtifactDB = name
	s.log.Infof("Managed artifact store ready: %s", path)
	return nil
}

func isInternalArtifactDatabase(conf *core.Config, name string) bool {
	if conf == nil || !conf.Artifacts.Enabled || !strings.EqualFold(strings.TrimSpace(conf.Artifacts.Source), strings.TrimSpace(name)) {
		return false
	}
	_, public := conf.SourceByName(name)
	return !public
}

func prepareManagedArtifactPath(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("refusing non-regular SQLite file")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func openManagedArtifactSQLite(ctx context.Context, path string) (*sql.DB, error) {
	// Install the busy handler as part of connection creation. Setting it with a
	// later PRAGMA is too late when a previous service instance is still releasing
	// a WAL lock: the first Ping or journal-mode query can otherwise fail fast.
	dsn := path + "?_pragma=busy_timeout%3d5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close() //nolint:errcheck
		return nil, fmt.Errorf("ping: %w", err)
	}
	if err := configureManagedArtifactSQLite(ctx, db); err != nil {
		db.Close() //nolint:errcheck
		return nil, fmt.Errorf("configure: %w", err)
	}
	return db, nil
}

func configureManagedArtifactSQLite(ctx context.Context, db *sql.DB) error {
	var journalMode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&journalMode); err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(journalMode), "wal") {
		return fmt.Errorf("journal_mode is %q, want WAL", journalMode)
	}
	for _, statement := range []string{
		"PRAGMA synchronous=FULL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
