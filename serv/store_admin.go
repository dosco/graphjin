package serv

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/dosco/graphjin/serv/v3/internal/util"
)

// StoreStatus describes the control-plane store: where it lives and which of
// its tables exist. `graphjin db store status` prints it.
type StoreStatus struct {
	Source  string       `json:"source"`
	Type    string       `json:"type"`
	Schema  string       `json:"schema"`
	Managed bool         `json:"managed"`
	Path    string       `json:"path,omitempty"`
	Tables  []StoreTable `json:"tables"`
}

// StoreTable reports one control-plane table.
type StoreTable struct {
	Name   string `json:"name"`
	Exists bool   `json:"exists"`
}

// Ready reports whether every store table exists.
func (st StoreStatus) Ready() bool {
	for _, t := range st.Tables {
		if !t.Exists {
			return false
		}
	}
	return len(st.Tables) > 0
}

var errStoreDisabled = errors.New("control-plane store is disabled: set artifacts.enabled in the config")

// InitStore creates and migrates the control-plane store tables (artifacts,
// watches, tasks, ...) without starting the server. It ignores auto_init, so
// production deployments can run with auto_init off and initialise the store
// from the CLI instead.
func InitStore(conf *Config) (StoreStatus, error) {
	s, err := newStoreAdminService(conf)
	if err != nil {
		return StoreStatus{}, err
	}
	defer s.closeStoreAdmin()
	if err := s.ensureStoreTables(); err != nil {
		return StoreStatus{}, fmt.Errorf("control-plane store init: %w", err)
	}
	return s.storeStatus(context.Background())
}

// InspectStore reports the control-plane store without changing it.
func InspectStore(conf *Config) (StoreStatus, error) {
	s, err := newStoreAdminService(conf)
	if err != nil {
		return StoreStatus{}, err
	}
	defer s.closeStoreAdmin()
	return s.storeStatus(context.Background())
}

func newStoreAdminService(conf *Config) (*graphjinService, error) {
	if conf == nil || !conf.Core.Artifacts.Enabled {
		return nil, errStoreDisabled
	}
	if err := normalizeConfigMode(conf); err != nil {
		return nil, err
	}
	zlog := util.NewLogger(conf.ShouldUseJSONLogs())
	s := &graphjinService{
		conf:       conf,
		zlog:       zlog,
		log:        zlog.Sugar(),
		dbs:        make(map[string]*sql.DB),
		managedDBs: make(map[string]managedDB),
	}
	if err := s.initDB(); err != nil {
		return nil, err
	}
	if err := s.initManagedArtifactStore(); err != nil {
		s.closeStoreAdmin()
		return nil, err
	}
	if _, _, _, ok := s.artifactDB(); !ok {
		s.closeStoreAdmin()
		return nil, fmt.Errorf("control-plane store database %q is not configured or could not be opened", conf.Core.EffectiveArtifactsConfig().Source)
	}
	return s, nil
}

func (s *graphjinService) closeStoreAdmin() {
	for _, db := range s.dbs {
		db.Close() //nolint:errcheck
	}
}

// storeTableNames lists the unqualified control-plane tables the current
// config expects.
func (s *graphjinService) storeTableNames() []string {
	names := []string{"artifacts", "revisions"}
	if s.watchesEnabled() {
		names = append(names, "watches", "watch_events")
	}
	if s.tasksEnabled() {
		names = append(names, "tasks", "task_entries")
	}
	if s.threadsEnabled() {
		names = append(names, "threads", "messages", "runs", "run_events")
	}
	return names
}

func (s *graphjinService) storeStatus(ctx context.Context) (StoreStatus, error) {
	db, dbType, _, ok := s.artifactDB()
	if !ok {
		return StoreStatus{}, errStoreDisabled
	}
	cfg := s.conf.Core.EffectiveArtifactsConfig()
	st := StoreStatus{
		Source:  cfg.Source,
		Type:    dbType,
		Schema:  cfg.Schema,
		Managed: s.conf.managedArtifactStore,
	}
	if st.Managed {
		st.Source = s.managedArtifactDB
		if s.runtimeCore != nil {
			st.Path = s.runtimeCore.Databases[s.managedArtifactDB].Path
		}
	}
	for _, name := range s.storeTableNames() {
		table := artifactTableName(dbType, cfg.Schema, name)
		st.Tables = append(st.Tables, StoreTable{Name: name, Exists: storeTableExists(ctx, db, table)})
	}
	// Login state is outside storeTableNames because no role, not even the
	// internal one, may read it through GraphQL.
	if s.authStoreEnabled() {
		table := artifactTableName(dbType, cfg.Schema, authStoreTable)
		st.Tables = append(st.Tables, StoreTable{Name: authStoreTable, Exists: storeTableExists(ctx, db, table)})
	}
	return st, nil
}

// storeTableExists probes a qualified table with a query that reads no rows,
// which works the same on every store dialect.
func storeTableExists(ctx context.Context, db *sql.DB, table string) bool {
	rows, err := db.QueryContext(ctx, "SELECT 1 FROM "+table+" WHERE 1=0")
	if err != nil {
		return false
	}
	rows.Close() //nolint:errcheck
	return true
}
