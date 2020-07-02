package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hash/maphash"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dosco/super-graph/core/internal/psql"
	"github.com/dosco/super-graph/core/internal/qcode"
	"github.com/rs/xid"
)

const (
	maxMembersPerWorker = 2000
)

type sub struct {
	name string
	role string
	st   stmt
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
	id     xid.ID
	vl     []interface{}
}

func (sg *SuperGraph) Subscribe(c context.Context, query string, vars json.RawMessage) (*Member, error) {
	var err error
	name := Name(query)

	if Operation(query) != OpQuery {
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

	h := maphash.Hash{}
	h.SetSeed(sg.hashSeed)
	_, _ = h.WriteString(name)
	_, _ = h.WriteString(role)

	v, _ := sg.subs.LoadOrStore(h.Sum64(), &sub{
		name: name,
		role: role,
		add:  make(chan *Member),
		del:  make(chan *Member),
		udh:  make(chan dhmsg),
	})
	s := v.(*sub)

	s.Do(func() {
		err = sg.newSub(c, s, query, vars)
	})
	if err != nil {
		return nil, err
	}

	varsList, err := sg.argList(c, s.st.md, vars)
	if err != nil {
		return nil, err
	}

	m := &Member{
		Result: make(chan *Result),
		sub:    s,
		vl:     varsList,
	}
	s.add <- m

	return m, nil
}

func (sg *SuperGraph) UnSubscribe(c context.Context, m *Member) {
	if m != nil {
		m.sub.del <- m
	}
}

func (sg *SuperGraph) newSub(c context.Context, s *sub, query string, vars json.RawMessage) error {
	st, err := sg.buildStmt(qcode.QTQuery, []byte(query), vars, s.role, true)
	if err != nil {
		return err
	}

	if len(st) == 0 {
		return fmt.Errorf("invalid query")
	}

	s.st = st[0]

	if len(s.st.md.Params()) != 0 {
		s.st.sql = renderSubWrap(s.st)
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
			json.NewEncoder(&buf).Encode(m.vl)
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

	hasParams := len(s.st.md.Params()) != 0
	c := context.Background()

	// when params are not available we use a more optimized
	// codepath that does not use a join query
	if hasParams {
		rows, err = sg.db.QueryContext(c, s.st.sql, renderJSONArray(mv.params[start:end]))
	} else {
		rows, err = sg.db.QueryContext(c, s.st.sql)
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
			op:   s.st.qc.Type,
			name: s.name,
			// sql:  s.st.sql,
			role: s.st.role.Name,
			Data: js,
		}

		if hasParams {
			select {
			case mv.res[j] <- res:
			}
			continue
		}

		// if no params exist then optimize by notifying
		// all channels since there will only be one result
		for k := start; k < end; k++ {
			select {
			case mv.res[k] <- res:
			}
		}
	}
}

func renderSubCols(params []psql.Param, values bool, start int) string {
	var s strings.Builder
	for i, p := range params {
		s.WriteString(`, `)
		if values {
			s.WriteRune('$')
			s.WriteString(strconv.Itoa(i + start))
		} else {
			s.WriteString(p.Name)
			s.WriteRune(' ')
			s.WriteString(p.Type)
		}
	}
	return s.String()
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
