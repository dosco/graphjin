package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/dosco/graphjin/core/v3/internal/jsn"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

func (s *gstate) execRemoteJoin(c context.Context) (err error) {
	// fetch the field name used within the db response json
	// that are used to mark insertion points and the mapping between
	// those field names and their select objects
	fids, sfmap, err := s.parentFieldIds()
	if err != nil {
		return
	}

	// fetch the field values of the marked insertion points
	// these values contain the id to be used with fetching remote data
	from := jsn.Get(s.data, fids)
	if len(from) == 0 {
		err = errors.New("something wrong no remote ids found in db response")
		return
	}

	to, err := s.resolveRemotes(c, from, sfmap)
	if err != nil {
		return
	}

	var ob bytes.Buffer
	if err = jsn.Replace(&ob, s.data, from, to); err != nil {
		return
	}
	s.data = ob.Bytes()
	return
}

func (s *gstate) resolveRemotes(
	ctx context.Context,
	from []jsn.Field,
	sfmap map[string]*qcode.Select,
) ([]jsn.Field, error) {
	selects := s.cs.st.qc.Selects

	// replacement data for the marked insertion points
	// key and value will be replaced by whats below
	to := make([]jsn.Field, len(from))

	var wg sync.WaitGroup
	wg.Add(len(from))

	var cerr error

	for i, id := range from {
		// use the json key to find the related Select object
		sel, ok := sfmap[string(id.Key)]
		if !ok {
			return nil, fmt.Errorf("invalid remote field key")
		}
		p := selects[sel.ParentID]

		// then use the Table name in the Select and it's parent
		// to find the resolver to use for this relationship
		r, ok := s.gj.rmap[(sel.Table + p.Table)]
		if !ok {
			return nil, fmt.Errorf("no resolver found")
		}

		id := jsn.Value(id.Value)
		if len(id) == 0 {
			return nil, fmt.Errorf("invalid remote field id")
		}

		go func(n int, id []byte, sel *qcode.Select) {
			defer wg.Done()

			// st := time.Now()

			ctx1, span := s.gj.spanStart(ctx, "Execute Remote Request")

			b, err := r.Fn.Resolve(ctx1, ResolverReq{
				ID: string(id), Sel: sel, Log: s.gj.log, ReqConfig: s.r.rc,
			})
			if err != nil {
				cerr = fmt.Errorf("%s: %s", sel.Table, err)
				span.Error(cerr)
			}
			span.End()

			if err != nil {
				return
			}

			if len(r.Path) != 0 {
				b = jsn.Strip(b, r.Path)
			}

			var ob bytes.Buffer

			if len(sel.Fields) != 0 {
				err = jsn.Filter(&ob, b, fieldsToList(sel.Fields))
				if err != nil {
					cerr = fmt.Errorf("%s: %w", sel.Table, err)
					return
				}

			} else {
				ob.WriteString("null")
			}

			to[n] = jsn.Field{Key: []byte(sel.FieldName), Value: ob.Bytes()}
		}(i, id, sel)
	}
	wg.Wait()
	return to, cerr
}

func (s *gstate) parentFieldIds() ([][]byte, map[string]*qcode.Select, error) {
	selects := s.cs.st.qc.Selects
	remotes := s.cs.st.qc.Remotes

	// list of keys (and it's related value) to extract from
	// the db json response
	fm := make([][]byte, 0, remotes)

	// mapping between the above extracted key and a Select
	// object
	sm := make(map[string]*qcode.Select, remotes)

	for i, sel := range selects {
		if sel.SkipRender != qcode.SkipTypeRemote {
			continue
		}

		p := selects[sel.ParentID]

		if r, ok := s.gj.rmap[(sel.Table + p.Table)]; ok {
			fm = append(fm, r.IDField)
			sm[string(r.IDField)] = &selects[i]
		}
	}
	return fm, sm, nil
}

func fieldsToList(fields []qcode.Field) []string {
	var f []string

	for _, col := range fields {
		f = append(f, col.FieldName)
	}
	return f
}
