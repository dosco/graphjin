package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dosco/graphjin/core/internal/qcode"
	"github.com/rs/xid"
)

const (
	maxMembersPerWorker = 2000
)

type sub struct {
	name string
	role string
	qc   *queryComp

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
	sub    *sub
	Result chan *Result
	done   bool
	id     xid.ID
	vl     []interface{}
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
	op, name := qcode.GetQType(query)

	if op != qcode.QTSubscription {
		return nil, errors.New("subscription: not a subscription query")
	}

	if name == "" {
		if gj.allowList != nil && gj.prod {
			return nil, errors.New("subscription: query name is required")
		} else {
			h := sha256.Sum256([]byte(query))
			name = base64.StdEncoding.EncodeToString(h[:])
		}
	}

	var role string

	if v, ok := c.Value(UserRoleKey).(string); ok {
		role = v
	} else if c.Value(UserIDKey) != nil {
		role = "user"
	} else {
		role = "anon"
	}

	if role == "user" && gj.abacEnabled {
		if role, err = gj.executeRoleQuery(c, nil, gj.roleStmtMD, vars, rc); err != nil {
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
		err = gj.newSub(c, s, query, vars)
	})

	if err != nil {
		gj.subs.Delete((name + role))
		return nil, err
	}

	args, err := gj.argList(c, s.qc.st.md, vars, rc)
	if err != nil {
		return nil, err
	}

	m := &Member{
		Result: make(chan *Result, 10),
		sub:    s,
		vl:     args.values,
		cindx:  args.cindx,
	}
	s.add <- m

	return m, nil
}

func (gj *graphjin) newSub(c context.Context, s *sub, query string, vars json.RawMessage) error {
	var err error

	qr := queryReq{
		op:    qcode.QTSubscription,
		name:  s.name,
		query: []byte(query),
		vars:  vars,
	}

	if s.qc, err = gj.compileQuery(qr, s.role); err != nil {
		return err
	}

	if gj.allowList != nil && !gj.prod {
		if err := gj.allowList.Set(vars, query, s.qc.st.qc.Metadata); err != nil {
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
	var ps time.Duration

	if gj.conf.SubsPollDuration < 5 {
		ps = 5 * time.Second
	} else {
		ps = gj.conf.SubsPollDuration * time.Second
	}

	for {
		select {
		case m := <-s.add:
			if err := s.addMember(m); err != nil {
				gj.log.Printf("Subscription Error: %s", err)
				return
			}

		case m := <-s.del:
			s.deleteMember(m)
			if len(s.ids) == 0 {
				return
			}

		case msg := <-s.updt:
			if err := s.updateMember(msg); err != nil {
				gj.log.Printf("Subscription Error: %s", err)
				return
			}

		case <-time.After(ps):
			s.fanOutJobs(gj)
		}
	}
}

func (s *sub) addMember(m *Member) error {
	v, err := json.Marshal(m.vl)
	if err != nil {
		return err
	}

	m.id = xid.New()
	mi := minfo{cindx: m.cindx}
	if mi.cindx != -1 {
		mi.values = m.vl
	}

	s.params = append(s.params, v)
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
		go gj.checkUpdates(s, s.mval, 0)

	default:
		// fan out chunks of work to multiple routines
		// seperated by a random duration
		for i := 0; i < len(s.ids); i += maxMembersPerWorker {
			go gj.checkUpdates(s, s.mval, i)
		}
	}
}

func (gj *graphjin) checkUpdates(s *sub, mv mval, start int) {
	// Do not use the `mval` embedded inside sub since
	// its not thread safe use the copy `mv mval`.

	// random wait to prevent multiple queries hitting the db
	// at the same time.
	time.Sleep(time.Duration(rand.Int63n(500)) * time.Millisecond)

	end := start + maxMembersPerWorker
	if len(mv.ids) < end {
		end = start + (len(mv.ids) - start)
	}

	var rows *sql.Rows
	var err error

	hasParams := len(s.qc.st.md.Params()) != 0
	c := context.Background()

	// when params are not available we use a more optimized
	// codepath that does not use a join query
	// more details on this optimization are towards the end
	// of the function
	if hasParams {
		rows, err = gj.db.QueryContext(c, s.qc.st.sql, renderJSONArray(mv.params[start:end]))
	} else {
		rows, err = gj.db.QueryContext(c, s.qc.st.sql)
	}

	if err != nil {
		gj.log.Printf("Subscription Error: %s", err)
		return
	}

	var js json.RawMessage
	i := 0

	for rows.Next() {
		if err := rows.Scan(&js); err != nil {
			gj.log.Printf("Subscription Error: %s", err)
			return
		}
		j := start + i
		i++

		newDH := sha256.Sum256(js)
		if mv.mi[j].dh == newDH {
			continue
		}

		cur, err := gj.encryptCursor(s.qc.st.qc, js)
		if err != nil {
			gj.log.Printf("Subscription Error: %s", err)
			return
		}

		// we're expecting a cursor but the cursor was null
		// so we skip this one.
		if mv.mi[j].cindx != -1 && cur.value == "" {
			continue
		}

		s.updt <- mmsg{id: mv.ids[j], dh: newDH, cursor: cur.value}

		res := &Result{
			op:   qcode.QTQuery,
			name: s.name,
			sql:  s.qc.st.sql,
			role: s.qc.st.role.Name,
			Data: cur.data,
		}

		// if parameters exists then each response is unique
		// so each channel should be notified only with it's own
		// result value
		if hasParams {
			select {
			case mv.res[j] <- res:
			case <-time.After(250 * time.Millisecond):
			}
		} else {

			// if no params exist then it means we are not using
			// the joined query so we are expecting only a single
			// result, so we can optimize here by notifying
			// all channels since there will only be one result
			for k := start; k < end; k++ {
				select {
				case mv.res[k] <- res:
				case <-time.After(250 * time.Millisecond):
				}
			}
		}
	}
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
			w.WriteString("`" + p.Name + "`")
			w.WriteString(` INT PATH "$[`)
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
			w.WriteString(`) as `)
			w.WriteString(p.Name)
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
