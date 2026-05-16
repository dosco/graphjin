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
	"github.com/dosco/graphjin/core/v3/sourcecap"
)

const dbTypeCodeSQL = "codesql"

type managedDB struct {
	handle   *codesql.Managed
	watch    bool
	readOnly bool
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

	inferDBRefs := true
	if dbConf.InferDBRefs != nil {
		inferDBRefs = *dbConf.InferDBRefs
	}
	_, watch := s.codeSQLSourcePolicy(name, dbConf)
	managed, stats, err := codesql.OpenManaged(ctx, codesql.Options{
		Name:        name,
		Root:        root,
		CacheDir:    filepath.Join(base, dbTypeCodeSQL),
		Watch:       watch,
		InferDBRefs: inferDBRefs,
	})
	if err != nil {
		return nil, core.DatabaseConfig{}, nil, nil, err
	}
	s.bindCodeSQLCacheHook(name, managed)

	analyticsMode := true
	runtime := dbConf
	runtime.ManagedType = dbTypeCodeSQL
	runtime.Type = "sqlite"
	runtime.ConnString = managed.CachePath
	runtime.Path = managed.CachePath
	runtime.ReadOnly = true
	runtime.AnalyticsMode = &analyticsMode

	return managed.DB, runtime, managed, stats, nil
}

func (s *graphjinService) codeSQLSourcePolicy(name string, dbConf core.DatabaseConfig) (readOnly, watch bool) {
	readOnly = dbConf.ReadOnly
	watch = !dbConf.ReadOnly
	if s == nil || s.conf == nil || !s.conf.Core.IsSourcesUsed() {
		return readOnly, watch
	}
	source, ok := s.conf.Core.SourceByName(name)
	if !ok || source.CanonicalKind() != sourcecap.KindCode {
		return readOnly, watch
	}
	if source.ReadOnly {
		return true, false
	}
	writeAllowed, _ := s.conf.sourceCapabilityForSource(source, sourcecap.KeyCodeWrite)
	watchAllowed, _ := s.conf.sourceCapabilityForSource(source, sourcecap.KeyCodeWatch)
	return !writeAllowed, watchAllowed
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

func (s *graphjinService) bindCodeSQLCacheHooks() {
	for name, managed := range s.managedDBs {
		if managed.handle == nil {
			continue
		}
		s.bindCodeSQLCacheHook(name, managed.handle)
	}
}

func (s *graphjinService) bindCodeSQLCacheHook(name string, managed *codesql.Managed) {
	if s.cache == nil || managed == nil {
		return
	}
	tables := codesql.ManagedCacheTables()
	managed.SetSourceChangeHook(func() {
		if err := s.cache.InvalidateRows(context.Background(), core.CodeSQLTableRefs(name, tables)); err != nil && s.log != nil {
			s.log.Warnf("codesql cache invalidation failed for %s: %s", name, err)
		}
	})
}
