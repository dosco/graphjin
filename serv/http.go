package serv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/dosco/super-graph/qcode"
	"github.com/go-pg/pg"
	"github.com/gorilla/websocket"
	"github.com/valyala/fasttemplate"
)

const (
	introspectionQuery = "IntrospectionQuery"
	openVar            = "{{"
	closeVar           = "}}"
)

var (
	upgrader    = websocket.Upgrader{}
	errNoUserID = errors.New("no user_id available")
)

type gqlReq struct {
	OpName    string            `json:"operationName"`
	Query     string            `json:"query"`
	Variables map[string]string `json:"variables"`
}

type gqlResp struct {
	Error      string          `json:"error,omitempty"`
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
	ctx := r.Context()

	if authFailBlock == authFailBlockAlways && authCheck(ctx) == false {
		http.Error(w, "Not authorized", 401)
		return
	}

	b, err := ioutil.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		errorResp(w, err)
		return
	}

	req := &gqlReq{}
	if err := json.Unmarshal(b, req); err != nil {
		errorResp(w, err)
		return
	}

	if strings.EqualFold(req.OpName, introspectionQuery) {
		dat, err := ioutil.ReadFile("test.schema")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Write(dat)
		return
	}

	qc, err := qcompile.CompileQuery(req.Query)
	if err != nil {
		errorResp(w, err)
		return
	}

	var sqlStmt strings.Builder

	if err := pcompile.Compile(&sqlStmt, qc); err != nil {
		errorResp(w, err)
		return
	}

	t := fasttemplate.New(sqlStmt.String(), openVar, closeVar)
	sqlStmt.Reset()

	_, err = t.Execute(&sqlStmt, varValues(ctx))
	if err == errNoUserID &&
		authFailBlock == authFailBlockPerQuery &&
		authCheck(ctx) == false {
		http.Error(w, "Not authorized", 401)
		return
	}
	if err != nil {
		errorResp(w, err)
		return
	}
	finalSQL := sqlStmt.String()
	if debug > 0 {
		fmt.Println(finalSQL)
	}
	st := time.Now()

	var root json.RawMessage
	_, err = db.Query(pg.Scan(&root), finalSQL)

	if err != nil {
		errorResp(w, err)
		return
	}

	et := time.Now()
	resp := gqlResp{}

	if tracing {
		resp.Extensions = &extensions{newTrace(st, et, qc)}
	}

	resp.Data = json.RawMessage(root)
	json.NewEncoder(w).Encode(resp)
}

/*
func apiv1Ws(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer c.Close()
	for {
		mt, message, err := c.ReadMessage()
		if err != nil {
			fmt.Println("read:", err)
			break
		}
		fmt.Printf("recv: %s", message)
		err = c.WriteMessage(mt, message)
		if err != nil {
			fmt.Println("write:", err)
			break
		}
	}
}

func serve(w http.ResponseWriter, r *http.Request) {
	// if websocket.IsWebSocketUpgrade(r) {
	// 	apiv1Ws(w, r)
	// 	return
	// }
	apiv1Http(w, r)
}
*/

func errorResp(w http.ResponseWriter, err error) {
	b, _ := json.Marshal(gqlResp{Error: err.Error()})
	http.Error(w, string(b), http.StatusBadRequest)
}

func authCheck(ctx context.Context) bool {
	return (ctx.Value(userIDKey) != nil)
}

func varValues(ctx context.Context) map[string]interface{} {
	uidFn := fasttemplate.TagFunc(func(w io.Writer, _ string) (int, error) {
		if v := ctx.Value(userIDKey); v != nil {
			return w.Write([]byte(v.(string)))
		}
		return 0, errNoUserID
	})

	uidpFn := fasttemplate.TagFunc(func(w io.Writer, _ string) (int, error) {
		if v := ctx.Value(userIDProviderKey); v != nil {
			return w.Write([]byte(v.(string)))
		}
		return 0, errNoUserID
	})

	return map[string]interface{}{
		"USER_ID":          uidFn,
		"user_id":          uidFn,
		"USER_ID_PROVIDER": uidpFn,
		"user_id_provider": uidpFn,
	}
}

func newTrace(st, et time.Time, qc *qcode.QCode) *trace {
	du := et.Sub(et)

	t := &trace{
		Version:   1,
		StartTime: st,
		EndTime:   et,
		Duration:  du,
		Execution: execution{
			[]resolver{
				resolver{
					Path:        []string{qc.Query.Select.Table},
					ParentType:  "Query",
					FieldName:   qc.Query.Select.Table,
					ReturnType:  "object",
					StartOffset: 1,
					Duration:    du,
				},
			},
		},
	}

	return t
}
