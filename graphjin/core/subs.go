package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dosco/graphjin/core/v3/internal/allow"
	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

const (
	maxMembersPerWorker = 2000
	errSubs             = "subscription: %s: %s"
)

var minPollDuration = (200 * time.Millisecond)

type sub struct {
	k  string
	s  gstate
	js json.RawMessage

	idgen uint64
	add   chan *Member
	del   chan *Member
	updt  chan mmsg

	mval
	sync.Once
}

type mval struct {
	params []json.RawMessage
	mi     []minfo
	res    []chan *Result
	ids    []uint64
}

type minfo struct {
	dh     [sha256.Size]byte
	values []interface{}
	// index of cursor value in the arguments array
	cindx int
}

type mmsg struct {
	id     uint64
	dh     [sha256.Size]byte
	cursor string
}

type Member struct {
	ns     string
	params json.RawMessage
	sub    *sub
	Result chan *Result
	done   bool
	id     uint64
	vl     []interface{}
	mm     mmsg
	// index of cursor value in the arguments array
	cindx int
}

// Subscribe function is called on the GraphJin struct to subscribe to query.
// Any database changes that apply to the query are streamed back in realtime.
//
// In developer mode all named queries are saved into the queries folder and in production mode only
// queries from these saved queries can be used.
func (g *GraphJin) Subscribe(
	c context.Context,
	query string,
	vars json.RawMessage,
	rc *ReqConfig,
) (m *Member, err error) {
	// get the name, query vars
	h, err := graph.FastParse(query)
	if err != nil {
		return
	}

	gj := g.Load().(*graphjin)

	// create the request object
	r := gj.newGraphqlReq(rc, "subscription", h.Name, nil, vars)

	// if production security enabled then get query and metadata
	// from allow list
	if gj.prodSec {
		var item allow.Item
		item, err = gj.allowList.GetByName(h.Name, true)
		if err != nil {
			return
		}
		r.Set(item)
	} else {
		r.query = []byte(query)
	}

	m, err = gj.subscribe(c, r)
	return
}

// SubscribeByName is similar to the Subscribe function except that queries saved
// in the queries folder can directly be used by their filename.
func (g *GraphJin) SubscribeByName(
	c context.Context,
	name string,
	vars json.RawMessage,
	rc *ReqConfig,
) (m *Member, err error) {
	gj := g.Load().(*graphjin)

	item, err := gj.allowList.GetByName(name, gj.prod)
	if err != nil {
		return
	}
	r := gj.newGraphqlReq(rc, "subscription", name, nil, vars)
	r.Set(item)

	m, err = gj.subscribe(c, r)
	return
}

func (gj *graphjin) subscribe(c context.Context, r graphqlReq) (
	m *Member, err error,
) {
	if r.op != qcode.QTSubscription {
		return nil, errors.New("subscription: not a subscription query")
	}

	// transactions not supported with subscriptions
	if r.rc != nil && r.rc.Tx != nil {
		return nil, errors.New("subscription: database transactions not supported")
	}

	if r.name == "" {
		h := sha256.Sum256([]byte(r.query))
		r.name = hex.EncodeToString(h[:])
	}

	s, err := newGState(c, gj, r)
	if err != nil {
		return
	}

	if s.role == "user" && gj.abacEnabled {
		if err = s.executeRoleQuery(c, nil); err != nil {
			return
		}
	}

	k := s.key()
	v, _ := gj.subs.LoadOrStore(k, &sub{
		k:    k,
		s:    s,
		add:  make(chan *Member),
		del:  make(chan *Member),
		updt: make(chan mmsg, 10),
	})
	sub := v.(*sub)

	sub.Do(func() {
		err = gj.initSub(c, sub)
	})

	if err != nil {
		gj.subs.Delete(k)
		return
	}

	// don't use the vmap in the sub gstate use the new
	// one that was created this current subscription
	args, err := sub.s.argListForSub(c, s.vmap)
	if err != nil {
		return nil, err
	}

	m = &Member{
		ns:     r.ns,
		id:     atomic.AddUint64(&sub.idgen, 1),
		Result: make(chan *Result, 10),
		sub:    sub,
		vl:     args.values,
		params: args.json,
		cindx:  args.cindx,
	}

	m.mm, err = gj.subFirstQuery(sub, m)
	if err != nil {
		return nil, err
	}
	sub.add <- m
	return
}

func (gj *graphjin) initSub(c context.Context, sub *sub) (err error) {
	if err = sub.s.compile(); err != nil {
		return
	}

	if !gj.prod {
		err = gj.saveToAllowList(sub.s.cs.st.qc, sub.s.r.ns)
		if err != nil {
			return
		}
	}

	if len(sub.s.cs.st.md.Params()) != 0 {
		sub.s.cs.st.sql = renderSubWrap(sub.s.cs.st, gj.schema.DBType())
	}

	go gj.subController(sub)
	return
}

func (gj *graphjin) subController(sub *sub) {
	// remove subscription if controller exists
	defer gj.subs.Delete(sub.k)

	ps := gj.conf.SubsPollDuration
	if ps < minPollDuration {
		ps = minPollDuration
	}

	for {
		select {
		case m := <-sub.add:
			if err := sub.addMember(m); err != nil {
				gj.log.Printf(errSubs, "add-sub", err)
				return
			}

		case m := <-sub.del:
			sub.deleteMember(m)
			if len(sub.ids) == 0 {
				return
			}

		case msg := <-sub.updt:
			if err := sub.updateMember(msg); err != nil {
				gj.log.Printf(errSubs, "update-sub", err)
				return
			}

		case <-time.After(ps):
			sub.fanOutJobs(gj)

		case <-gj.done:
			return
		}
	}
}

func (s *sub) addMember(m *Member) error {
	mi := minfo{cindx: m.cindx}
	if mi.cindx != -1 {
		mi.values = m.vl
	}
	mi.dh = m.mm.dh

	// if cindex is not -1 then this query contains
	// a cursor that must be updated with the new
	// cursor value so subscriptions can paginate.
	if mi.cindx != -1 && m.mm.cursor != "" {
		mi.values[mi.cindx] = m.mm.cursor

		// values is a pre-generated json value that
		// must be re-created.
		if v, err := json.Marshal(mi.values); err != nil {
			return nil
		} else {
			m.params = v
		}
	}

	s.params = append(s.params, m.params)
	s.mi = append(s.mi, mi)
	s.res = append(s.res, m.Result)
	s.ids = append(s.ids, m.id)

	return nil
}

func (s *sub) deleteMember(m *Member) {
	i, ok := s.findByID(m.id)
	if !ok {
		return
	}

	s.params[i] = s.params[len(s.params)-1]
	s.params = s.params[:len(s.params)-1]

	s.mi[i] = s.mi[len(s.mi)-1]
	s.mi = s.mi[:len(s.mi)-1]

	s.res[i] = s.res[len(s.res)-1]
	s.res = s.res[:len(s.res)-1]

	s.ids[i] = s.ids[len(s.ids)-1]
	s.ids = s.ids[:len(s.ids)-1]
}

func (s *sub) updateMember(msg mmsg) error {
	i, ok := s.findByID(msg.id)
	if !ok {
		return nil
	}

	s.mi[i].dh = msg.dh

	// if cindex is not -1 then this query contains
	// a cursor that must be updated with the new
	// cursor value so subscriptions can paginate.
	if s.mi[i].cindx != -1 && msg.cursor != "" {
		s.mi[i].values[s.mi[i].cindx] = msg.cursor

		// values is a pre-generated json value that
		// must be re-created.
		if v, err := json.Marshal(s.mi[i].values); err != nil {
			return nil
		} else {
			s.params[i] = v
		}
	}
	return nil
}

func (s *sub) fanOutJobs(gj *graphjin) {
	switch {
	case len(s.ids) == 0:
		return

	case len(s.ids) <= maxMembersPerWorker:
		go gj.subCheckUpdates(s, s.mval, 0)

	default:
		// fan out chunks of work to multiple routines
		// separated by a random duration
		for i := 0; i < len(s.ids); i += maxMembersPerWorker {
			gj.subCheckUpdates(s, s.mval, i)
		}
	}
}

func (gj *graphjin) subCheckUpdates(sub *sub, mv mval, start int) {
	// Do not use the `mval` embedded inside sub since
	// its not thread safe use the copy `mv mval`.

	// random wait to prevent multiple queries hitting the db
	// at the same time.
	// ps := gj.conf.SubsPollDuration
	// if ps < minPollDuration {
	// 	ps = minPollDuration
	// }

	// rt := rand.Int63n(ps.Milliseconds()) // #nosec F404
	// time.Sleep(time.Duration(rt) * time.Millisecond)

	end := start + maxMembersPerWorker
	if len(mv.ids) < end {
		end = start + (len(mv.ids) - start)
	}

	hasParams := len(sub.s.cs.st.md.Params()) != 0

	var rows *sql.Rows
	var err error

	c := context.Background()

	// when params are not available we use a more optimized
	// codepath that does not use a join query
	// more details on this optimization are towards the end
	// of the function

	var params json.RawMessage

	if hasParams {
		params = renderJSONArray(mv.params[start:end])
	}

	err = retryOperation(c, func() (err1 error) {
		if hasParams {
			//nolint: sqlclosecheck
			rows, err1 = gj.db.QueryContext(c, sub.s.cs.st.sql, params)
		} else {
			//nolint: sqlclosecheck
			rows, err1 = gj.db.QueryContext(c, sub.s.cs.st.sql)
		}
		return
	})

	if err != nil {
		gj.log.Printf(errSubs, "query", err)
		return
	}
	defer rows.Close()

	var js json.RawMessage

	i := 0
	for rows.Next() {
		if err := rows.Scan(&js); err != nil {
			gj.log.Printf(errSubs, "scan", err)
			return
		}

		j := start + i
		i++

		if hasParams {
			gj.subNotifyMember(sub, mv, j, js)
			continue
		}

		for k := start; k < end; k++ {
			gj.subNotifyMember(sub, mv, k, js)
		}
		sub.js = js
	}
}

func (gj *graphjin) subFirstQuery(sub *sub, m *Member) (mmsg, error) {
	c := context.Background()

	// when params are not available we use a more optimized
	// codepath that does not use a join query
	// more details on this optimization are towards the end
	// of the function
	var js json.RawMessage
	var mm mmsg
	var err error

	if sub.js != nil {
		js = sub.js
	} else {
		err := retryOperation(c, func() error {
			var row *sql.Row
			q := sub.s.cs.st.sql

			if m.params != nil {
				row = gj.db.QueryRowContext(c, q,
					renderJSONArray([]json.RawMessage{m.params}))
			} else {
				row = gj.db.QueryRowContext(c, q)
			}
			return row.Scan(&js)
		})
		if err != nil {
			return mm, fmt.Errorf(errSubs, "scan", err)
		}
	}

	mm, err = gj.subNotifyMemberEx(sub,
		[32]byte{},
		m.cindx,
		m.id,
		m.Result, js, false)

	return mm, err
}

func (gj *graphjin) subNotifyMember(s *sub, mv mval, j int, js json.RawMessage) {
	_, err := gj.subNotifyMemberEx(s,
		mv.mi[j].dh,
		mv.mi[j].cindx,
		mv.ids[j],
		mv.res[j], js, true)
	if err != nil {
		gj.log.Print(err.Error())
	}
}

func (gj *graphjin) subNotifyMemberEx(sub *sub,
	dh [32]byte, cindx int, id uint64, rc chan *Result, js json.RawMessage, update bool,
) (mmsg, error) {
	mm := mmsg{id: id}

	mm.dh = sha256.Sum256(js)
	if dh == mm.dh {
		return mm, nil
	}

	nonce := mm.dh

	if cv := firstCursorValue(js, gj.pf); len(cv) != 0 {
		mm.cursor = string(cv)
	}

	ejs, err := encryptValues(js,
		gj.pf,
		decPrefix,
		nonce[:],
		gj.encKey)
	if err != nil {
		return mm, fmt.Errorf(errSubs, "cursor", err)
	}

	// we're expecting a cursor but the cursor was null
	// so we skip this one.
	if cindx != -1 && mm.cursor == "" {
		return mm, nil
	}

	if update {
		sub.updt <- mm
	}

	res := &Result{
		op:   qcode.QTQuery,
		name: sub.s.r.name,
		sql:  sub.s.cs.st.sql,
		role: sub.s.cs.st.role,
		Data: ejs,
	}

	// if parameters exists then each response is unique
	// so each channel should be notified only with it's own
	// result value
	select {
	case rc <- res:
	case <-time.After(250 * time.Millisecond):
	}

	return mm, nil
}

func renderSubWrap(st stmt, ct string) string {
	var w strings.Builder

	switch ct {
	case "mysql":
		w.WriteString(`WITH _gj_sub AS (SELECT * FROM JSON_TABLE(?, "$[*]" COLUMNS(`)
		for i, p := range st.md.Params() {
			if i != 0 {
				w.WriteString(`, `)
			}
			w.WriteString("`" + p.Name + "` ")
			w.WriteString(p.Type)
			w.WriteString(` PATH "$[`)
			w.WriteString(strconv.FormatInt(int64(i), 10))
			w.WriteString(`]" ERROR ON ERROR`)
		}
		w.WriteString(`)) AS _gj_jt`)

	default:
		w.WriteString(`WITH _gj_sub AS (SELECT `)
		for i, p := range st.md.Params() {
			if i != 0 {
				w.WriteString(`, `)
			}
			w.WriteString(`CAST(x->>`)
			w.WriteString(strconv.FormatInt(int64(i), 10))
			w.WriteString(` AS `)
			w.WriteString(p.Type)
			w.WriteString(`) AS "` + p.Name + `"`)
		}
		w.WriteString(` FROM json_array_elements($1::json) AS x`)
	}
	w.WriteString(`) SELECT _gj_sub_data.__root FROM _gj_sub LEFT OUTER JOIN LATERAL (`)
	w.WriteString(st.sql)
	w.WriteString(`) AS _gj_sub_data ON true`)

	return w.String()
}

func renderJSONArray(v []json.RawMessage) json.RawMessage {
	w := bytes.Buffer{}
	w.WriteRune('[')
	for i := range v {
		if i != 0 {
			w.WriteRune(',')
		}
		w.Write(v[i])
	}
	w.WriteRune(']')
	return json.RawMessage(w.Bytes())
}

func (s *sub) findByID(id uint64) (int, bool) {
	for i := range s.ids {
		if s.ids[i] == id {
			return i, true
		}
	}
	return 0, false
}

func (m *Member) Unsubscribe() {
	if m != nil && !m.done {
		m.sub.del <- m
		m.done = true
	}
}

func (m *Member) ID() uint64 {
	return m.id
}

func (m *Member) String() string {
	return strconv.Itoa(int(m.id))
}
