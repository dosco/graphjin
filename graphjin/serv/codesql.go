package serv

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/dosco/graphjin/codesql"
	"github.com/dosco/graphjin/core/v3"
)

const dbTypeCodeSQL = "codesql"

type managedDB struct {
	handle *codesql.Managed
}

func isCodeSQLType(dbType string) bool {
	return strings.EqualFold(strings.TrimSpace(dbType), dbTypeCodeSQL)
}

func validateServiceDBType(dbType string) error {
	if isCodeSQLType(dbType) {
		return nil
	}
	return core.ValidateDBType(dbType)
}

func validateServiceMultiDBType(dbType string) error {
	if isCodeSQLType(dbType) {
		return nil
	}
	return core.ValidateMultiDBType(dbType)
}

func (s *graphjinService) openCodeSQLDatabase(name string, dbConf core.DatabaseConfig) (*sql.DB, core.DatabaseConfig, *codesql.Managed, *codesql.Stats, error) {
	root := strings.TrimSpace(dbConf.Path)
	if root == "" {
		return nil, core.DatabaseConfig{}, nil, nil, fmt.Errorf("codesql database %q requires path", name)
	}

	base, err := s.basePath()
	if err != nil {
		return nil, core.DatabaseConfig{}, nil, nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	managed, stats, err := codesql.OpenManaged(ctx, codesql.Options{
		Name:     name,
		Root:     root,
		CacheDir: filepath.Join(base, dbTypeCodeSQL),
		Watch:    true,
	})
	if err != nil {
		return nil, core.DatabaseConfig{}, nil, nil, err
	}

	analyticsMode := true
	runtime := dbConf
	runtime.Type = "sqlite"
	runtime.ConnString = managed.CachePath
	runtime.Path = managed.CachePath
	runtime.ReadOnly = true
	runtime.AnalyticsMode = &analyticsMode

	return managed.DB, runtime, managed, stats, nil
}

func (s *graphjinService) closeManagedDBs(keep map[string]*sql.DB) map[string]struct{} {
	closed := make(map[string]struct{})
	for name, managed := range s.managedDBs {
		if managed.handle == nil {
			delete(s.managedDBs, name)
			continue
		}
		if keep != nil && keep[name] == managed.handle.DB {
			continue
		}
		managed.handle.Close() //nolint:errcheck
		closed[name] = struct{}{}
		delete(s.managedDBs, name)
		s.log.Infof("closed managed codesql database: %s", name)
	}
	return closed
}
