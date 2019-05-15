package serv

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/cespare/xxhash/v2"
	"github.com/dosco/super-graph/psql"
)

var (
	rmap map[uint64]*resolvFn
)

type resolvFn struct {
	IDField []byte
	Path    [][]byte
	Fn      func(r *http.Request, id []byte) ([]byte, error)
}

func initResolvers() {
	rmap = make(map[uint64]*resolvFn)

	for _, t := range conf.DB.Tables {
		initRemotes(t)
	}
}

func initRemotes(t configTable) {
	h := xxhash.New()

	for _, r := range t.Remotes {
		// defines the table column to be used as an id in the
		// remote request
		idcol := r.ID

		// if no table column specified in the config then
		// use the primary key of the table as the id
		if len(idcol) == 0 {
			idcol = pcompile.IDColumn(t.Name)
		}
		idk := fmt.Sprintf("__%s_%s", t.Name, idcol)

		// register a relationship between the remote data
		// and the database table

		h.WriteString(strings.ToLower(r.Name))
		h.WriteString(t.Name)
		key := h.Sum64()
		h.Reset()

		val := &psql.DBRel{
			Type: psql.RelRemote,
			Col1: idcol,
			Col2: idk,
		}
		pcompile.AddRelationship(key, val)

		// the function thats called to resolve this remote
		// data request
		fn := buildFn(r)

		path := [][]byte{}
		for _, p := range strings.Split(r.Path, ".") {
			path = append(path, []byte(p))
		}

		rf := &resolvFn{
			IDField: []byte(idk),
			Path:    path,
			Fn:      fn,
		}

		// index resolver obj by parent and child names
		rmap[mkkey(h, r.Name, t.Name)] = rf

		// index resolver obj by IDField
		rmap[xxhash.Sum64(rf.IDField)] = rf
	}
}

func buildFn(r configRemote) func(*http.Request, []byte) ([]byte, error) {
	reqURL := strings.Replace(r.URL, "$id", "%s", 1)
	client := &http.Client{}
	h := make(http.Header, len(r.PassHeaders))

	for _, v := range r.SetHeaders {
		h.Set(v.Name, v.Value)
	}

	fn := func(inReq *http.Request, id []byte) ([]byte, error) {
		req, err := http.NewRequest("GET", fmt.Sprintf(reqURL, id), nil)
		if err != nil {
			return nil, err
		}

		for _, v := range r.PassHeaders {
			h.Set(v, inReq.Header.Get(v))
		}
		req.Header = h

		res, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()

		b, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return nil, err
		}

		return b, nil
	}

	return fn
}
