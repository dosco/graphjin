package serv

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/chirino/graphql"
	"github.com/chirino/graphql/relay"
	"github.com/dosco/super-graph/core"
	"github.com/dosco/super-graph/internal/serv/internal/auth"
	"github.com/rs/cors"
	"go.uber.org/zap"
)

const (
	maxReadBytes       = 100000 // 100Kb
	introspectionQuery = "IntrospectionQuery"
)

var (
	errUnauthorized = errors.New("not authorized")
)

type gqlReq struct {
	OpName string          `json:"operationName"`
	Query  string          `json:"query"`
	Vars   json.RawMessage `json:"variables"`
}

type errorResp struct {
	Error string `json:"error"`
}

func apiV1Handler() http.Handler {
	h, err := auth.WithAuth(http.HandlerFunc(apiV1), &conf.Auth)
	if err != nil {
		log.Fatalf("ERR %s", err)
	}

	if len(conf.AllowedOrigins) != 0 {
		c := cors.New(cors.Options{
			AllowedOrigins:   conf.AllowedOrigins,
			AllowCredentials: true,
			Debug:            conf.DebugCORS,
		})
		h = c.Handler(h)
	}

	return h
}

func apiV1(w http.ResponseWriter, r *http.Request) {
	ct := r.Context()

	//nolint: errcheck
	if conf.AuthFailBlock && !auth.IsAuth(ct) {
		renderErr(w, errUnauthorized)
		return
	}

	// this http handler supports GET, PUT requests..
	var res *core.Result = nil
	h := &relay.Handler{
		MaxRequestSizeBytes: maxReadBytes,
		ServeGraphQL: func(request *graphql.Request) *graphql.Response {
			response := sg.ServeGraphQL(request)
			res = response.Details["*core.Result"].(*core.Result)
			return response
		},
	}
	h.ServeHTTP(w, r)

	doLog := true
	if !conf.Production && res.QueryName() == "IntrospectionQuery" {
		doLog = false
	}

	if doLog && logLevel >= LogLevelDebug {
		log.Printf("DBG query %s: %s", res.QueryName(), res.SQL())
	}
	if doLog && logLevel >= LogLevelInfo {
		zlog.Info("success",
			zap.String("op", res.Operation()),
			zap.String("name", res.QueryName()),
			zap.String("role", res.Role()),
		)
	}
}

//nolint: errcheck
func renderErr(w http.ResponseWriter, err error) {
	if err == errUnauthorized {
		w.WriteHeader(http.StatusUnauthorized)
	}

	json.NewEncoder(w).Encode(errorResp{err.Error()})
}
