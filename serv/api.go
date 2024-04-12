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
		"github.com/dosco/graphjin/core/v3"
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
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/core/v3"
	otelPlugin "github.com/dosco/graphjin/plugin/otel/v3"
	"github.com/dosco/graphjin/serv/v3/internal/util"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type HttpService struct {
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

type GraphjinService struct {
	log          *zap.SugaredLogger // logger
	zlog         *zap.Logger        // faster logger
	logLevel     int                // log level
	conf         *Config            // parsed config
	db           *sql.DB            // database connection pool
	gj           *core.GraphJin
	srv          *http.Server
	fs           core.FS
	asec         [32]byte
	closeFn      func()
	chash        string
	state        servState
	hook         HookFn
	prod         bool
	deployActive bool
	adminCount   int32
	namespace    *string
	tracer       trace.Tracer
}

type Option func(*GraphjinService) error

// NewGraphJinService a new service
func NewGraphJinService(conf *Config, options ...Option) (*HttpService, error) {
	if conf.dirty {
		return nil, errors.New("do not re-use config object")
	}

	s, err := newGraphJinService(conf, nil, options...)
	if err != nil {
		return nil, err
	}

	s1 := &HttpService{opt: options, cpath: conf.Serv.ConfigPath}
	s1.Store(s)

	if s.conf.WatchAndReload {
		initConfigWatcher(s1)
	}

	if s.conf.HotDeploy {
		initHotDeployWatcher(s1)
	}

	return s1, nil
}

// OptionSetDB sets a new db client
func OptionSetDB(db *sql.DB) Option {
	return func(s *GraphjinService) error {
		s.db = db
		return nil
	}
}

// OptionSetHookFunc sets a function to be called on every request
func OptionSetHookFunc(fn HookFn) Option {
	return func(s *GraphjinService) error {
		s.hook = fn
		return nil
	}
}

// OptionSetNamespace sets service namespace
func OptionSetNamespace(namespace string) Option {
	return func(s *GraphjinService) error {
		s.namespace = &namespace
		return nil
	}
}

// OptionSetFS sets service filesystem
func OptionSetFS(fs core.FS) Option {
	return func(s *GraphjinService) error {
		s.fs = fs
		return nil
	}
}

// OptionSetZapLogger sets service structured logger
func OptionSetZapLogger(zlog *zap.Logger) Option {
	return func(s *GraphjinService) error {
		s.zlog = zlog
		s.log = zlog.Sugar()
		return nil
	}
}

// OptionDeployActive caused the active config to be deployed on
func OptionDeployActive() Option {
	return func(s *GraphjinService) error {
		s.deployActive = true
		return nil
	}
}

// newGraphJinService creates a new service
func newGraphJinService(conf *Config, db *sql.DB, options ...Option) (*GraphjinService, error) {
	var err error
	if conf == nil {
		conf = &Config{Core: Core{Debug: true}}
	}

	zlog := util.NewLogger(conf.LogFormat == "json")
	prod := conf.Serv.Production
	conf.Core.Production = prod

	s := &GraphjinService{
		conf:         conf,
		zlog:         zlog,
		log:          zlog.Sugar(),
		db:           db,
		chash:        conf.hash,
		prod:         prod,
		deployActive: prod && conf.HotDeploy && db == nil,
		tracer:       otel.Tracer("graphjin.com/serv"),
	}

	if err := s.initConfig(); err != nil {
		return nil, err
	}

	if err := s.initFS(); err != nil {
		return nil, err
	}

	for _, op := range options {
		if err := op(s); err != nil {
			return nil, err
		}
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

// normalStart starts the service in normal mode
func (s *GraphjinService) normalStart() error {
	opts := []core.Option{core.OptionSetFS(s.fs)}
	if s.namespace != nil {
		opts = append(opts, core.OptionSetNamespace(*s.namespace))
	}

	var err error
	s.gj, err = core.NewGraphJin(&s.conf.Core, s.db, opts...)
	return err
}

// hotStart starts the service in hot-deploy mode
func (s *GraphjinService) hotStart() error {
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

	cf := s.conf.viper.ConfigFileUsed()
	cf = filepath.Base(strings.TrimSuffix(cf, filepath.Ext(cf)))
	cf = filepath.Join("/", cf)

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

	opts := []core.Option{
		core.OptionSetFS(newAferoFS(bfs.fs, "/")),
		core.OptionSetTrace(otelPlugin.NewTracerFrom(s.tracer)),
	}

	if s.namespace != nil {
		opts = append(opts,
			core.OptionSetNamespace(*s.namespace))
	}

	s.gj, err = core.NewGraphJin(&s.conf.Core, s.db, opts...)
	return err
}

// Deploy a new configuration
func (s *HttpService) Deploy(conf *Config, options ...Option) error {
	var err error
	os := s.Load().(*GraphjinService)

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

// Start the service listening on the configured port
func (s *HttpService) Start() error {
	startHTTP(s)
	return nil
}

// Attach route to the internal http service
func (s *HttpService) Attach(mux Mux) error {
	return s.attach(mux, nil)
}

// AttachWithNS a namespaced route to the internal http service
func (s *HttpService) AttachWithNS(mux Mux, namespace string) error {
	return s.attach(mux, &namespace)
}

// attach attaches the service to the router
func (s *HttpService) attach(mux Mux, ns *string) error {
	if _, err := routesHandler(s, mux, ns); err != nil {
		return err
	}

	s1 := s.Load().(*GraphjinService)

	ver := version
	dep := s1.conf.name

	if ver == "" {
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

	if s1.namespace != nil {
		fields = append(fields, zap.String("namespace", *s1.namespace))
	}

	if s1.conf.HotDeploy {
		fields = append(fields, zap.String("deployment-name", dep))
	}

	s1.zlog.Info("GraphJin attached to router", fields...)
	return nil
}

// GraphQLis the http handler the GraphQL endpoint
func (s *HttpService) GraphQL(ah auth.HandlerFunc) http.Handler {
	return s.apiHandler(nil, ah, false)
}

// GraphQLWithNS is the http handler the namespaced GraphQL endpoint
func (s *HttpService) GraphQLWithNS(ah auth.HandlerFunc, ns string) http.Handler {
	return s.apiHandler(&ns, ah, false)
}

// REST is the http handler the REST endpoint
func (s *HttpService) REST(ah auth.HandlerFunc) http.Handler {
	return s.apiHandler(nil, ah, true)
}

// RESTWithNS is the http handler the namespaced REST endpoint
func (s *HttpService) RESTWithNS(ah auth.HandlerFunc, ns string) http.Handler {
	return s.apiHandler(&ns, ah, true)
}

func (s *HttpService) apiHandler(ns *string, ah auth.HandlerFunc, rest bool) http.Handler {
	var h http.Handler
	if rest {
		h = s.apiV1Rest(ns, ah)
	} else {
		h = s.apiV1GraphQL(ns, ah)
	}
	return apiV1Handler(s, ns, h, ah)
}

// WebUI is the http handler the web ui endpoint
func (s *HttpService) WebUI(routePrefix, gqlEndpoint string) http.Handler {
	return webuiHandler(routePrefix, gqlEndpoint)
}

// GetGraphJin fetching internal GraphJin core
func (s *HttpService) GetGraphJin() *core.GraphJin {
	s1 := s.Load().(*GraphjinService)
	return s1.gj
}

// GetDB fetching internal db client
func (s *HttpService) GetDB() *sql.DB {
	s1 := s.Load().(*GraphjinService)
	return s1.db
}

// Reload re-runs database discover and reinitializes service.
func (s *HttpService) Reload() error {
	s1 := s.Load().(*GraphjinService)
	return s1.gj.Reload()
}

// spanStart starts the tracer
func (s *GraphjinService) spanStart(c context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return s.tracer.Start(c, name, opts...)
}

// spanError records an error in the span
func spanError(span trace.Span, err error) {
	if span.IsRecording() {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}
