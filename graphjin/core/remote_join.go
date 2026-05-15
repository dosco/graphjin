package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/jsn"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// execRemoteJoin fetches remote data for the marked insertion points
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

	to, extra, err := s.resolveRemotes(c, from, sfmap, s.requestedRootCursorFields())
	if err != nil {
		return
	}

	var ob bytes.Buffer
	if err = jsn.Replace(&ob, s.data, from, to); err != nil {
		return
	}
	s.data = ob.Bytes()
	if len(extra) != 0 {
		if s.data, err = mergeRemoteRootFields(s.data, extra); err != nil {
			return
		}
		s.dhash = sha256.Sum256(s.data)
		s.data, err = encryptValues(s.data, s.gj.printFormat, decPrefix, s.dhash[:], s.gj.encryptionKey)
	}
	return
}

// resolveRemotes fetches remote data for the marked insertion points
func (s *gstate) resolveRemotes(
	ctx context.Context,
	from []jsn.Field,
	sfmap map[string]*qcode.Select,
	cursorFields map[string]struct{},
) ([]jsn.Field, map[string]json.RawMessage, error) {
	selects := s.cs.st.qc.Selects

	// replacement data for the marked insertion points
	// key and value will be replaced by whats below
	to := make([]jsn.Field, len(from))
	extra := make(map[string]json.RawMessage)
	var extraMutex sync.Mutex

	var wg sync.WaitGroup
	wg.Add(len(from))

	var cerr error
	var cerrMutex sync.Mutex

	for i, id := range from {
		// use the json key to find the related Select object
		sel, ok := sfmap[string(id.Key)]
		if !ok {
			return nil, nil, fmt.Errorf("invalid remote field key")
		}

		// Lookup the resolver. Top-level remotes (ParentID == -1) are
		// keyed by table name alone; row-joins are keyed by table+parent.
		var (
			r    resItem
			rid  []byte
			rkey string
		)
		if sel.ParentID == -1 {
			rkey = sel.Table
			r, ok = s.gj.rmap[rkey]
		} else {
			p := selects[sel.ParentID]
			rkey = sel.Table + p.Table
			r, ok = s.gj.rmap[rkey]
			if ok {
				rid = jsn.Value(id.Value)
				if len(rid) == 0 {
					return nil, nil, fmt.Errorf("invalid remote field id")
				}
			}
		}
		if !ok {
			return nil, nil, fmt.Errorf("no resolver found for remote %q", rkey)
		}

		go func(n int, id []byte, sel *qcode.Select, r resItem, rkey string) {
			defer wg.Done()

			ctx1, span := s.gj.spanStart(ctx, "Execute Remote Request")
			defer span.End()

			source := r.Source
			if source == "" {
				source = "remote"
			}
			scope := r.Scope
			if scope == "" {
				scope = rkey
			}
			cursorField := sel.FieldName + "_cursor"
			_, cursorWanted := cursorFields[cursorField]
			cursorWanted = cursorWanted && sel.ParentID == -1
			cacheKey := s.remoteFragmentKey(ctx1, source, scope, r.Fingerprint, id, sel)
			cacheOpts := s.remoteFragmentCacheOptions(source, scope)
			if cursorWanted {
				cacheOpts.NoStore = true
			}
			produce := func(c context.Context) ([]byte, []RowRef, string, error) {
				b, err := r.Fn.Resolve(c, ResolverReq{
					ID: string(id), Sel: sel, Log: s.gj.log, Vars: s.vmap, RequestConfig: s.r.requestconfig,
				})
				if err != nil {
					return nil, nil, "", err
				}
				cursor := remoteCursorValue(b)

				if len(r.Path) != 0 {
					b = jsn.Strip(b, r.Path)
				}

				var ob bytes.Buffer
				if len(sel.Fields) != 0 {
					if err = jsn.Filter(&ob, b, fieldsToList(sel.Fields)); err != nil {
						return nil, nil, "", err
					}
				} else {
					ob.WriteString("null")
				}

				return ob.Bytes(), remoteFragmentRefs(source, scope, id, sel), cursor, nil
			}

			parentCtx := context.WithoutCancel(ctx)
			if !cacheOpts.NoStore {
				if cached, ok := s.fragmentCacheGet(ctx1, cacheKey, func() ([]byte, []RowRef, CacheEntryOptions, error) {
					c, cancel := context.WithTimeout(parentCtx, swrRefreshTimeout)
					defer cancel()
					data, refs, _, err := produce(c)
					return data, refs, cacheOpts, err
				}); ok {
					to[n] = jsn.Field{Key: []byte(sel.FieldName), Value: cached}
					return
				}
			}

			start := time.Now()
			b, refs, cursor, err := produce(ctx1)
			if err != nil {
				cerrMutex.Lock()
				cerr = fmt.Errorf("%s: %s", sel.Table, err)
				spanErr := cerr
				cerrMutex.Unlock()
				span.Error(spanErr)
				return
			}

			s.fragmentCacheSet(ctx1, cacheKey, b, refs, start, cacheOpts)
			to[n] = jsn.Field{Key: []byte(sel.FieldName), Value: b}
			if cursorWanted {
				extraMutex.Lock()
				if cursor == "" {
					extra[cursorField] = json.RawMessage("null")
				} else {
					extra[cursorField] = json.RawMessage(fmt.Sprintf("%q", string(s.gj.printFormat)+cursor))
				}
				extraMutex.Unlock()
			}
		}(i, rid, sel, r, rkey)
	}
	wg.Wait()
	return to, extra, cerr
}

func (s *gstate) requestedRootCursorFields() map[string]struct{} {
	op, err := graph.Parse(s.r.query)
	if err != nil {
		return nil
	}
	out := make(map[string]struct{})
	for _, f := range op.Fields {
		if f.ParentID == -1 && f.Type == graph.FieldKeyword && strings.HasSuffix(f.Name, "_cursor") {
			out[f.Name] = struct{}{}
		}
	}
	return out
}

func remoteCursorValue(b []byte) string {
	var wrapper struct {
		Cursor string `json:"cursor"`
	}
	if err := json.Unmarshal(b, &wrapper); err != nil {
		return ""
	}
	return wrapper.Cursor
}

func mergeRemoteRootFields(data []byte, extra map[string]json.RawMessage) ([]byte, error) {
	var m map[string]json.RawMessage
	if len(data) != 0 {
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
	}
	if m == nil {
		m = make(map[string]json.RawMessage, len(extra))
	}
	for k, v := range extra {
		m[k] = v
	}
	return json.Marshal(m)
}

// parentFieldIds fetches the field name used within the db response json
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

		// Top-level remote: marker keyed by the field name itself; no
		// parent row to extract an ID from.
		if sel.ParentID == -1 {
			marker := []byte(sel.FieldName)
			fm = append(fm, marker)
			sm[string(marker)] = &selects[i]
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

// fieldsToList converts a list of qcode.Field to a list of strings
func fieldsToList(fields []qcode.Field) []string {
	var f []string

	for _, col := range fields {
		f = append(f, col.FieldName)
	}
	return f
}

// injectRemoteMarkers merges marker entries for top-level remote roots
// into s.data after SQL execution. psql skips remote roots, so without
// this step a mixed query (one real-table root + one remote root)
// would leave the remote slot absent from s.data and execRemoteJoin
// would fail to find anything to replace.
func injectRemoteMarkers(data []byte, qc *qcode.QCode) []byte {
	var m map[string]json.RawMessage
	if len(data) > 0 {
		_ = json.Unmarshal(data, &m)
	}
	if m == nil {
		m = make(map[string]json.RawMessage)
	}
	added := false
	for _, rid := range qc.Roots {
		sel := &qc.Selects[rid]
		if sel.SkipRender != qcode.SkipTypeRemote || sel.ParentID != -1 {
			continue
		}
		m[sel.FieldName] = json.RawMessage(fmt.Sprintf("%q", "__remote__:"+sel.FieldName))
		added = true
	}
	if !added && len(data) > 0 {
		return data
	}
	out, err := json.Marshal(m)
	if err != nil {
		return data
	}
	return out
}

// seedRemotePlaceholders builds JSON for the all-remote case where
// executeQuery is skipped. Each top-level remote root gets a marker
// keyed by its FieldName; execRemoteJoin uses the same key for lookup.
func seedRemotePlaceholders(qc *qcode.QCode) []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	first := true
	for _, rid := range qc.Roots {
		sel := &qc.Selects[rid]
		if sel.SkipRender != qcode.SkipTypeRemote {
			continue
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		fmt.Fprintf(&b, "%q:%q", sel.FieldName, "__remote__:"+sel.FieldName)
	}
	b.WriteByte('}')
	return b.Bytes()
}
