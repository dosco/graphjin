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
	"time"

	"github.com/dosco/graphjin/core/internal/graph"
	"github.com/dosco/graphjin/core/internal/qcode"
	"github.com/rs/xid"
)

const (
	maxMembersPerWorker = 2000
	errSubs             = "subscription: %s: %s"
)

var (
	minPollDuration = (200 * time.Millisecond)
)

type sub struct {
	name string
	role string
	qc   *queryComp
	js   json.RawMessage

	add  chan *Member
	del  chan *Member
	updt chan mmsg

	mval
	sync.Once
}

type mval struct {
	params []json.RawMessage
	mi     []minfo
	res    []chan *Result
	ids    []xid.ID
}

type minfo struct {
	dh     [sha256.Size]byte
	values []interface{}
	// index of cursor value in the arguments array
	cindx int
}

type mmsg struct {
	id     xid.ID
	dh     [sha256.Size]byte
	cursor string
}

type Member struct {
	params json.RawMessage
	sub    *sub
	Result chan *Result
	done   bool
	id     xid.ID
	vl     []interface{}
	mm     mmsg
	// index of cursor value in the arguments array
	cindx int
}

// GraphQLEx is the extended version of the Subscribe function allowing for request specific config.
func (g *GraphJin) Subscribe(
	c context.Context,
	query string,
	vars json.RawMessage,
	rc *ReqConfig) (*Member, error) {
	var err error

	gj := g.Load().(*graphjin)

	h, err := graph.FastParse(query)
	if err != nil {
		panic(err)
	}
	op := qcode.GetQTypeByName(h.Operation)
	name := h.Name

	if op != qcode.QTSubscription {
		return nil, errors.New("subscription: not a subscription query")
	}

	if name == "" {
		if gj.prod {
			return nil, errors.New("subscription: query name is required")
		} else {
			h := sha256.Sum256([]byte(query))
			name = hex.EncodeToString(h[:])
		}
	}

	var role string

	if v, ok := c.Value(UserRoleKey).(string); ok {
		role = v
	} else {
		switch c.Value(UserIDKey).(type) {
		case string, int:
			role = "user"
		default:
			role = "anon"
		}
	}

	if role == "user" && gj.abacEnabled {
		if role, err = gj.executeRoleQuery(c, nil, vars, gj.pf, rc); err != nil {
			return nil, err
		}
	}

	v, _ := gj.subs.LoadOrStore((name + role), &sub{
		name: name,
		role: role,
		add:  make(chan *Member),
		del:  make(chan *Member),
		updt: make(chan mmsg, 10),
	})
	s := v.(*sub)

	s.Do(func() {
		err = gj.newSub(c, s, query, vars, rc)
	})

	if err != nil {
		gj.subs.Delete((name + role))
		return nil, err
	}

	args, err := gj.argList(c, s.qc.st.md, vars, gj.pf, rc)
	if err != nil {
		return nil, err
	}

	var params json.RawMessage

	if len(args.values) != 0 {
		if params, err = json.Marshal(args.values); err != nil {
			return nil, err
		}
	}

	m := &Member{
		id:     xid.New(),
		Result: make(chan *Result, 10),
		sub:    s,
		vl:     args.values,
		params: params,
		cindx:  args.cindx,
	}

	m.mm, err = gj.subFirstQuery(s, m, params)
	if err != nil {
		return nil, err
	}
	s.add <- m

	return m, nil
}

func (gj *graphjin) newSub(c context.Context,
	s *sub, query string, vars json.RawMessage, rc *ReqConfig) error {
	var err error

	qr := queryReq{
		ns:    gj.namespace,
		op:    qcode.QTSubscription,
		name:  s.name,
		query: []byte(query),
		vars:  vars,
	}

	if rc != nil && rc.ns != nil {
		qr.ns = *rc.ns
	}

	if s.qc, err = gj.compileQuery(qr, s.role); err != nil {
		return err
	}

	if !gj.prod && !gj.conf.DisableAllowList {
		err := gj.allowList.Set(
			nil,
			query,
			qr.ns)

		if err != nil {
			return err
		}
	}

	if len(s.qc.st.md.Params()) != 0 {
		s.qc.st.sql = renderSubWrap(s.qc.st, gj.schema.DBType())
	}

	go gj.subController(s)
	return nil
}

func (gj *graphjin) subController(s *sub) {
	defer gj.subs.Delete((s.name + s.role))

	ps := gj.conf.SubsPollDuration
	if ps < minPollDuration {
		ps = minPollDuration
	}

	for {
		select {
		case m := <-s.add:
			if err := s.addMember(m); err != nil {
				gj.log.Printf(errSubs, "add-sub", err)
				return
			}

		case m := <-s.del:
			s.deleteMember(m)
			if len(s.ids) == 0 {
				return
			}

		case msg := <-s.updt:
			if err := s.updateMember(msg); err != nil {
				gj.log.Printf(errSubs, "update-sub", err)
				return
			}

		case <-time.After(ps):
			s.fanOutJobs(gj)
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

func (gj *graphjin) subCheckUpdates(s *sub, mv mval, start int) {
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

	hasParams := len(s.qc.st.md.Params()) != 0

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

	err = retryOperation(c, func() error {
		if hasParams {
			//nolint: sqlclosecheck
			rows, err = gj.db.QueryContext(c, s.qc.st.sql, params)
		} else {
			//nolint: sqlclosecheck
			rows, err = gj.db.QueryContext(c, s.qc.st.sql)
		}

		return err
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
			gj.subNotifyMember(s, mv, j, js)
			continue
		}

		for k := start; k < end; k++ {
			gj.subNotifyMember(s, mv, k, js)
		}
		s.js = js
	}
}

func (gj *graphjin) subFirstQuery(s *sub, m *Member, params json.RawMessage) (mmsg, error) {
	c := context.Background()

	// when params are not available we use a more optimized
	// codepath that does not use a join query
	// more details on this optimization are towards the end
	// of the function
	var js json.RawMessage
	var mm mmsg
	var err error

	if s.js != nil {
		js = s.js

	} else {
		err := retryOperation(c, func() error {
			switch {
			case params != nil:
				err = gj.db.
					QueryRowContext(c, s.qc.st.sql, renderJSONArray([]json.RawMessage{params})).
					Scan(&js)
			default:
				err = gj.db.
					QueryRowContext(c, s.qc.st.sql).
					Scan(&js)
			}
			return err
		})

		if err != nil {
			return mm, fmt.Errorf(errSubs, "scan", err)
		}
	}

	mm, err = gj.subNotifyMemberEx(s,
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

func (gj *graphjin) subNotifyMemberEx(s *sub,
	dh [32]byte, cindx int, id xid.ID, rc chan *Result, js json.RawMessage, update bool) (mmsg, error) {
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
		s.updt <- mm
	}

	res := &Result{
		op:   qcode.QTQuery,
		name: s.name,
		sql:  s.qc.st.sql,
		role: s.qc.st.role.Name,
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

func (s *sub) findByID(id xid.ID) (int, bool) {
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

func (m *Member) String() string {
	return m.id.String()
}
