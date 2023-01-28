package serv

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/serv/v3/internal/etags"
	"github.com/gorilla/websocket"
	"github.com/klauspost/compress/gzhttp"
	"github.com/rs/cors"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	maxReadBytes = 100000 // 100Kb
)

var errUnauthorized = errors.New("not authorized")

type extensions struct {
	Persisted apqExt `json:"persistedQuery"`
}

type apqExt struct {
	Version    int    `json:"version"`
	Sha256Hash string `json:"sha256Hash"`
}

type gqlReq struct {
	OpName string          `json:"operationName"`
	Query  string          `json:"query"`
	Vars   json.RawMessage `json:"variables"`
	Ext    extensions      `json:"extensions"`
}

type errorResp struct {
	Errors []string `json:"errors"`
}

func apiV1Handler(s1 *Service, ns *string, h http.Handler, ah auth.HandlerFunc) http.Handler {
	var zlog *zap.Logger
	s := s1.Load().(*service)

	if s.conf.Core.Debug {
		zlog = s.zlog
	}

	if ah != nil {
		authOpt := auth.Options{AuthFailBlock: s.conf.Serv.AuthFailBlock}
		useAuth, err := auth.NewAuth(s.conf.Auth, zlog, authOpt, ah)
		if err != nil {
			s.log.Fatalf("api: error with auth: %s", err)
		}
		h = useAuth(h)
	}

	if len(s.conf.AllowedOrigins) != 0 {
		allowedHeaders := []string{
			"Origin", "Accept", "Content-Type", "X-Requested-With", "Authorization",
		}

		if len(s.conf.AllowedHeaders) != 0 {
			allowedHeaders = s.conf.AllowedHeaders
		}

		c := cors.New(cors.Options{
			AllowedOrigins:   s.conf.AllowedOrigins,
			AllowedHeaders:   allowedHeaders,
			AllowCredentials: true,
			Debug:            s.conf.DebugCORS,
		})
		h = c.Handler(h)
	}

	h = etags.Handler(h, false)

	if s.conf.rateLimiterEnable() {
		h = rateLimiter(s1, h)
	}

	if s.conf.HTTPGZip {
		gz, err := gzhttp.NewWrapper(gzhttp.CompressionLevel(6))
		if err != nil {
			s.log.Fatalf("api: error with compression: %s", err)
		}
		h = gz(h)
	}

	return h
}

func (s1 *Service) apiV1GraphQL(ns *string, ah auth.HandlerFunc) http.Handler {
	dtrace := otel.GetTextMapPropagator()

	h := func(w http.ResponseWriter, r *http.Request) {
		var err error

		start := time.Now()
		s := s1.Load().(*service)

		w.Header().Set("Content-Type", "application/json")

		if websocket.IsWebSocketUpgrade(r) {
			s.apiV1Ws(w, r, ah)
			return
		}

		var req gqlReq

		ctx, opts := newDTrace(dtrace, r)
		ctx, span := s.spanStart(ctx, "GraphQL Request", opts...)
		defer span.End()

		switch r.Method {
		case "POST":
			var b []byte
			b, err = io.ReadAll(io.LimitReader(r.Body, maxReadBytes))
			if err == nil {
				defer r.Body.Close()
				err = json.Unmarshal(b, &req)
			}

		case "GET":
			q := r.URL.Query()
			req.Query = q.Get("query")
			req.OpName = q.Get("operationName")
			req.Vars = json.RawMessage(q.Get("variables"))

			if ext := q.Get("extensions"); ext != "" {
				err = json.Unmarshal([]byte(ext), &req.Ext)
			}
		}

		if err != nil {
			spanError(span, err)
			renderErr(w, err)
			return
		}

		var rc core.ReqConfig

		if req.apqEnabled() {
			rc.APQKey = (req.OpName + req.Ext.Persisted.Sha256Hash)
		}

		if rc.Vars == nil && len(s.conf.Core.HeaderVars) != 0 {
			rc.Vars = s.setHeaderVars(r)
		}

		if ns != nil {
			rc.SetNamespace(*ns)
		}

		if req.OpName == "subscription" {
			err := errors.New("use websockets for subscriptions")
			spanError(span, err)
			renderErr(w, err)
			return
		}

		res, err := s.gj.GraphQL(ctx, req.Query, req.Vars, &rc)
		if res == nil && err != nil {
			renderErr(w, err)
			return
		}

		s.responseHandler(
			ctx,
			w,
			r,
			start,
			rc,
			res,
			err)

		if span.IsRecording() {
			span.SetAttributes(
				attribute.String("http.path", r.RequestURI),
				attribute.String("http.method", r.Method),
				attribute.Bool("query.apq", req.apqEnabled()))
		}

		if err != nil {
			spanError(span, err)
		}
	}
	return http.HandlerFunc(h)
}

func (s1 *Service) apiV1Rest(ns *string, ah auth.HandlerFunc) http.Handler {
	rLen := len(routeREST)
	dtrace := otel.GetTextMapPropagator()

	h := func(w http.ResponseWriter, r *http.Request) {
		var err error

		start := time.Now()
		s := s1.Load().(*service)

		w.Header().Set("Content-Type", "application/json")

		if websocket.IsWebSocketUpgrade(r) {
			s.apiV1Ws(w, r, ah)
			return
		}

		var vars json.RawMessage
		var span trace.Span

		ctx, opts := newDTrace(dtrace, r)
		ctx, span = s.spanStart(ctx, "REST Request", opts...)
		defer span.End()

		if len(r.RequestURI) < rLen {
			err := errors.New("no query name defined")
			spanError(span, err)
			renderErr(w, err)
			return
		}

		queryName := r.RequestURI[rLen-1:]
		if n := strings.IndexRune(queryName, '?'); n != -1 {
			queryName = queryName[:n]
		}

		switch r.Method {
		case "POST":
			vars, err = parseBody(r)

		case "GET":
			vars = json.RawMessage(r.URL.Query().Get("variables"))
		}

		if err != nil {
			spanError(span, err)
			renderErr(w, err)
			return
		}

		var rc core.ReqConfig

		if rc.Vars == nil && len(s.conf.Core.HeaderVars) != 0 {
			rc.Vars = s.setHeaderVars(r)
		}

		if ns != nil {
			rc.SetNamespace(*ns)
		}

		res, err := s.gj.GraphQLByName(ctx, queryName, vars, &rc)
		s.responseHandler(
			ctx,
			w,
			r,
			start,
			rc,
			res,
			err)

		if span.IsRecording() {
			span.SetAttributes(
				attribute.String("http.path", r.RequestURI),
				attribute.String("http.method", r.Method))
		}

		if err != nil {
			spanError(span, err)
		}
	}
	return http.HandlerFunc(h)
}

func (s *service) responseHandler(ct context.Context,
	w http.ResponseWriter,
	r *http.Request,
	start time.Time,
	rc core.ReqConfig,
	res *core.Result,
	err error,
) {
	if s.hook != nil {
		s.hook(res)
	}

	if err == nil && r.Method == "GET" && res.Operation() == core.OpQuery {
		switch {
		case res.CacheControl() != "":
			w.Header().Set("Cache-Control", res.CacheControl())

		case s.conf.CacheControl != "":
			w.Header().Set("Cache-Control", s.conf.CacheControl)
		}

		w.Header().Set("ETag", hex.EncodeToString(res.Hash[:]))
	}

	if err := json.NewEncoder(w).Encode(res); err != nil {
		renderErr(w, err)
		return
	}

	rt := time.Since(start).Milliseconds()

	if s.logLevel >= logLevelInfo {
		s.reqLog(res, rc, rt, err)
	}

	if s.conf.ServerTiming {
		b := []byte("DB;dur=")
		b = strconv.AppendInt(b, rt, 10)
		w.Header().Set("Server-Timing", string(b))
	}
}

func (s *service) reqLog(res *core.Result, rc core.ReqConfig, resTimeMs int64, err error) {
	var fields []zapcore.Field
	var sql string

	if res != nil {
		sql = res.SQL()
		fields = []zapcore.Field{
			zap.String("op", res.OperationName()),
			zap.String("name", res.QueryName()),
			zap.String("role", res.Role()),
			zap.Int64("responseTimeMs", resTimeMs),
		}
	}

	if ns, ok := rc.GetNamespace(); ok {
		fields = append(fields, zap.String("namespace", ns))
	}

	if sql != "" && s.logLevel >= logLevelDebug {
		fields = append(fields, zap.String("sql", sql))
	}

	if sql != "" && s.conf.Core.Debug {
		s.log.Info(sql)
	}

	if err != nil {
		fields = append(fields, zap.Error(err))
		s.zlog.Error("query failed", fields...)
	} else {
		s.zlog.Info("query", fields...)
	}
}

func (s *service) setHeaderVars(r *http.Request) map[string]interface{} {
	vars := make(map[string]interface{})
	for k, v := range s.conf.Core.HeaderVars {
		vars[k] = func() string {
			if v1, ok := r.Header[v]; ok {
				return v1[0]
			}
			return ""
		}
	}
	return vars
}

func (r gqlReq) apqEnabled() bool {
	return r.Ext.Persisted.Sha256Hash != ""
}

// nolint:errcheck
func renderErr(w http.ResponseWriter, err error) {
	if err == errUnauthorized {
		w.WriteHeader(http.StatusUnauthorized)
	}

	err1 := json.NewEncoder(w).Encode(errorResp{[]string{err.Error()}})
	if err1 != nil {
		panic(fmt.Errorf("%s: %w", err, err1))
	}
}

func parseBody(r *http.Request) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r.Body, maxReadBytes))
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	return b, nil
}

func newDTrace(dtrace propagation.TextMapPropagator, r *http.Request) (context.Context, []trace.SpanStartOption) {
	ctx := dtrace.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

	opts := []trace.SpanStartOption{
		trace.WithAttributes(semconv.NetAttributesFromHTTPRequest(
			"tcp", r)...),
		trace.WithAttributes(semconv.EndUserAttributesFromHTTPRequest(
			r)...),
		trace.WithAttributes(semconv.HTTPServerAttributesFromHTTPRequest(
			"GraphJin", r.URL.Path, r)...),
		trace.WithSpanKind(trace.SpanKindServer),
	}

	return ctx, opts
}
