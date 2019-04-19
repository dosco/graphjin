package serv

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/dosco/super-graph/qcode"
)

func errorResp(w http.ResponseWriter, err error) {
	b, _ := json.Marshal(gqlResp{Error: err.Error()})
	http.Error(w, string(b), http.StatusBadRequest)
}

func authCheck(ctx context.Context) bool {
	return (ctx.Value(userIDKey) != nil)
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
