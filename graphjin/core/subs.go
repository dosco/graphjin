package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dosco/super-graph/core/internal/qcode"
	"github.com/rs/xid"
)

const (
	maxMembersPerWorker = 2000
)

type sub struct {
	name string
	role string
	q    *cquery
	ops  int64

	add chan *Member
	del chan *Member
	udh chan dhmsg

	mval
	sync.Once
}

type mval struct {
	params []json.RawMessage
	dh     [][sha256.Size]byte
	res    []chan *Result
	ids    []xid.ID
}

type dhmsg struct {
	id xid.ID
	dh [sha256.Size]byte
}

type Member struct {
	sub    *sub
	Result chan *Result
	done   bool
	id     xid.ID
	vl     []interface{}
}

func (sg *SuperGraph) Subscribe(c context.Context, query string, vars json.RawMessage) (*Member, error) {
	var err error
	name := Name(query)
	op := qcode.GetQType(query)

	if op != qcode.QTSubscription {
		return nil, errors.New("subscription: not a subscription query")
	}

	if name == "" {
		return nil, errors.New("subscription: query name is required")
	}

	var role string

	if v := c.Value(UserIDKey); v != nil {
		role = "user"
	} else {
		role = "anon"
	}

	v, _ := sg.subs.LoadOrStore((name + role), &sub{
		name: name,
		role: role,
		add:  make(chan *Member),
		del:  make(chan *Member),
		udh:  make(chan dhmsg, 10),
	})
	s := v.(*sub)

	s.Do(func() {
		err = sg.newSub(c, s, query, vars)
	})

	if err != nil {
		sg.subs.Delete((name + role))
		return nil, err
	}

	varsList, err := sg.argList(c, s.q.st.md, vars)
	if err != nil {
		return nil, err
	}

	m := &Member{
		Result: make(chan *Result, 10),
		sub:    s,
		vl:     varsList,
	}
	s.add <- m

	return m, nil
}

func (sg *SuperGraph) newSub(c context.Context, s *sub, query string, vars json.RawMessage) error {
	rq := rquery{
		op:    qcode.QTSubscription,
		name:  s.name,
		query: []byte(query),
		vars:  vars,
	}
	s.q = &cquery{q: rq}

	if err := sg.compileQuery(s.q, s.role); err != nil {
		return err
	}

	if len(s.q.st.md.Params()) != 0 {
		s.q.st.sql = renderSubWrap(s.q.st)
	}

	go sg.subController(s)
	return nil
}

func (sg *SuperGraph) subController(s *sub) {
	var buf bytes.Buffer
	var stop bool
	var pollSeconds time.Duration

	if sg.conf.PollDuration != 0 {
		pollSeconds = sg.conf.PollDuration * time.Second
	} else {
		pollSeconds = 5 * time.Second
	}

	for {
		if stop {
			break
		}

		select {
		case m := <-s.add:
			_ = json.NewEncoder(&buf).Encode(m.vl)
			v := buf.Bytes()
			buf.Reset()

			m.id = xid.New()

			s.params = append(s.params, v)
			s.dh = append(s.dh, [sha256.Size]byte{})
			s.res = append(s.res, m.Result)
			s.ids = append(s.ids, m.id)

		case m := <-s.del:
			i, ok := s.findByID(m.id)
			if !ok {
				continue
			}

			s.params[i] = s.params[len(s.params)-1]
			s.params = s.params[:len(s.params)-1]

			s.dh[i] = s.dh[len(s.dh)-1]
			s.dh = s.dh[:len(s.dh)-1]

			s.res[i] = s.res[len(s.res)-1]
			s.res = s.res[:len(s.res)-1]

			s.ids[i] = s.ids[len(s.ids)-1]
			s.ids = s.ids[:len(s.ids)-1]

			if len(s.ids) == 0 {
				stop = true
				sg.subs.Delete(s.name)
			}

		case msg := <-s.udh:
			if i, ok := s.findByID(msg.id); ok {
				s.dh[i] = msg.dh
			}

		case <-time.After(pollSeconds):
			switch {
			case s.ops != 0 || len(s.ids) == 0:
				continue

			case len(s.ids) <= maxMembersPerWorker:
				go sg.checkUpdates(s, s.mval, 0)

			default:
				// fan out chunks of work to multiple routines
				// seperated by a random duration
				for i := 0; i < len(s.ids); i += maxMembersPerWorker {
					go sg.checkUpdates(s, s.mval, i)
				}
			}
		}
	}
}

func (sg *SuperGraph) checkUpdates(s *sub, mv mval, start int) {
	// Do not use the `mval` embedded inside sub since
	// its not thread safe use the copy `mv mval`.
	atomic.AddInt64(&s.ops, 1)
	defer atomic.AddInt64(&s.ops, -1)

	// random wait to prevent multiple queries hitting the db
	// at the same time.
	time.Sleep(time.Duration(rand.Int63n(500)) * time.Millisecond)

	end := start + maxMembersPerWorker
	if len(mv.ids) < end {
		end = start + (len(mv.ids) - start)
	}

	var rows *sql.Rows
	var err error

	hasParams := len(s.q.st.md.Params()) != 0
	c := context.Background()

	// when params are not available we use a more optimized
	// codepath that does not use a join query
	if hasParams {
		rows, err = sg.db.QueryContext(c, s.q.st.sql, renderJSONArray(mv.params[start:end]))
	} else {
		rows, err = sg.db.QueryContext(c, s.q.st.sql)
	}

	if err != nil {
		sg.log.Printf("ERR %s", err)
		return
	}

	var js json.RawMessage
	i := 0

	for rows.Next() {
		if err := rows.Scan(&js); err != nil {
			sg.log.Printf("ERR %s", err)
			return
		}
		j := start + i
		i++

		newDH := sha256.Sum256(js)
		if mv.dh[j] != newDH {
			s.udh <- dhmsg{id: mv.ids[j], dh: newDH}
		} else {
			continue
		}

		res := &Result{
			op:   qcode.QTQuery,
			name: s.name,
			sql:  s.q.st.sql,
			role: s.q.st.role.Name,
			Data: js,
		}

		if hasParams {
			select {
			case mv.res[j] <- res:
			default:
			}
			continue
		}

		// if no params exist then optimize by notifying
		// all channels since there will only be one result
		for k := start; k < end; k++ {
			select {
			case mv.res[k] <- res:
			default:
			}
		}
	}
}

func renderSubWrap(st stmt) string {
	var w strings.Builder

	w.WriteString(`WITH "_sg_sub" AS (SELECT `)
	for i, p := range st.md.Params() {
		if i != 0 {
			w.WriteString(`, `)
		}
		w.WriteString(`CAST(x->>`)
		w.WriteString(strconv.FormatInt(int64(i), 10))
		w.WriteString(` as `)
		w.WriteString(p.Type)
		w.WriteString(`) `)
		w.WriteString(p.Name)
	}
	w.WriteString(` FROM json_array_elements($1::json) AS x`)
	w.WriteString(`) SELECT "_sg_sub_data"."__root" FROM "_sg_sub" LEFT OUTER JOIN LATERAL (`)
	w.WriteString(st.sql)
	w.WriteString(`) AS "_sg_sub_data" ON ('true')`)

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
