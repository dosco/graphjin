package serv

import (
	"bytes"
	"fmt"
	"io"

	"github.com/dosco/super-graph/qcode"
	"github.com/go-pg/pg"
	"github.com/valyala/fasttemplate"
)

type preparedItem struct {
	stmt    *pg.Stmt
	args    []string
	skipped uint32
	qc      *qcode.QCode
}

var (
	_preparedList map[string]*preparedItem
)

func initPreparedList() {
	_preparedList = make(map[string]*preparedItem)

	for k, v := range _allowList.list {
		err := prepareStmt(k, v.gql)
		if err != nil {
			panic(err)
		}
	}
}

func prepareStmt(key, gql string) error {
	if len(gql) == 0 || len(key) == 0 {
		return nil
	}

	qc, err := qcompile.CompileQuery([]byte(gql))
	if err != nil {
		return err
	}

	buf := &bytes.Buffer{}

	skipped, err := pcompile.Compile(qc, buf)
	if err != nil {
		return err
	}

	t := fasttemplate.New(buf.String(), `('{{`, `}}')`)
	am := make([]string, 0, 5)
	i := 0

	finalSQL := t.ExecuteFuncString(func(w io.Writer, tag string) (int, error) {
		am = append(am, tag)
		i++
		return w.Write([]byte(fmt.Sprintf("$%d", i)))
	})

	if err != nil {
		return err
	}

	pstmt, err := db.Prepare(finalSQL)
	if err != nil {
		return err
	}

	_preparedList[key] = &preparedItem{
		stmt:    pstmt,
		args:    am,
		skipped: skipped,
		qc:      qc,
	}

	return nil
}
