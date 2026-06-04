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
	"github.com/dosco/graphjin/core/v3/internal/dialect"
	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

const (
	errSubs = "subscription: %s: %s"
)

var minPollDuration = (200 * time.Millisecond)

const (
	defaultSubsInitialChunk = 2000
	defaultSubsMinChunk     = 50
	defaultSubsMaxChunk     = 5000
)

type sub struct {
	k  string
	s  gstate
	js json.RawMessage

	idgen        uint64
	pollInFlight uint32
	add          chan *Member
	del          chan *Member
	updt         chan mmsg
	done         chan struct{}

	sizer *chunkSizer

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
	// indices of cursor value in the arguments array
	cindxs []int
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
	// indices of cursor value in the arguments array
	cindxs []int
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
	rc *RequestConfig,
) (m *Member, err error) {
	// get the name, query vars
	h, err := graph.FastParse(query)
	if err != nil {
		return
	}

	gj, err := g.getEngine()
	if err != nil {
		return
	}

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
	rc *RequestConfig,
) (m *Member, err error) {
	gj, err := g.getEngine()
	if err != nil {
		return
	}

	item, err := gj.allowList.GetByName(name, gj.prod)
	if err != nil {
		return
	}
	r := gj.newGraphqlReq(rc, "subscription", name, nil, vars)
	r.Set(item)

	m, err = gj.subscribe(c, r)
	return
}

// subscribe function is called on the graphjin struct to subscribe to a query.
func (gj *graphjinEngine) subscribe(c context.Context, r GraphqlReq) (
	m *Member, err error,
) {
	if r.operation != qcode.QTSubscription {
		return nil, errors.New("subscription: not a subscription query")
	}

	// transactions not supported with subscriptions
	if r.requestconfig != nil && r.requestconfig.Tx != nil {
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
	for {
		v, _ := gj.subs.LoadOrStore(k, &sub{
			k:     k,
			s:     s,
			add:   make(chan *Member),
			del:   make(chan *Member),
			updt:  make(chan mmsg, 10),
			done:  make(chan struct{}),
			sizer: newChunkSizer(resolveSubsSizerConfig(gj.conf)),
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
		args, err1 := sub.s.argListForSub(c, s.vmap)
		if err1 != nil {
			return nil, err1
		}

		m = &Member{
			ns:     r.namespace,
			id:     atomic.AddUint64(&sub.idgen, 1),
			Result: make(chan *Result, 10),
			sub:    sub,
			vl:     args.values,
			params: args.json,
			cindxs: args.cindxs,
		}

		m.mm, err = gj.subFirstQuery(sub, m)
		if err != nil {
			return nil, err
		}

		select {
		case sub.add <- m:
			return
		case <-sub.done:
			gj.subs.Delete(k)
			continue
		}
	}
}

// initSub function is called on the graphjin struct to initialize a subscription.
func (gj *graphjinEngine) initSub(c context.Context, sub *sub) (err error) {
	if err = sub.s.compile(); err != nil {
		return
	}

	if !gj.prod {
		err = gj.saveToAllowList(sub.s.cs.st.qc, sub.s.r.namespace)
		if err != nil {
			return
		}
	}

	// Only wrap subscriptions for batching if the dialect supports it
	targetCtx := sub.s.getTargetDBCtx()
	if len(sub.s.cs.st.md.Params()) != 0 && dialectSupportsSubscriptionBatching(targetCtx.schema.DBType()) {
		sub.s.cs.st.sql = renderSubWrap(sub.s.cs.st, targetCtx.schema.DBType())
	}

	go gj.subController(sub)
	return
}

// subController function is called on the graphjin struct to control the subscription.
func (gj *graphjinEngine) subController(sub *sub) {
	// remove subscription if controller exists
	defer gj.subs.Delete(sub.k)
	defer close(sub.done)

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

// addMember function is called on the sub struct to add a member.
func (s *sub) addMember(m *Member) error {
	mi := minfo{cindxs: m.cindxs}
	if len(mi.cindxs) != 0 {
		mi.values = m.vl
	}
	mi.dh = m.mm.dh

	// if cindices is not empty then this query contains
	// a cursor that must be updated with the new
	// cursor value so subscriptions can paginate.
	if len(mi.cindxs) != 0 && m.mm.cursor != "" {
		for _, idx := range mi.cindxs {
			mi.values[idx] = m.mm.cursor
		}

		// values is a pre-generated json value that
		// must be re-created.
		if v, err := json.Marshal(mi.values); err != nil {
			return err
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

// deleteMember function is called on the sub struct to delete a member.
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

// updateMember function is called on the sub struct to update a member.
func (s *sub) updateMember(msg mmsg) error {
	i, ok := s.findByID(msg.id)
	if !ok {
		return nil
	}

	if len(s.mi[i].cindxs) != 0 && msg.cursor != "" {
		for _, idx := range s.mi[i].cindxs {
			s.mi[i].values[idx] = msg.cursor
		}
		v, err := json.Marshal(s.mi[i].values)
		if err != nil {
			return err
		}
		s.params[i] = v
	}
	s.mi[i].dh = msg.dh
	return nil
}

// snapshotMembers creates a point-in-time copy of member data for worker goroutines.
// The subscription controller owns live state mutation in s.params/s.mi/s.res/s.ids,
// so this must be called on the controller goroutine before poll workers start.
func (s *sub) snapshotMembers() mval {
	mv := mval{
		params: append([]json.RawMessage(nil), s.params...),
		mi:     make([]minfo, len(s.mi)),
		res:    append([]chan *Result(nil), s.res...),
		ids:    append([]uint64(nil), s.ids...),
	}

	for i, mi := range s.mi {
		mv.mi[i] = mi

		if len(mi.values) != 0 {
			mv.mi[i].values = make([]interface{}, len(mi.values))
			copy(mv.mi[i].values, mi.values)
		}

		if len(mi.cindxs) != 0 {
			mv.mi[i].cindxs = make([]int, len(mi.cindxs))
			copy(mv.mi[i].cindxs, mi.cindxs)
		}
	}

	return mv
}

func (s *sub) beginPollCycle() bool {
	return atomic.CompareAndSwapUint32(&s.pollInFlight, 0, 1)
}

func (s *sub) endPollCycle() {
	atomic.StoreUint32(&s.pollInFlight, 0)
}

// fanOutJobs function is called on the sub struct to fan out jobs.
func (s *sub) fanOutJobs(gj *graphjinEngine) {
	// Do not start a new poll while the previous cycle is still running.
	// Overlapping polls can use stale cursor snapshots and re-emit already
	// delivered rows, which is most visible on slower databases.
	if !s.beginPollCycle() {
		return
	}

	// Snapshot member state on the controller goroutine before launching
	// async workers to avoid races with live add/remove/update operations.
	mv := s.snapshotMembers()
	if len(mv.ids) == 0 {
		s.endPollCycle()
		return
	}

	// Run the full poll cycle asynchronously so the controller can continue
	// handling add/remove/update events while polling is in progress.
	go func(mv mval) {
		defer s.endPollCycle()

		// Process members in adaptive chunks within one poll cycle.
		// Re-read the sizer between chunks so observations from the
		// previous chunk influence the next one.
		for i := 0; i < len(mv.ids); {
			chunk := s.sizer.current()
			gj.subCheckUpdates(s, mv, i, chunk)
			i += chunk
		}
	}(mv)
}

// subCheckUpdates function is called on the graphjin struct to check updates.
func (gj *graphjinEngine) subCheckUpdates(sub *sub, mv mval, start, chunk int) {
	// Do not use the `mval` embedded inside sub since
	// its not thread safe use the copy `mv mval`.

	end := start + chunk
	if len(mv.ids) < end {
		end = start + (len(mv.ids) - start)
	}

	hasParams := len(sub.s.cs.st.md.Params()) != 0
	subDBCtx := sub.s.getTargetDBCtx()
	supportsBatching := dialectSupportsSubscriptionBatching(subDBCtx.schema.DBType())

	var rows *sql.Rows
	var err error

	c := context.Background()

	// when params are not available we use a more optimized
	// codepath that does not use a join query
	// more details on this optimization are towards the end
	// of the function

	// For dialects that don't support batching, we need to query each member individually
	if hasParams && !supportsBatching {
		mdParams := sub.s.cs.st.md.Params()
		for j := start; j < end; j++ {
			jIdx := j // capture for closure
			err = retryOperationForDB(c, subDBCtx.dbtype, func() error {
				// Parse JSON params to get individual values
				var values []interface{}
				if mv.mi[jIdx].values != nil {
					// Use stored values if available (cursor case)
					values = mv.mi[jIdx].values
				} else {
					// Parse from JSON params
					var arr []json.RawMessage
					if err := json.Unmarshal(mv.params[jIdx], &arr); err != nil {
						return err
					}
					values = make([]interface{}, len(mdParams))
					for i := range mdParams {
						if i < len(arr) {
							// Parse raw value using parseVarVal for proper type conversion
							values[i] = parseVarVal(arr[i])
						}
					}
				}
				q, qargs, err := prepareQueryArgsForDB(subDBCtx.dbtype, sub.s.cs.st.sql, values)
				if err != nil {
					return err
				}
				row := subDBCtx.db.QueryRowContext(c, q, qargs...)
				var b []byte
				if err := row.Scan(&b); err != nil {
					return err
				}
				js := json.RawMessage(b)
				gj.subNotifyMember(sub, mv, jIdx, js)
				return nil
			})
			if err != nil {
				gj.log.Printf(errSubs, "query", err)
			}
		}
		return
	}

	var params json.RawMessage

	if hasParams {
		params = renderJSONArray(mv.params[start:end])
	}

	t0 := time.Now()
	err = retryOperationForDB(c, subDBCtx.dbtype, func() (err1 error) {
		if hasParams {
			q, qargs, err2 := prepareQueryArgsForDB(subDBCtx.dbtype, sub.s.cs.st.sql, []interface{}{string(params)})
			if err2 != nil {
				return err2
			}
			//nolint: sqlclosecheck
			rows, err1 = subDBCtx.db.QueryContext(c, q, qargs...)
		} else {
			//nolint: sqlclosecheck
			rows, err1 = subDBCtx.db.QueryContext(c, sub.s.cs.st.sql)
		}
		return
	})
	if err != nil {
		sub.sizer.observeError()
		gj.log.Printf(errSubs, "query", err)
		return
	}
	sub.sizer.observe(time.Since(t0))
	defer rows.Close() //nolint:errcheck

	var b []byte
	i := 0
	for rows.Next() {
		if err := rows.Scan(&b); err != nil {
			gj.log.Printf(errSubs, "scan", err)
			return
		}
		js := json.RawMessage(b)

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

// subFirstQuery function is called on the graphjin struct to get the first query.
func (gj *graphjinEngine) subFirstQuery(sub *sub, m *Member) (mmsg, error) {
	c := context.Background()

	// when params are not available we use a more optimized
	// codepath that does not use a join query
	// more details on this optimization are towards the end
	// of the function
	var js json.RawMessage
	var mm mmsg
	var err error

	subDBCtx := sub.s.getTargetDBCtx()
	supportsBatching := dialectSupportsSubscriptionBatching(subDBCtx.schema.DBType())

	if sub.js != nil {
		js = sub.js
	} else {
		err := retryOperationForDB(c, subDBCtx.dbtype, func() error {
			var row *sql.Row
			q := sub.s.cs.st.sql

			if m.params != nil {
				if supportsBatching {
					// Use JSON array for batching-enabled dialects
					sqlQuery, sqlArgs, err2 := prepareQueryArgsForDB(subDBCtx.dbtype, q,
						[]interface{}{string(renderJSONArray([]json.RawMessage{m.params}))})
					if err2 != nil {
						return err2
					}
					row = subDBCtx.db.QueryRowContext(c, sqlQuery, sqlArgs...)
				} else {
					// Use m.vl (value list) directly for non-batching dialects
					// m.vl contains the parsed values in the correct order
					sqlQuery, sqlArgs, err2 := prepareQueryArgsForDB(subDBCtx.dbtype, q, m.vl)
					if err2 != nil {
						return err2
					}
					row = subDBCtx.db.QueryRowContext(c, sqlQuery, sqlArgs...)
				}
			} else {
				row = subDBCtx.db.QueryRowContext(c, q)
			}
			var b []byte
			if err := row.Scan(&b); err != nil {
				return err
			}
			js = json.RawMessage(b)
			return nil
		})
		if err != nil {
			return mm, fmt.Errorf(errSubs, "scan", err)
		}
	}

	mm, err = gj.subNotifyMemberEx(sub,
		[32]byte{},
		m.cindxs,
		m.id,
		m.Result, js, false)

	return mm, err
}

// subNotifyMember function is called on the graphjin struct to notify a member.
func (gj *graphjinEngine) subNotifyMember(s *sub, mv mval, j int, js json.RawMessage) {
	_, err := gj.subNotifyMemberEx(s,
		mv.mi[j].dh,
		mv.mi[j].cindxs,
		mv.ids[j],
		mv.res[j], js, true)
	if err != nil {
		gj.log.Print(err.Error())
	}
}

// subNotifyMemberEx function is called on the graphjin struct to notify a member.
func (gj *graphjinEngine) subNotifyMemberEx(sub *sub,
	dh [32]byte, cindxs []int, id uint64, rc chan *Result, js json.RawMessage, update bool,
) (mm mmsg, err error) {
	mm = mmsg{id: id}

	mm.dh = sha256.Sum256(js)
	if dh == mm.dh {
		return mm, nil
	}

	nonce := mm.dh

	if cv := firstCursorValue(js, gj.printFormat); len(cv) != 0 {
		cursor := string(cv)
		// Strip the gj-xxx: prefix from cursor for internal subscription use
		// The cursor format is: gj-hexTimestamp:selID:val1:val2
		// We store just: selID:val1:val2 to match the decrypted format
		// that the SQL CTE expects
		if strings.HasPrefix(cursor, "gj-") {
			if idx := strings.Index(cursor, ":"); idx != -1 {
				cursor = cursor[idx+1:]
			}
		}
		mm.cursor = cursor
	}

	ejs, err := encryptValues(js,
		gj.printFormat,
		decPrefix,
		nonce[:],
		gj.encryptionKey)
	if err != nil {
		return mm, err
	}

	// we're expecting a cursor but the cursor was null
	// so we skip this one but still send the hash update
	// to prevent reprocessing the same result.
	if len(cindxs) != 0 && mm.cursor == "" {
		if update {
			sub.updt <- mm
		}
		return mm, nil
	}

	if update {
		sub.updt <- mm
	}

	res := &Result{
		operation: qcode.QTQuery,
		name:      sub.s.r.name,
		sql:       sub.s.cs.st.sql,
		role:      sub.s.cs.st.role,
		Data:      ejs,
	}

	// If this is an update notification, avoid blocking indefinitely by using a timeout.
	// For the initial subscription response, perform a blocking send to guarantee delivery.
	if update {
		select {
		case rc <- res:
		case <-time.After(250 * time.Millisecond):
		}
	} else {
		rc <- res
	}

	return mm, nil
}

// getDialectForType returns a dialect instance for the given database type.
func getDialectForType(ct string) dialect.Dialect {
	switch ct {
	case "mysql":
		return &dialect.MySQLDialect{}
	case "mariadb":
		return &dialect.MariaDBDialect{}
	case "oracle":
		return &dialect.OracleDialect{}
	case "sqlite":
		return &dialect.SQLiteDialect{}
	case "mssql":
		return &dialect.MSSQLDialect{}
	case "snowflake":
		return &dialect.SnowflakeDialect{}
	case "mongodb":
		return &dialect.MongoDBDialect{}
	case "cassandra":
		return &dialect.CassandraDialect{}
	default:
		return &dialect.PostgresDialect{}
	}
}

// dialectSupportsSubscriptionBatching checks if the database type supports subscription batching.
func dialectSupportsSubscriptionBatching(ct string) bool {
	return getDialectForType(ct).SupportsSubscriptionBatching()
}

// renderSubWrap function is called on the graphjin struct to render a sub wrap.
func renderSubWrap(st stmt, ct string) string {
	d := getDialectForType(ct)

	params := make([]dialect.Param, len(st.md.Params()))
	for i, p := range st.md.Params() {
		params[i] = dialect.Param{Name: p.Name, Type: p.Type}
	}

	sc := &stringContext{ct: ct}
	d.RenderSubscriptionUnbox(sc, params, st.sql)

	return sc.sb.String()
}

type stringContext struct {
	sb strings.Builder
	ct string
}

func (c *stringContext) Write(s string) (int, error) {
	return c.sb.WriteString(s)
}

func (c *stringContext) WriteString(s string) (int, error) {
	return c.sb.WriteString(s)
}
func (c *stringContext) AddParam(p dialect.Param) string {
	return ""
}
func (c *stringContext) Quote(s string) {
	switch c.ct {
	case "mysql":
		c.sb.WriteString("`")
		c.sb.WriteString(s)
		c.sb.WriteString("`")
	case "oracle":
		c.sb.WriteString(`"`)
		c.sb.WriteString(strings.ToUpper(s))
		c.sb.WriteString(`"`)
	case "mssql":
		c.sb.WriteString(`[`)
		c.sb.WriteString(s)
		c.sb.WriteString(`]`)
	default:
		c.sb.WriteString(`"`)
		c.sb.WriteString(s)
		c.sb.WriteString(`"`)
	}
}
func (c *stringContext) ColWithTable(table, col string) {
	if table != "" {
		c.Quote(table)
		c.sb.WriteString(".")
	}
	c.Quote(col)
}
func (c *stringContext) RenderJSONFields(sel *qcode.Select)      {}
func (c *stringContext) IsTableMutated(table string) bool        { return false }
func (c *stringContext) GetStaticVar(name string) (string, bool) { return "", false }
func (c *stringContext) GetSecPrefix() string                    { return "" }
func (c *stringContext) RenderExp(ti sdata.DBTable, ex *qcode.Exp) {
	// Not implemented for stringContext - only used for subscription unboxing
}
func (c *stringContext) SetError(err error) {
	// stringContext is fire-and-forget; no error path
}

// renderJSONArray function is called on the graphjin struct to render a json array.
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

// chunkSizer tunes the per-poll-cycle chunk size used when batching
// subscriber queries to the database. It uses AIMD on top of an
// EMA-smoothed observed latency: grow additively when queries finish
// well under the target, shrink multiplicatively when they overshoot.
//
// All state is guarded by a single mutex; callers should read current()
// once per chunk into a local so a concurrent observation cannot make
// loop bounds inconsistent.
type chunkSizer struct {
	mu     sync.Mutex
	cur    int
	min    int
	max    int
	target time.Duration
	ema    time.Duration
	seeded bool
}

type sizerCfg struct {
	min     int
	max     int
	target  time.Duration
	initial int
}

// resolveSubsSizerConfig returns the effective bounds + target + initial
// chunk size from a Config, applying defaults for zero values. Splitting
// this from newChunkSizer keeps the resolution rules unit-testable
// without constructing a sizer.
func resolveSubsSizerConfig(c *Config) sizerCfg {
	cfg := sizerCfg{
		min: c.SubsMinMembersPerWorker,
		max: c.SubsMaxMembersPerWorker,
	}
	if cfg.min <= 0 {
		cfg.min = defaultSubsMinChunk
	}
	if cfg.max <= 0 {
		cfg.max = defaultSubsMaxChunk
	}
	if cfg.min > cfg.max {
		cfg.min = cfg.max
	}

	cfg.target = c.SubsTargetQueryLatency
	if cfg.target <= 0 {
		ps := c.SubsPollDuration
		if ps < minPollDuration {
			ps = minPollDuration
		}
		cfg.target = ps / 4
	}

	cfg.initial = defaultSubsInitialChunk
	if cfg.initial > cfg.max {
		cfg.initial = cfg.max
	}
	if cfg.initial < cfg.min {
		cfg.initial = cfg.min
	}
	return cfg
}

func newChunkSizer(cfg sizerCfg) *chunkSizer {
	return &chunkSizer{
		cur:    cfg.initial,
		min:    cfg.min,
		max:    cfg.max,
		target: cfg.target,
	}
}

func (z *chunkSizer) current() int {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.cur
}

// observe records a successful query latency and adjusts the chunk size
// using AIMD with a deadband around the target.
func (z *chunkSizer) observe(d time.Duration) {
	if z == nil {
		return
	}
	z.mu.Lock()
	defer z.mu.Unlock()

	// EMA with α = 0.3; first observation seeds the value to avoid a
	// long warmup from zero.
	if !z.seeded {
		z.ema = d
		z.seeded = true
	} else {
		// ema = ema*0.7 + d*0.3, in nanos to keep things integral.
		z.ema = (z.ema*7 + d*3) / 10
	}

	low := (z.target * 8) / 10
	high := (z.target * 12) / 10

	switch {
	case z.ema < low:
		step := z.cur / 8
		if step < 64 {
			step = 64
		}
		z.cur += step
		if z.cur > z.max {
			z.cur = z.max
		}
	case z.ema > high:
		next := (z.cur * 7) / 10
		if next < z.min {
			next = z.min
		}
		z.cur = next
	}
}

// observeError records a failed query: shrink once, never grow,
// and don't poison the EMA with a meaningless latency reading.
func (z *chunkSizer) observeError() {
	if z == nil {
		return
	}
	z.mu.Lock()
	defer z.mu.Unlock()
	next := z.cur / 2
	if next < z.min {
		next = z.min
	}
	z.cur = next
}

// findByID function is called on the sub struct to find a member by id.
func (s *sub) findByID(id uint64) (int, bool) {
	for i := range s.ids {
		if s.ids[i] == id {
			return i, true
		}
	}
	return 0, false
}

// Unsubscribe function is called on the member struct to unsubscribe.
func (m *Member) Unsubscribe() {
	if m != nil && !m.done {
		m.sub.del <- m
		m.done = true
	}
}

// ID function is called on the member struct to get the id.
func (m *Member) ID() uint64 {
	return m.id
}

// String function is called on the member struct to get the string.
func (m *Member) String() string {
	return strconv.Itoa(int(m.id))
}
