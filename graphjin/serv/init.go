package serv

import (
	// "crypto/sha256"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

// initLogLevel initializes the log level
func initLogLevel(s *graphjinService) {
	switch s.conf.LogLevel {
	case "debug":
		s.logLevel = logLevelDebug
	case "error":
		s.logLevel = logLevelError
	case "warn":
		s.logLevel = logLevelWarn
	case "info":
		s.logLevel = logLevelInfo
	default:
		s.logLevel = logLevelNone
	}
}

// validateConf validates the configuration
func validateConf(s *graphjinService) error {
	var anonFound bool

	for _, r := range s.conf.Roles {
		if r.Name == "anon" {
			anonFound = true
		}
	}

	if !anonFound && s.conf.DefaultBlock {
		s.log.Warn("unauthenticated requests will be blocked. no role 'anon' defined")
		s.conf.AuthFailBlock = false
	}

	// Fail closed (security, audit F2): in agentic mode the MCP/agent surface is
	// exposed to agents and authorization rests entirely on the caller's role.
	// auth.development=true swaps in the header-trust SimpleHandler, which lets
	// any client set X-User-Role / X-User-ID / X-Account-ID unverified — trivial
	// role and account spoofing. Refuse to start rather than only warn.
	if s.conf.Auth.Development && effectiveMode(s.conf) == modeAgentic {
		return fmt.Errorf("security: auth.development=true is not allowed in agentic mode (it trusts X-User-Role/X-User-ID headers without verification); use auth.type: jwt, or auth.type: none behind a trusted proxy or network boundary")
	}

	// prod hard-gate (security model): prod is the only pre-agentic compatibility
	// mode and never mounts the agentic surface. The serv predicates
	// (agenticSurfaceEnabled) already gate the catalog/MCP/agent/workflow/
	// control-plane surfaces off in prod; here we force-disable the core-owned
	// artifacts flag (so nothing is persisted to the database even if it was set)
	// and warn loudly for each agentic subsystem a prod config tried to enable, so
	// the override is visible rather than silent.
	if effectiveMode(s.conf) == modeProd {
		if s.conf.Core.Artifacts.Enabled {
			s.log.Warn("prod mode: artifacts persistence is disabled (agentic-only); saved queries, fragments, workflows, and catalog annotations are not written to the database. Use agentic mode to enable it.")
			s.conf.Core.Artifacts.Enabled = false
		}
		if s.conf.Agent.Enabled {
			s.log.Warn("prod mode: the GraphJin agent and MCP server are disabled (agentic-only). Use agentic mode to expose the agent endpoint and MCP tools.")
		}
	}

	return nil
}

// initFS initializes the file system
func (s *graphjinService) initFS() error {
	basePath, err := s.basePath()
	if err != nil {
		return err
	}

	err = OptionSetFS(core.NewOsFS(basePath))(s)
	if err != nil {
		return err
	}
	return nil
}

// initConfig initializes the configuration
func (s *graphjinService) initConfig() error {
	c := s.conf
	c.dirty = true
	if err := normalizeConfigMode(c); err != nil {
		return err
	}
	if err := normalizeDiscoveryAndSemanticConfig(c); err != nil {
		return err
	}

	if err := validateServiceIsSourcesUsedConfig(c); err != nil {
		return err
	}
	if err := validateMCPOAuthConfig(c); err != nil {
		return err
	}
	applySourceCapabilitySourceDefaults(c)
	if err := normalizeServiceSources(c); err != nil {
		return err
	}

	// copy over db_type from database.type
	if c.DBType == "" {
		c.DBType = c.DB.Type
	}

	// Validate database type early. CodeSQL is a service-level logical type;
	// it is translated to SQLite before core initialization.
	if err := validateServiceDBType(c.DBType); err != nil {
		return err
	}

	// if c.HotDeploy {
	// 	if c.AdminSecretKey != "" {
	// 		s.asec = sha256.Sum256([]byte(s.conf.AdminSecretKey))
	// 	} else {
	// 		return fmt.Errorf("please set an admin_secret_key")
	// 	}
	// }

	if c.Auth.Type == "" || c.Auth.Type == "none" {
		c.DefaultBlock = false
	}

	hp := strings.SplitN(s.conf.HostPort, ":", 2)

	if len(hp) == 2 {
		if s.conf.Host != "" {
			hp[0] = s.conf.Host
		}

		if s.conf.Port != "" {
			hp[1] = s.conf.Port
		}

		s.conf.hostPort = fmt.Sprintf("%s:%s", hp[0], hp[1])
	}

	if s.conf.hostPort == "" {
		s.conf.hostPort = defaultHP
	}

	return nil
}

// ErrGraphJinNotInitialized is returned when GraphJin core is not initialized
var ErrGraphJinNotInitialized = errors.New("GraphJin not initialized - no database configured")

// checkGraphJinInitialized returns an error if GraphJin core is not initialized
func (s *graphjinService) checkGraphJinInitialized() error {
	if s.gj == nil {
		return ErrGraphJinNotInitialized
	}
	return nil
}

// isDatabaseConfigured checks if a database connection is configured
func (s *graphjinService) isDatabaseConfigured() bool {
	// Check if connection string is provided
	if s.conf.DB.ConnString != "" {
		return true
	}
	if s.conf.DB.Path != "" {
		return true
	}
	// Check if host and dbname are provided (minimal required fields for auto-connect)
	if s.conf.DB.Host != "" && s.conf.DB.DBName != "" {
		return true
	}
	// Check if multi-database configs exist with actual connection info
	for _, dbConf := range s.conf.Core.Databases {
		if dbConf.ConnString != "" || dbConf.Host != "" || dbConf.Path != "" {
			return true
		}
	}
	return false
}

// initDB initializes database connections for all entries in conf.Core.Databases.
func (s *graphjinService) initDB() error {
	runtimeCore := cloneCoreConfig(s.conf.Core)
	if err := s.hydrateCoreConfigSecrets(&runtimeCore); err != nil {
		return err
	}
	if err := s.hydrateLegacyDatabaseSecrets(&s.conf.DB); err != nil {
		return err
	}
	s.runtimeCore = &runtimeCore

	if len(s.dbs) > 0 && !s.hasDatabaseConfigs() {
		return nil
	}

	// In dev mode, allow starting without a database configured
	if !s.conf.Serv.Production && !s.isDatabaseConfigured() {
		s.log.Warn("No databases configured. Use MCP to add a database configuration.")
		return nil
	}

	// In sources used, absence of SQL/CodeSQL connection details means there is
	// no legacy database to fall back to. Virtual/system sources get a small
	// host database in normalStart when needed.
	if s.conf.Core.IsSourcesUsed() && !s.hasDatabaseConfigs() {
		return nil
	}

	// If there are entries in conf.Core.Databases with connection info, use them.
	// Otherwise fall back to the legacy single-DB path via conf.DB.
	if s.hasDatabaseConfigs() {
		return s.initAllDBs()
	}

	// Legacy single-DB path: create one connection from conf.DB
	return s.initLegacyDB()
}

// hasDatabaseConfigs returns true if any entry in conf.Core.Databases
// has enough info to create a connection.
func (s *graphjinService) hasDatabaseConfigs() bool {
	for _, dbConf := range s.conf.Core.Databases {
		if dbConf.ConnString != "" || dbConf.Host != "" || dbConf.Path != "" {
			return true
		}
	}
	return false
}

// initAllDBs creates connections for every entry in conf.Core.Databases.
func (s *graphjinService) initAllDBs() error {
	dbNames := make([]string, 0, len(s.conf.Core.Databases))
	for name := range s.conf.Core.Databases {
		dbNames = append(dbNames, name)
	}
	sort.Strings(dbNames)
	for _, name := range dbNames {
		dbConf := s.conf.Core.Databases[name]
		runtimeDBConf := dbConf
		if s.runtimeCore != nil && s.runtimeCore.Databases != nil {
			if hydrated, ok := s.runtimeCore.Databases[name]; ok {
				runtimeDBConf = hydrated
			}
		}
		if _, ok := s.dbs[name]; ok {
			s.recordRuntimeEvent(context.Background(), runtimeEvent{
				Phase:        "database",
				Status:       runtimeStatusReady,
				Severity:     "info",
				Summary:      "Database connection established.",
				NextAction:   "Proceed with schema discovery and catalog-guided queries.",
				DatabaseName: name,
				Details:      map[string]any{"database": name, "database_type": dbConf.Type, "provided": true},
			})
			continue
		}
		db, err := s.newDBFromDatabaseConfigInto(name, runtimeDBConf, s.runtimeCore, s.managedDBs)
		if err != nil {
			s.recordRuntimeEvent(context.Background(), runtimeEvent{
				Phase:        "database",
				Status:       runtimeStatusFailed,
				Severity:     "error",
				Summary:      "Database connection failed.",
				NextAction:   "Fix this database source configuration or choose another active database before application queries.",
				DatabaseName: name,
				ErrorCode:    "database_connect_failed",
				Details:      map[string]any{"database": name, "database_type": dbConf.Type, "error": redactRuntimeStringValue(err.Error())},
			})
			if s.conf.Serv.Production {
				return fmt.Errorf("database %s: %s", name, redactRuntimeStringValue(err.Error()))
			}
			s.log.Warnf("Database '%s' connection failed: %s. Skipping.", name, redactRuntimeStringValue(err.Error()))
			continue
		}
		s.dbs[name] = db
		s.recordRuntimeEvent(context.Background(), runtimeEvent{
			Phase:        "database",
			Status:       runtimeStatusReady,
			Severity:     "info",
			Summary:      "Database connection established.",
			NextAction:   "Proceed with schema discovery and catalog-guided queries.",
			DatabaseName: name,
			Details:      map[string]any{"database": name, "database_type": dbConf.Type},
		})
	}
	// Sync legacy conf.DB from first database for code that still reads it
	if len(s.dbs) > 0 {
		syncRuntimeDBFromDatabases(s.conf, s.runtimeCore)
	}
	return nil
}

// initLegacyDB creates a single connection from the legacy conf.DB fields.
func (s *graphjinService) initLegacyDB() error {
	if isCodeSQLType(s.conf.DB.Type) || isCodeSQLType(s.conf.DBType) {
		dbConf := core.DatabaseConfig{
			Type:            dbTypeCodeSQL,
			Path:            s.conf.DB.Path,
			PingTimeout:     s.conf.DB.PingTimeout,
			PoolSize:        s.conf.DB.PoolSize,
			MaxConnections:  s.conf.DB.MaxConnections,
			MaxConnIdleTime: s.conf.DB.MaxConnIdleTime,
			MaxConnLifeTime: s.conf.DB.MaxConnLifeTime,
		}
		db, err := s.newDBFromDatabaseConfigInto(core.DefaultDBName, dbConf, s.runtimeCore, s.managedDBs)
		if err != nil {
			s.recordRuntimeEvent(context.Background(), runtimeEvent{
				Phase:        "database",
				Status:       runtimeStatusFailed,
				Severity:     "error",
				Summary:      "CodeSQL database initialization failed.",
				NextAction:   "Inspect CodeSQL source configuration before application queries.",
				DatabaseName: core.DefaultDBName,
				ErrorCode:    "database_connect_failed",
				Details:      map[string]any{"database": core.DefaultDBName, "database_type": dbTypeCodeSQL, "error": redactRuntimeStringValue(err.Error())},
			})
			if s.conf.Serv.Production {
				return fmt.Errorf("%s", redactRuntimeStringValue(err.Error()))
			}
			s.log.Warnf("CodeSQL database initialization failed: %s. Server starting without database — use MCP to configure.", redactRuntimeStringValue(err.Error()))
			return nil
		}
		s.dbs[core.DefaultDBName] = db
		s.recordRuntimeEvent(context.Background(), runtimeEvent{
			Phase:        "database",
			Status:       runtimeStatusReady,
			Severity:     "info",
			Summary:      "CodeSQL database initialized.",
			NextAction:   "Proceed with schema discovery and catalog-guided queries.",
			DatabaseName: core.DefaultDBName,
			Details:      map[string]any{"database": core.DefaultDBName, "database_type": dbTypeCodeSQL},
		})
		if s.runtimeCore.Databases != nil {
			runtime := s.runtimeCore.Databases[core.DefaultDBName]
			s.conf.DB.Type = runtime.Type
			s.conf.DB.Path = runtime.Path
			s.conf.DB.ConnString = runtime.ConnString
			s.conf.DBType = runtime.Type
		}
		return nil
	}

	var db *sql.DB
	var err error

	if s.conf.Serv.Production {
		db, err = newDB(s.conf, true, true, s.log, s.fs)
		if err != nil {
			s.recordRuntimeEvent(context.Background(), runtimeEvent{
				Phase:        "database",
				Status:       runtimeStatusFailed,
				Severity:     "error",
				Summary:      "Database connection failed.",
				NextAction:   "Fix database configuration before starting GraphJin in production.",
				DatabaseName: core.DefaultDBName,
				ErrorCode:    "database_connect_failed",
				Details:      map[string]any{"database": core.DefaultDBName, "database_type": s.conf.DB.Type, "error": redactRuntimeStringValue(err.Error())},
			})
			return fmt.Errorf("%s", redactRuntimeStringValue(err.Error()))
		}
	} else {
		db, err = newDBOnce(s.conf, true, true, s.log, s.fs)
		if err != nil {
			s.recordRuntimeEvent(context.Background(), runtimeEvent{
				Phase:        "database",
				Status:       runtimeStatusFailed,
				Severity:     "error",
				Summary:      "Database connection failed.",
				NextAction:   "Use MCP/config tools to fix the database configuration before application queries.",
				DatabaseName: core.DefaultDBName,
				ErrorCode:    "database_connect_failed",
				Details:      map[string]any{"database": core.DefaultDBName, "database_type": s.conf.DB.Type, "error": redactRuntimeStringValue(err.Error())},
			})
			s.log.Warnf("Database connection failed: %s. Server starting without database — use MCP to configure.", redactRuntimeStringValue(err.Error()))
			return nil
		}
	}

	// Store under the first Databases key (sorted for determinism)
	name := core.DefaultDBName
	if len(s.conf.Core.Databases) > 0 {
		names := make([]string, 0, len(s.conf.Core.Databases))
		for n := range s.conf.Core.Databases {
			names = append(names, n)
		}
		sort.Strings(names)
		name = names[0]
	}
	s.dbs[name] = db
	s.recordRuntimeEvent(context.Background(), runtimeEvent{
		Phase:        "database",
		Status:       runtimeStatusReady,
		Severity:     "info",
		Summary:      "Database connection established.",
		NextAction:   "Proceed with schema discovery and catalog-guided queries.",
		DatabaseName: name,
		Details:      map[string]any{"database": name, "database_type": s.conf.DB.Type},
	})
	return nil
}

// newDBFromDatabaseConfig creates a *sql.DB from a core.DatabaseConfig.
func (s *graphjinService) newDBFromDatabaseConfig(name string, dbConf core.DatabaseConfig) (*sql.DB, error) {
	return s.newDBFromDatabaseConfigInto(name, dbConf, &s.conf.Core, s.managedDBs)
}

func (s *graphjinService) newDBFromDatabaseConfigInto(name string, dbConf core.DatabaseConfig, runtimeCore *core.Config, managed map[string]managedDB) (*sql.DB, error) {
	dbType := strings.ToLower(dbConf.Type)
	if dbType == "" {
		dbType = "postgres"
	}

	if isCodeSQLType(dbType) {
		db, runtime, handle, stats, err := s.openCodeSQLDatabase(name, dbConf)
		if err != nil {
			return nil, err
		}
		readOnly, watch := s.codeSQLSourcePolicy(name, dbConf)
		if runtimeCore != nil {
			if runtimeCore.Databases == nil {
				runtimeCore.Databases = make(map[string]core.DatabaseConfig)
			}
			runtimeCore.Databases[name] = runtime
			if runtimeCore.DBType == "" || isCodeSQLType(runtimeCore.DBType) {
				runtimeCore.DBType = runtime.Type
			}
		}
		if managed != nil {
			managed[name] = managedDB{handle: handle, watch: watch, readOnly: readOnly}
		}
		if stats != nil {
			s.log.Infof("codesql database %q indexed: added=%d changed=%d deleted=%d skipped=%d cache=%s",
				name, stats.FilesAdded, stats.FilesChanged, stats.FilesDeleted, stats.FilesSkipped, handle.CachePath)
		}
		return db, nil
	}

	// Configured databases must honor the per-database ping_timeout. Fall back
	// to 30s — generous enough for cold cloud-database TLS handshakes without
	// hanging forever on a truly unreachable host.
	pingTimeout := dbConf.PingTimeout
	if pingTimeout <= 0 {
		pingTimeout = 30 * time.Second
	}

	// A raw connection string already defines its database. Preserve it exactly
	// as the previous multi-database path did; openDB only applies DBName when
	// the connection is assembled from individual fields.
	openDB := dbConf.ConnString == ""
	if openDB && dbConf.DBName == "" && dbType != "sqlite" {
		dbConf.DBName = name
	}
	conf := configuredDatabaseServiceConfig(dbType, dbConf)
	fs := s.fs
	if fs == nil {
		fs = core.NewOsFS("")
	}
	driver, err := initDBDriver(conf, openDB, false, fs)
	if err != nil {
		return nil, err
	}
	return connectConfiguredDatabase(driver, conf.DB, pingTimeout)
}

// configuredDatabaseServiceConfig converts the multi-database core shape into
// the service driver shape. Keeping this path on initDBDriver ensures TLS,
// database-specific encryption, key-pair auth, and connector-backed drivers
// behave exactly like the legacy single-database configuration.
func configuredDatabaseServiceConfig(dbType string, dbConf core.DatabaseConfig) *Config {
	poolSize := dbConf.PoolSize
	if poolSize == 0 {
		poolSize = dbConf.MaxIdleConns
	}
	maxConnections := dbConf.MaxConnections
	if maxConnections == 0 {
		maxConnections = dbConf.MaxOpenConns
	}
	return &Config{
		Core: core.Config{DBType: dbType},
		Serv: Serv{DB: Database{
			Type:                   dbType,
			ConnString:             dbConf.ConnString,
			Host:                   dbConf.Host,
			Port:                   uint16(dbConf.Port),
			DBName:                 dbConf.DBName,
			User:                   dbConf.User,
			Password:               dbConf.Password,
			Schema:                 dbConf.Schema,
			Path:                   dbConf.Path,
			PoolSize:               poolSize,
			MaxConnections:         maxConnections,
			MaxConnIdleTime:        dbConf.MaxConnIdleTime,
			MaxConnLifeTime:        dbConf.MaxConnLifeTime,
			PingTimeout:            dbConf.PingTimeout,
			EnableTLS:              dbConf.EnableTLS,
			ServerName:             dbConf.ServerName,
			ServerCert:             dbConf.ServerCert,
			ClientCert:             dbConf.ClientCert,
			ClientKey:              dbConf.ClientKey,
			Encrypt:                dbConf.Encrypt,
			TrustServerCertificate: dbConf.TrustServerCertificate,
			PrivateKeyPath:         dbConf.PrivateKeyPath,
			PrivateKeyPEM:          dbConf.PrivateKeyPEM,
			KeyPassphrase:          dbConf.KeyPassphrase,
		}},
	}
}

func connectConfiguredDatabase(driver *dbConf, conf Database, timeout time.Duration) (*sql.DB, error) {
	var (
		db  *sql.DB
		err error
	)
	if driver.connector != nil {
		db = sql.OpenDB(driver.connector)
	} else {
		db, err = sql.Open(driver.driverName, driver.connString)
		if err != nil {
			return nil, err
		}
	}

	db.SetMaxIdleConns(conf.PoolSize)
	db.SetMaxOpenConns(conf.MaxConnections)
	db.SetConnMaxIdleTime(conf.MaxConnIdleTime)
	db.SetConnMaxLifetime(conf.MaxConnLifeTime)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close() //nolint:errcheck
		return nil, err
	}
	return db, nil
}

// driverForType returns the Go SQL driver name for a database type.
func driverForType(dbType string) string {
	switch dbType {
	case "postgres":
		return "pgx"
	case "mysql", "mariadb":
		return "mysql"
	case "mssql":
		return "sqlserver"
	case "oracle":
		return "oracle"
	case "sqlite":
		return "sqlite"
	case "snowflake":
		return "snowflake"
	default:
		return dbType
	}
}

// basePath returns the base path
func (s *graphjinService) basePath() (string, error) {
	if s.conf.ConfigPath == "" {
		if cp, err := os.Getwd(); err == nil {
			return filepath.Join(cp, "config"), nil
		} else {
			return "", err
		}
	}
	return s.conf.ConfigPath, nil
}

// initResponseCache initializes the response cache (Redis or in-memory)
func (s *graphjinService) initResponseCache() error {
	// Caching is enabled by default unless explicitly disabled
	if s.conf.Caching.Disable {
		s.log.Info("Response cache disabled")
		return nil
	}

	if s.conf.Redis.URL != "" {
		// Try to use Redis
		cache, err := NewRedisCache(s.conf.Redis.URL, s.conf.Caching)
		if err != nil {
			s.log.Warnf("Redis unavailable, falling back to in-memory cache: %s", err)
			s.cache, err = NewMemoryCache(s.conf.Caching, defaultMemoryCacheSize)
			if err != nil {
				s.log.Warnf("Failed to initialize memory cache: %s", err)
				return nil
			}
			s.log.Info("Using in-memory response cache (Redis unavailable)")
		} else {
			s.cache = cache
			s.log.Info("Redis response cache enabled")
		}
	} else {
		// No Redis URL - use in-memory cache
		var err error
		s.cache, err = NewMemoryCache(s.conf.Caching, defaultMemoryCacheSize)
		if err != nil {
			s.log.Warnf("Failed to initialize memory cache: %s", err)
			return nil
		}
		s.log.Info("Using in-memory response cache (no Redis URL configured)")
	}

	// Enable cache tracking in qcode compiler (injects __gj_id fields)
	s.conf.CacheTrackingEnabled = true

	return nil
}

// initCursorCache initializes the MCP cursor cache (Redis or in-memory)
// This cache maps short numeric IDs to encrypted cursor strings for LLM-friendly pagination
func (s *graphjinService) initCursorCache() error {
	// Skip if MCP is disabled
	if s.conf.mcpDisabled() {
		return nil
	}

	ttl := time.Duration(s.conf.MCP.CursorCacheTTL) * time.Second
	if ttl == 0 {
		ttl = 30 * time.Minute // Default 30 minutes
	}

	maxEntries := s.conf.MCP.CursorCacheSize
	if maxEntries == 0 {
		maxEntries = 10000 // Default 10k entries
	}

	if s.conf.Redis.URL != "" {
		// Try to use Redis
		cache, err := NewRedisCursorCache(s.conf.Redis.URL, ttl)
		if err != nil {
			s.log.Warnf("Redis unavailable for cursor cache, using in-memory: %s", err)
			s.cursorCache = NewMemoryCursorCache(maxEntries, ttl)
			s.log.Info("MCP cursor cache: in-memory (Redis unavailable)")
		} else {
			s.cursorCache = cache
			s.log.Info("MCP cursor cache: Redis")
		}
	} else {
		s.cursorCache = NewMemoryCursorCache(maxEntries, ttl)
		s.log.Info("MCP cursor cache: in-memory")
	}

	return nil
}
