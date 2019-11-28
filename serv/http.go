package serv

import (
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"time"
)

const (
	maxReadBytes       = 100000 // 100Kb
	introspectionQuery = "IntrospectionQuery"
	openVar            = "{{"
	closeVar           = "}}"
)

var (
	errUnauthorized = errors.New("not authorized")
)

type gqlReq struct {
	OpName string          `json:"operationName"`
	Query  string          `json:"query"`
	Vars   json.RawMessage `json:"variables"`
	ref    string
	role   string
	hdr    http.Header
}

type gqlResp struct {
	Error      string          `json:"message,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
	Extensions *extensions     `json:"extensions,omitempty"`
}

type extensions struct {
	Tracing *trace `json:"tracing,omitempty"`
}

type trace struct {
	Version   int           `json:"version"`
	StartTime time.Time     `json:"startTime"`
	EndTime   time.Time     `json:"endTime"`
	Duration  time.Duration `json:"duration"`
	Execution execution     `json:"execution"`
}

type execution struct {
	Resolvers []resolver `json:"resolvers"`
}

type resolver struct {
	Path        []string      `json:"path"`
	ParentType  string        `json:"parentType"`
	FieldName   string        `json:"fieldName"`
	ReturnType  string        `json:"returnType"`
	StartOffset int           `json:"startOffset"`
	Duration    time.Duration `json:"duration"`
}

func apiv1Http(w http.ResponseWriter, r *http.Request) {
	ctx := &coreContext{Context: r.Context()}

	//nolint: errcheck
	if conf.AuthFailBlock && !authCheck(ctx) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(gqlResp{Error: errUnauthorized.Error()})
		return
	}

	b, err := ioutil.ReadAll(io.LimitReader(r.Body, maxReadBytes))
	if err != nil {
		errlog.Error().Err(err).Msg("failed to read request body")
		errorResp(w, err)
		return
	}
	defer r.Body.Close()

	err = json.Unmarshal(b, &ctx.req)
	if err != nil {
		errlog.Error().Err(err).Msg("failed to decode json request body")
		errorResp(w, err)
		return
	}

	if strings.EqualFold(ctx.req.OpName, introspectionQuery) {
		introspect(w)
		return
	}

	err = ctx.handleReq(w, r)

	//nolint: errcheck
	if err == errUnauthorized {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(gqlResp{Error: err.Error()})
		return
	}

	if err != nil {
		errlog.Error().Err(err).Msg("failed to handle request")
		errorResp(w, err)
		return
	}
}

//nolint: errcheck
func errorResp(w http.ResponseWriter, err error) {
	json.NewEncoder(w).Encode(gqlResp{Error: err.Error()})
}
