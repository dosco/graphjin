// Package serv provides an API to include and use the GraphJin service with your own code.
// For detailed documentation visit https://graphjin.com
//
// Example usage:
/*
	package main

	import (
		"database/sql"
		"fmt"
		"time"
		"github.com/dosco/graphjin/core"
		_ "github.com/jackc/pgx/v4/stdlib"
	)

	func main() {
		conf := serv.Config{ AppName: "Test App" }
		conf.DB.Host := "127.0.0.1"
		conf.DB.Port := 5432
		conf.DB.DBName := "test_db"
		conf.DB.User := "postgres"
		conf.DB.Password := "postgres"

		gjs, err := serv.NewGraphJinService(conf)
		if err != nil {
			log.Fatal(err)
		}

	 	if err := gjs.Start(); err != nil {
			log.Fatal(err)
		}
	}
*/
package serv

import (
	"database/sql"
	"fmt"
	"net/http"

	"github.com/dosco/graphjin/core"
	"github.com/dosco/graphjin/internal/util"
	"go.uber.org/zap"
)

type Service struct {
	log      *zap.SugaredLogger // logger
	zlog     *zap.Logger        // faster logger
	logLevel int                // log level
	conf     *Config            // parsed config
	db       *sql.DB            // database connection pool
	gj       *core.GraphJin
}

func NewGraphJinService(conf *Config) (*Service, error) {
	if conf == nil {
		conf = &Config{Core: Core{Debug: true}}
	}

	zlog := util.NewLogger(conf.LogFormat == "json")
	log := zlog.Sugar()

	if err := initConfig(conf, log); err != nil {
		return nil, err
	}

	s := &Service{conf: conf, log: log, zlog: zlog}
	initLogLevel(s)
	validateConf(s)

	if s.conf != nil && s.conf.WatchAndReload {
		initWatcher(s)
	}

	return s, nil
}

func (s *Service) init() error {
	var err error

	if s.db != nil {
		return nil
	}

	if s.db, err = s.newDB(true, true); err != nil {
		return fmt.Errorf("Failed to connect to database: %w", err)
	}

	s.gj, err = core.NewGraphJin(&s.conf.Core, s.db)
	if err != nil {
		return fmt.Errorf("Failed to initialize: %w", err)
	}

	return nil
}

func (s *Service) Start() error {
	if err := s.init(); err != nil {
		return err
	}
	startHTTP(s)
	return nil
}

func (s *Service) Attach(mux *http.ServeMux) error {
	if err := s.init(); err != nil {
		return err
	}
	_, err := routeHandler(s, mux)
	return err
}
