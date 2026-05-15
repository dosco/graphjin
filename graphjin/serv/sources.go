package serv

import (
	"fmt"
	"strings"
)

func (c *Config) legacyMCPToolsEnabled() bool {
	return c.legacyDiscoveryEnabled()
}

func (c *Config) legacyDiscoveryEnabled() bool {
	if c == nil {
		return false
	}
	return !c.Core.SourceMode() || c.MCP.LegacyDiscovery
}

func (c *Config) catalogToolsEnabled() bool {
	if c == nil {
		return false
	}
	return c.Core.CatalogEnabled()
}

func (c *Config) graphjinControlPlaneEnabled() bool {
	if c == nil {
		return false
	}
	source, ok := c.Core.GraphJinSource()
	return ok && sourceBool(source.ControlPlane, true)
}

func (c *Config) workflowsSourceEnabled() bool {
	if c == nil {
		return false
	}
	_, ok := c.Core.WorkflowsSource()
	return ok
}

func (c *Config) graphjinSourceReadOnly() bool {
	if c == nil {
		return false
	}
	source, ok := c.Core.GraphJinSource()
	return ok && source.ReadOnly
}

func (c *Config) workflowsSourceReadOnly() bool {
	if c == nil {
		return false
	}
	source, ok := c.Core.WorkflowsSource()
	return ok && source.ReadOnly
}

func (c *Config) needsSystemHostDB() bool {
	if c == nil || !c.Core.SourceMode() {
		return false
	}
	for _, source := range c.Core.Sources {
		switch strings.ToLower(strings.TrimSpace(source.Kind)) {
		case "filesystem", "openapi", "graphjin", "workflows":
			return true
		}
	}
	return false
}

func validateServiceSourceModeConfig(conf *Config) error {
	if conf == nil {
		return nil
	}
	if !conf.Core.SourceMode() {
		if isCodeSQLType(conf.DB.Type) || isCodeSQLType(conf.DBType) {
			return fmt.Errorf("database.type codesql is legacy config; move CodeSQL providers to sources with kind: codesql")
		}
		return conf.Core.ValidateSourceMode()
	}
	if (conf.viper != nil && conf.viper.InConfig("database")) || serviceDatabaseConfigured(conf.DB) {
		return fmt.Errorf("database is legacy database-only config; move SQL providers to sources")
	}
	return conf.Core.ValidateSourceMode()
}

func serviceDatabaseConfigured(db Database) bool {
	return db.ConnString != "" ||
		db.Type != "" ||
		db.Host != "" ||
		db.Port != 0 ||
		db.DBName != "" ||
		db.User != "" ||
		db.Password != "" ||
		db.Schema != "" ||
		db.Path != "" ||
		db.MaxConnections != 0 ||
		db.MaxConnIdleTime != 0 ||
		db.MaxConnLifeTime != 0 ||
		db.PingTimeout != 0 ||
		db.EnableTLS ||
		db.ServerName != "" ||
		db.ServerCert != "" ||
		db.ClientCert != "" ||
		db.ClientKey != "" ||
		db.Encrypt != nil ||
		db.TrustServerCertificate != nil ||
		db.PrivateKeyPath != "" ||
		db.PrivateKeyPEM != "" ||
		db.KeyPassphrase != ""
}

func normalizeServiceSources(conf *Config) error {
	if conf == nil {
		return nil
	}
	return conf.Core.NormalizeSources()
}

func sourceBool(v *bool, def bool) bool {
	if v == nil {
		return def
	}
	return *v
}
