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
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/dosco/graphjin/core"
	"github.com/dosco/graphjin/internal/util"
	"github.com/spf13/afero"
	"go.uber.org/zap"
)

type Service struct {
	atomic.Value
	opt   []Option
	cpath string
}

type servState int

const (
	servStarted servState = iota + 1
	servListening
)

type service struct {
	log          *zap.SugaredLogger // logger
	zlog         *zap.Logger        // faster logger
	logLevel     int                // log level
	conf         *Config            // parsed config
	db           *sql.DB            // database connection pool
	gj           *core.GraphJin
	srv          *http.Server
	fs           afero.Fs
	asec         [32]byte
	closeFn      func()
	chash        string
	state        servState
	prod         bool
	deployActive bool
}

type Option func(*service) error

func NewGraphJinService(conf *Config, options ...Option) (*Service, error) {
	if conf.dirty {
		return nil, errors.New("do not re-use config object")
	}

	s, err := newGraphJinService(conf, nil, options...)
	if err != nil {
		return nil, err
	}

	s1 := &Service{opt: options, cpath: conf.Serv.ConfigPath}
	s1.Store(s)

	if s.conf.WatchAndReload {
		initConfigWatcher(s1)
	}

	if s.conf.HotDeploy {
		initHotDeployWatcher(s1)
	}

	return s1, nil
}

func OptionSetFS(fs afero.Fs) Option {
	return func(s *service) error {
		s.fs = fs
		return nil
	}
}

func OptionDeployActive() Option {
	return func(s *service) error {
		s.deployActive = true
		return nil
	}
}

func newGraphJinService(conf *Config, db *sql.DB, options ...Option) (*service, error) {
	var err error
	if conf == nil {
		conf = &Config{Core: Core{Debug: true}}
	}

	zlog := util.NewLogger(conf.LogFormat == "json")
	prod := conf.Serv.Production || os.Getenv("GO_ENV") == "production"

	s := &service{
		conf:         conf,
		zlog:         zlog,
		log:          zlog.Sugar(),
		db:           db,
		chash:        conf.hash,
		prod:         prod,
		deployActive: prod && conf.HotDeploy && db == nil,
	}

	if err := s.initConfig(); err != nil {
		return nil, err
	}

	for _, op := range options {
		if err := op(s); err != nil {
			return nil, err
		}
	}

	if err := s.initFS(); err != nil {
		return nil, err
	}

	initLogLevel(s)
	validateConf(s)

	if err := s.initDB(); err != nil {
		return nil, err
	}

	if s.deployActive {
		err = s.hotStart()
	} else {
		err = s.normalStart()
	}

	if err != nil {
		return nil, err
	}

	s.state = servStarted
	return s, nil
}

func (s *service) normalStart() error {
	var err error
	s.gj, err = core.NewGraphJin(&s.conf.Core, s.db, core.OptionSetFS(s.fs))
	return err
}

func (s *service) hotStart() error {
	ab, err := fetchActiveBundle(s.db)
	if err != nil {
		if strings.Contains(err.Error(), "_graphjin.") {
			return fmt.Errorf("please run 'graphjin init' to setup database for hot-deploy")
		}
		return err
	}

	if ab == nil {
		return s.normalStart()
	}

	cf := s.conf.vi.ConfigFileUsed()
	cf = filepath.Base(strings.TrimSuffix(cf, filepath.Ext(cf)))
	cf = path.Join("/", cf)

	bfs, err := bundle2Fs(ab.name, ab.hash, cf, ab.bundle)
	if err != nil {
		return err
	}
	secFile := s.conf.Serv.SecretsFile
	s.conf = bfs.conf
	s.chash = bfs.conf.hash
	s.conf.Serv.SecretsFile = secFile

	if err := s.initConfig(); err != nil {
		return err
	}

	s.gj, err = core.NewGraphJin(&s.conf.Core, s.db, core.OptionSetFS(bfs.fs))
	return err
}

func (s1 *Service) Deploy(conf *Config, options ...Option) error {
	var err error
	os := s1.Load().(*service)

	if conf == nil {
		return nil
	}

	s, err := newGraphJinService(conf, os.db, options...)
	if err != nil {
		return err
	}
	s.srv = os.srv
	s.closeFn = os.closeFn

	s1.Store(s)
	return nil
}

func (s1 *Service) Start() error {
	startHTTP(s1)
	return nil
}

func (s1 *Service) Attach(mux *http.ServeMux) error {
	_, err := routeHandler(s1, mux)
	return err
}
