package core

import (
	"bytes"
	"errors"
	"fmt"
	"hash/maphash"
	"net/http"
	"sync"

	"github.com/dosco/super-graph/core/internal/qcode"
	"github.com/dosco/super-graph/jsn"
)

func (sg *SuperGraph) execRemoteJoin(res qres, hdr http.Header) (qres, error) {
	var err error

	sel := res.q.st.qc.Selects
	h := maphash.Hash{}
	h.SetSeed(sg.hashSeed)

	// fetch the field name used within the db response json
	// that are used to mark insertion points and the mapping between
	// those field names and their select objects
	fids, sfmap, err := sg.parentFieldIds(&h, sel, res.q.st.md.Remotes())
	if err != nil {
		return res, err
	}

	// fetch the field values of the marked insertion points
	// these values contain the id to be used with fetching remote data
	from := jsn.Get(res.data, fids)
	var to []jsn.Field

	if len(from) == 0 {
		return res, errors.New("something wrong no remote ids found in db response")
	}

	to, err = sg.resolveRemotes(hdr, &h, from, sel, sfmap)
	if err != nil {
		return res, err
	}

	var ob bytes.Buffer

	err = jsn.Replace(&ob, res.data, from, to)
	if err != nil {
		return res, err
	}
	res.data = ob.Bytes()

	return res, nil
}

func (sg *SuperGraph) resolveRemotes(
	hdr http.Header,
	h *maphash.Hash,
	from []jsn.Field,
	sel []qcode.Select,
	sfmap map[uint64]*qcode.Select) ([]jsn.Field, error) {

	// replacement data for the marked insertion points
	// key and value will be replaced by whats below
	to := make([]jsn.Field, len(from))

	var wg sync.WaitGroup
	wg.Add(len(from))

	var cerr error

	for i, id := range from {
		// use the json key to find the related Select object
		_, _ = h.Write(id.Key)
		k1 := h.Sum64()
		h.Reset()

		s, ok := sfmap[k1]
		if !ok {
			return nil, fmt.Errorf("invalid remote field key")
		}
		p := sel[s.ParentID]

		pti, err := sg.schema.GetTableInfo(p.Table)
		if err != nil {
			return nil, err
		}

		// then use the Table nme in the Select and it's parent
		// to find the resolver to use for this relationship
		k2 := mkkey(h, s.Table, pti.Name)

		r, ok := sg.rmap[k2]
		if !ok {
			return nil, fmt.Errorf("no resolver found")
		}

		id := jsn.Value(id.Value)
		if len(id) == 0 {
			return nil, fmt.Errorf("invalid remote field id")
		}

		go func(n int, id []byte, s *qcode.Select) {
			defer wg.Done()

			//st := time.Now()

			b, err := r.Fn(hdr, id)
			if err != nil {
				cerr = fmt.Errorf("%s: %s", s.Table, err)
				return
			}

			if len(r.Path) != 0 {
				b = jsn.Strip(b, r.Path)
			}

			var ob bytes.Buffer

			if len(s.Cols) != 0 {
				err = jsn.Filter(&ob, b, colsToList(s.Cols))
				if err != nil {
					cerr = fmt.Errorf("%s: %s", s.Table, err)
					return
				}

			} else {
				ob.WriteString("null")
			}

			to[n] = jsn.Field{Key: []byte(s.FieldName), Value: ob.Bytes()}
		}(i, id, s)
	}
	wg.Wait()

	return to, cerr
}

func (sg *SuperGraph) parentFieldIds(h *maphash.Hash, sel []qcode.Select, remotes int) (
	[][]byte, map[uint64]*qcode.Select, error) {

	// list of keys (and it's related value) to extract from
	// the db json response
	fm := make([][]byte, 0, remotes)

	// mapping between the above extracted key and a Select
	// object
	sm := make(map[uint64]*qcode.Select, remotes)

	for i := range sel {
		s := &sel[i]

		if s.SkipRender != qcode.SkipTypeRemote {
			continue
		}

		p := sel[s.ParentID]

		pti, err := sg.schema.GetTableInfo(p.Table)
		if err != nil {
			return nil, nil, err
		}

		k := mkkey(h, s.Table, pti.Name)

		if r, ok := sg.rmap[k]; ok {
			fm = append(fm, r.IDField)
			_, _ = h.Write(r.IDField)
			sm[h.Sum64()] = s
			h.Reset()
		}
	}

	return fm, sm, nil
}

func colsToList(cols []qcode.Column) []string {
	var f []string

	for _, col := range cols {
		f = append(f, col.Col.Name)
	}
	return f
}
