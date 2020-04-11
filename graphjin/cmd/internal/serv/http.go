package serv

import (
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/dosco/super-graph/cmd/internal/serv/internal/auth"
	"github.com/dosco/super-graph/core"
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
	Error error `json:"error"`
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
		renderErr(w, errUnauthorized, nil)
		return
	}

	b, err := ioutil.ReadAll(io.LimitReader(r.Body, maxReadBytes))
	if err != nil {
		renderErr(w, err, nil)
		return
	}
	defer r.Body.Close()

	req := gqlReq{}

	err = json.Unmarshal(b, &req)
	if err != nil {
		renderErr(w, err, nil)
		return
	}

	if strings.EqualFold(req.OpName, introspectionQuery) {
		introspect(w)
		return
	}

	res, err := sg.GraphQL(ct, req.Query, req.Vars)

	if logLevel >= LogLevelDebug {
		log.Printf("DBG query:\n%s\nsql:\n%s", req.Query, res.SQL())
	}

	if err != nil {
		renderErr(w, err, res)
		return
	}

	json.NewEncoder(w).Encode(res)

	if logLevel >= LogLevelInfo {
		zlog.Info("success",
			zap.String("op", res.Operation()),
			zap.String("name", res.QueryName()),
			zap.String("role", res.Role()),
		)
	}
}

//nolint: errcheck
func renderErr(w http.ResponseWriter, err error, res *core.Result) {
	if err == errUnauthorized {
		w.WriteHeader(http.StatusUnauthorized)
	}

	json.NewEncoder(w).Encode(&errorResp{err})

	if logLevel >= LogLevelError {
		if res != nil {
			zlog.Error(err.Error(),
				zap.String("op", res.Operation()),
				zap.String("name", res.QueryName()),
				zap.String("role", res.Role()),
			)
		} else {
			zlog.Error(err.Error())
		}
	}
}
