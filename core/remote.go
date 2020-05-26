package core

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/cespare/xxhash/v2"
	"github.com/dosco/super-graph/core/internal/qcode"
	"github.com/dosco/super-graph/jsn"
)

func (sg *SuperGraph) execRemoteJoin(st *stmt, data []byte, hdr http.Header) ([]byte, error) {
	var err error

	sel := st.qc.Selects
	h := xxhash.New()

	// fetch the field name used within the db response json
	// that are used to mark insertion points and the mapping between
	// those field names and their select objects
	fids, sfmap := sg.parentFieldIds(h, sel, st.md.Skipped)

	// fetch the field values of the marked insertion points
	// these values contain the id to be used with fetching remote data
	from := jsn.Get(data, fids)
	var to []jsn.Field

	switch {
	case len(from) == 1:
		to, err = sg.resolveRemote(hdr, h, from[0], sel, sfmap)

	case len(from) > 1:
		to, err = sg.resolveRemotes(hdr, h, from, sel, sfmap)

	default:
		return nil, errors.New("something wrong no remote ids found in db response")
	}

	if err != nil {
		return nil, err
	}

	var ob bytes.Buffer

	err = jsn.Replace(&ob, data, from, to)
	if err != nil {
		return nil, err
	}

	return ob.Bytes(), nil
}

func (sg *SuperGraph) resolveRemote(
	hdr http.Header,
	h *xxhash.Digest,
	field jsn.Field,
	sel []qcode.Select,
	sfmap map[uint64]*qcode.Select) ([]jsn.Field, error) {

	// replacement data for the marked insertion points
	// key and value will be replaced by whats below
	toA := [1]jsn.Field{}
	to := toA[:1]

	// use the json key to find the related Select object
	k1 := xxhash.Sum64(field.Key)

	s, ok := sfmap[k1]
	if !ok {
		return nil, nil
	}
	p := sel[s.ParentID]

	// then use the Table nme in the Select and it's parent
	// to find the resolver to use for this relationship
	k2 := mkkey(h, s.Name, p.Name)

	r, ok := sg.rmap[k2]
	if !ok {
		return nil, nil
	}

	id := jsn.Value(field.Value)
	if len(id) == 0 {
		return nil, nil
	}

	//st := time.Now()

	b, err := r.Fn(hdr, id)
	if err != nil {
		return nil, err
	}

	if len(r.Path) != 0 {
		b = jsn.Strip(b, r.Path)
	}

	var ob bytes.Buffer

	if len(s.Cols) != 0 {
		err = jsn.Filter(&ob, b, colsToList(s.Cols))
		if err != nil {
			return nil, err
		}

	} else {
		ob.WriteString("null")
	}

	to[0] = jsn.Field{Key: []byte(s.FieldName), Value: ob.Bytes()}
	return to, nil
}

func (sg *SuperGraph) resolveRemotes(
	hdr http.Header,
	h *xxhash.Digest,
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
		k1 := xxhash.Sum64(id.Key)

		s, ok := sfmap[k1]
		if !ok {
			return nil, nil
		}
		p := sel[s.ParentID]

		// then use the Table nme in the Select and it's parent
		// to find the resolver to use for this relationship
		k2 := mkkey(h, s.Name, p.Name)

		r, ok := sg.rmap[k2]
		if !ok {
			return nil, nil
		}

		id := jsn.Value(id.Value)
		if len(id) == 0 {
			return nil, nil
		}

		go func(n int, id []byte, s *qcode.Select) {
			defer wg.Done()

			//st := time.Now()

			b, err := r.Fn(hdr, id)
			if err != nil {
				cerr = fmt.Errorf("%s: %s", s.Name, err)
				return
			}

			if len(r.Path) != 0 {
				b = jsn.Strip(b, r.Path)
			}

			var ob bytes.Buffer

			if len(s.Cols) != 0 {
				err = jsn.Filter(&ob, b, colsToList(s.Cols))
				if err != nil {
					cerr = fmt.Errorf("%s: %s", s.Name, err)
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

func (sg *SuperGraph) parentFieldIds(h *xxhash.Digest, sel []qcode.Select, skipped uint32) (
	[][]byte,
	map[uint64]*qcode.Select) {

	c := 0
	for i := range sel {
		s := &sel[i]
		if isSkipped(skipped, uint32(s.ID)) {
			c++
		}
	}

	// list of keys (and it's related value) to extract from
	// the db json response
	fm := make([][]byte, c)

	// mapping between the above extracted key and a Select
	// object
	sm := make(map[uint64]*qcode.Select, c)
	n := 0

	for i := range sel {
		s := &sel[i]

		if !isSkipped(skipped, uint32(s.ID)) {
			continue
		}

		p := sel[s.ParentID]
		k := mkkey(h, s.Name, p.Name)

		if r, ok := sg.rmap[k]; ok {
			fm[n] = r.IDField
			n++

			k := xxhash.Sum64(r.IDField)
			sm[k] = s
		}
	}

	return fm, sm
}

func isSkipped(n uint32, pos uint32) bool {
	return ((n & (1 << pos)) != 0)
}

func colsToList(cols []qcode.Column) []string {
	var f []string

	for i := range cols {
		f = append(f, cols[i].Name)
	}
	return f
}
