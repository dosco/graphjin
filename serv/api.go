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
		_ "github.com/jackc/pgx/v5/stdlib"
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
	"context"
	"database/sql"
	"encoding/json"
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
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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

type HookFn func(*core.Result)

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
	hook         HookFn
	prod         bool
	deployActive bool
	adminCount   int32
	namespace    nspace
	tracer       trace.Tracer
}

type nspace struct {
	name string
	set  bool
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

func OptionSetDB(db *sql.DB) Option {
	return func(s *service) error {
		s.db = db
		return nil
	}
}

func OptionSetHookFunc(fn HookFn) Option {
	return func(s *service) error {
		s.hook = fn
		return nil
	}
}

func OptionSetNamespace(namespace string) Option {
	return func(s *service) error {
		s.namespace = nspace{namespace, true}
		return nil
	}
}

func OptionSetFS(fs afero.Fs) Option {
	return func(s *service) error {
		s.fs = fs
		return nil
	}
}

func OptionSetZapLogger(zlog *zap.Logger) Option {
	return func(s *service) error {
		s.zlog = zlog
		s.log = zlog.Sugar()
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
		tracer:       otel.Tracer("graphjin.com/serv"),
	}
	s.conf.Core.Production = prod

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
	opts := []core.Option{core.OptionSetFS(s.fs)}
	if s.namespace.set {
		opts = append(opts, core.OptionSetNamespace(s.namespace.name))
	}

	var err error
	s.gj, err = core.NewGraphJin(&s.conf.Core, s.db, opts...)
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

	opts := []core.Option{core.OptionSetFS(bfs.fs)}
	if s.namespace.set {
		opts = append(opts, core.OptionSetNamespace(s.namespace.name))
	}

	s.gj, err = core.NewGraphJin(&s.conf.Core, s.db, opts...)
	return err
}

func (s *Service) Deploy(conf *Config, options ...Option) error {
	var err error
	os := s.Load().(*service)

	if conf == nil {
		return nil
	}

	s1, err := newGraphJinService(conf, os.db, options...)
	if err != nil {
		return err
	}
	s1.srv = os.srv
	s1.closeFn = os.closeFn
	s1.namespace = os.namespace

	s.Store(s1)
	return nil
}

func (s *Service) Start() error {
	startHTTP(s)
	return nil
}

func (s *Service) Attach(mux Mux) error {
	return s.attach(mux, nspace{})
}

func (s *Service) AttachWithNamespace(mux Mux, namespace string) error {
	return s.attach(mux, nspace{namespace, true})
}

func (s *Service) attach(mux Mux, ns nspace) error {
	if _, err := routesHandler(s, mux, ns); err != nil {
		return err
	}

	s1 := s.Load().(*service)

	ver := version
	dep := s1.conf.name

	if version == "" {
		ver = "not-set"
	}

	fields := []zapcore.Field{
		zap.String("version", ver),
		zap.String("app-name", s1.conf.AppName),
		zap.String("deployment-name", dep),
		zap.String("env", os.Getenv("GO_ENV")),
		zap.Bool("hot-deploy", s1.conf.HotDeploy),
		zap.Bool("production", s1.conf.Core.Production),
		zap.Bool("secrets-used", (s1.conf.Serv.SecretsFile != "")),
	}

	if s1.namespace.set {
		fields = append(fields, zap.String("namespace", s1.namespace.name))
	}

	if s1.conf.HotDeploy {
		fields = append(fields, zap.String("deployment-name", dep))
	}

	s1.zlog.Info("GraphJin attached to router", fields...)
	return nil
}

func (s *Service) GetGraphJin() *core.GraphJin {
	s1 := s.Load().(*service)
	return s1.gj
}

func (s *Service) Subscribe(
	c context.Context,
	query string,
	vars json.RawMessage,
	rc *core.ReqConfig) (*core.Member, error) {

	s1 := s.Load().(*service)
	return s1.gj.Subscribe(c, query, vars, rc)
}

func (s *Service) GetDB() *sql.DB {
	s1 := s.Load().(*service)
	return s1.db
}

// Reload redoes database discover and reinitializes GraphJin.
func (s *Service) Reload() error {
	s1 := s.Load().(*service)
	return s1.gj.Reload()
}

func (s *service) spanStart(c context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return s.tracer.Start(c, name, opts...)
}

func spanError(span trace.Span, err error) {
	if span.IsRecording() {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}
