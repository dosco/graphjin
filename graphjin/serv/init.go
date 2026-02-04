package serv

import (
	// "crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
func validateConf(s *graphjinService) {
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

	// copy over db_type from database.type
	if c.DBType == "" {
		c.DBType = c.DB.Type
	}

	// Validate database type early
	if err := core.ValidateDBType(c.DBType); err != nil {
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

	c.Core.Production = c.Serv.Production
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
	// Check if host and dbname are provided (minimal required fields for auto-connect)
	if s.conf.DB.Host != "" && s.conf.DB.DBName != "" {
		return true
	}
	// Check if multi-database configs exist
	if len(s.conf.Core.Databases) > 0 {
		return true
	}
	return false
}

// initDB initializes the database
func (s *graphjinService) initDB() error {
	var err error

	if s.db != nil {
		return nil
	}

	// In dev mode, allow starting without a database configured
	if !s.conf.Serv.Production && !s.isDatabaseConfigured() {
		s.log.Warn("No databases configured. Use MCP to add a database configuration.")
		return nil
	}

	s.db, err = newDB(s.conf, true, true, s.log, s.fs)
	if err != nil {
		return err
	}
	return nil
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
	if s.conf.MCP.Disable {
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
