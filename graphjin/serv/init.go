package serv

import (
	"fmt"

	"go.uber.org/zap"

	// postgres drivers

	// mysql drivers
	_ "github.com/go-sql-driver/mysql"
)

func initLogLevel(s *Service) {
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

func validateConf(s *Service) {
	var anonFound bool

	for _, r := range s.conf.Roles {
		if r.Name == "anon" {
			anonFound = true
		}
	}

	if !anonFound && s.conf.DefaultBlock {
		s.log.Warn("Unauthenticated requests will be blocked. no role 'anon' defined")
		s.conf.AuthFailBlock = false
	}
}

func initConfig(c *Config, log *zap.SugaredLogger) error {
	// copy over db_type from database.type
	if c.Core.DBType == "" {
		c.Core.DBType = c.DB.Type
	}

	if c.Serv.Production {
		c.Auth.CredsInHeader = false
		c.Auth.SubsCredsInVars = false
	}

	// Auths: validate and sanitize
	am := make(map[string]struct{})

	for i := 0; i < len(c.Auths); i++ {
		a := &c.Auths[i]

		if c.Serv.Production {
			a.CredsInHeader = false
			a.SubsCredsInVars = false
		}

		if _, ok := am[a.Name]; ok {
			return fmt.Errorf("Duplicate auth found: %s", a.Name)
		}
		am[a.Name] = struct{}{}
	}

	// Actions: validate and sanitize
	axm := make(map[string]struct{})

	for i := 0; i < len(c.Actions); i++ {
		a := &c.Actions[i]

		if _, ok := axm[a.Name]; ok {
			return fmt.Errorf("Duplicate action found: %s", a.Name)
		}

		if _, ok := am[a.AuthName]; !ok {
			return fmt.Errorf("Invalid auth name: %s, For auth: %s", a.AuthName, a.Name)
		}
		axm[a.Name] = struct{}{}
	}

	if c.Auth.Type == "" || c.Auth.Type == "none" {
		c.DefaultBlock = false
	}

	c.Core.Production = c.Serv.Production
	return nil
}
