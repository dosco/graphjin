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
	Error string          `json:"error,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
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

	var root json.RawMessage
	_, err = db.Query(pg.Scan(&root), finalSQL)

	if err != nil {
		errorResp(w, err)
		return
	}

	json.NewEncoder(w).Encode(gqlResp{Data: json.RawMessage(root)})
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
	userIDFn := fasttemplate.TagFunc(func(w io.Writer, _ string) (int, error) {
		if v := ctx.Value(userIDKey); v != nil {
			return w.Write([]byte(v.(string)))
		}
		return 0, errNoUserID
	})

	return map[string]interface{}{
		"USER_ID": userIDFn,
		"user_id": userIDFn,
	}
}
