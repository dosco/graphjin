package serv

import (
	"context"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/sourcecap"
)

type schemaReloadResult struct {
	Mode            string
	Database        string
	Tables          []core.TableInfo
	CatalogRevision string
}

func (s *graphjinService) reloadSchema(ctx context.Context, database string) (schemaReloadResult, error) {
	result := schemaReloadResult{Mode: "full"}
	if s == nil || s.gj == nil {
		return result, fmt.Errorf("GraphJin engine is not initialized")
	}

	database = strings.TrimSpace(database)
	if database == "" {
		if err := s.gj.Reload(); err != nil {
			return result, err
		}
		s.markCatalogChanged("schema reload")
		result.Tables = s.gj.GetTables()
		if snap, err := s.catalogSnapshot(); err == nil {
			result.CatalogRevision = snap.Revision
		}
		return result, nil
	}

	if err := s.validateSourceScopedSchemaReload(database); err != nil {
		return result, err
	}
	result.Mode = "source_scoped"
	result.Database = database
	if err := s.gj.ReloadDatabase(database); err != nil {
		return result, err
	}
	result.Tables = s.gj.GetTablesForDatabase(database)
	if err := s.refreshCatalogAfterSourceSchemaReload(ctx, database); err != nil && s.log != nil {
		s.log.Warnf("catalog refresh after source-scoped schema reload failed: %s", redactRuntimeError(err))
	}
	if snap, err := s.catalogSnapshot(); err == nil {
		result.CatalogRevision = snap.Revision
	}
	return result, nil
}

func (s *graphjinService) validateSourceScopedSchemaReload(database string) error {
	if s == nil || s.gj == nil {
		return fmt.Errorf("GraphJin engine is not initialized")
	}
	found := false
	for _, name := range s.gj.DatabaseNames() {
		if name == database {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("database %q is not configured; omit database for a full reload or use one of: %s",
			database, strings.Join(s.gj.DatabaseNames(), ", "))
	}
	if s.conf == nil || !s.conf.Core.IsSourcesUsed() {
		return nil
	}
	source, ok := s.conf.Core.SourceByName(database)
	if !ok {
		return fmt.Errorf("database %q is not a configured source; omit database for a full reload", database)
	}
	if source.CanonicalKind() != sourcecap.KindDatabase {
		return fmt.Errorf("source %q has kind %q; source-scoped schema reload only supports database sources",
			database, source.CanonicalKind())
	}
	return nil
}

func (s *graphjinService) refreshCatalogAfterSourceSchemaReload(ctx context.Context, database string) error {
	if s == nil {
		return nil
	}
	s.invalidateCatalogCache()
	var err error
	if s.systemNanoDB != nil {
		err = s.refreshSystemNanoDBForSources([]string{database})
	} else {
		s.markCatalogChanged("schema reload")
	}
	s.recordCatalogRefreshRuntimeEvent(ctx, "source_scoped", []string{database}, "schema reload", err)
	return err
}
