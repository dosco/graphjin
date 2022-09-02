package serv

import (
	"crypto/sha256"
	"fmt"
	"strings"

	// postgres drivers

	// mysql drivers
	_ "github.com/go-sql-driver/mysql"
	"github.com/spf13/afero"
)

func initLogLevel(s *service) {
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

func validateConf(s *service) {
	var anonFound bool

	for _, r := range s.conf.Core.Roles {
		if r.Name == "anon" {
			anonFound = true
		}
	}

	if !anonFound && s.conf.Core.DefaultBlock {
		s.log.Warn("unauthenticated requests will be blocked. no role 'anon' defined")
		s.conf.AuthFailBlock = false
	}
}

func (s *service) initFS() error {
	if s.fs != nil {
		return nil
	}

	if s.conf.Serv.ConfigPath == "" {
		return nil
	}
	cpath := s.conf.Serv.ConfigPath

	s.fs = afero.NewBasePathFs(afero.NewOsFs(), cpath)
	s.conf.Auth.JWT.SetFS(s.fs)

	for i := range s.conf.Auths {
		s.conf.Auths[i].JWT.SetFS(s.fs)
	}
	return nil
}

func (s *service) initConfig() error {
	c := s.conf
	c.dirty = true

	// copy over db_type from database.type
	if c.Core.DBType == "" {
		c.Core.DBType = c.DB.Type
	}

	// Auths: validate and sanitize
	am := make(map[string]struct{})

	for i := 0; i < len(c.Auths); i++ {
		a := &c.Auths[i]

		if _, ok := am[a.Name]; ok {
			return fmt.Errorf("duplicate auth found: %s", a.Name)
		}
		am[a.Name] = struct{}{}
	}

	if c.HotDeploy {
		if c.AdminSecretKey != "" {
			s.asec = sha256.Sum256([]byte(s.conf.AdminSecretKey))
		} else {
			return fmt.Errorf("please set an admin_secret_key")
		}
	}

	// Actions: validate and sanitize
	axm := make(map[string]struct{})

	for i := 0; i < len(c.Actions); i++ {
		a := &c.Actions[i]

		if _, ok := axm[a.Name]; ok {
			return fmt.Errorf("duplicate action found: %s", a.Name)
		}

		if _, ok := am[a.AuthName]; !ok {
			return fmt.Errorf("invalid auth name: %s, For auth: %s", a.AuthName, a.Name)
		}
		axm[a.Name] = struct{}{}
	}

	if c.Auth.Type == "" || c.Auth.Type == "none" {
		c.Core.DefaultBlock = false
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

func (s *service) initDB() error {
	var err error

	if s.db != nil {
		return nil
	}

	s.db, err = newDB(s.conf, true, true, s.log, s.fs)
	if err != nil {
		return err
	}
	return nil
}
