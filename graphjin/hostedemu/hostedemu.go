package hostedemu

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	BackendCapture = "capture"
	BackendDuckDB  = "duckdb"

	FallbackPlaceholderOnError = "placeholder_on_error"
	FallbackStrict             = "strict"

	DiscoveryInformationSchema = "information_schema"
	DiscoveryShow              = "show"
)

type Config struct {
	SeedPath   string
	SeedSQL    string
	DBPath     string
	CaptureDir string
	TestName   string
	RunID      string
	Backend    string
	Fallback   string
	Discovery  string
}

type Adapter interface {
	Name() string
	DefaultSeedPath() string
	ParseSeed(seedSQL string) (any, error)
	NewSession(catalog any) Session
	TranslateSetup(seedSQL string, catalog any) ([]string, error)
	TranslateDiscoveryQuery(sql string, args []driver.NamedValue, catalog any) (string, []driver.NamedValue, error)
	TranslateDiscoveryExec(sql string, args []driver.NamedValue, catalog any) ([]string, []driver.NamedValue, error)
	TranslateRuntime(sql string, args []driver.NamedValue, catalog any) (string, []driver.NamedValue, error)
	TranslateDirect(sql string, args []driver.NamedValue, catalog any) (string, []driver.NamedValue, error)
	NormalizeIdentifier(identifier string) string
	MapType(sourceType string) string
	ClassifyPhase(sql string) string
}

type Session interface {
	PlaceholderQuery(sql string) (*Rows, string, error)
}

type CatalogMutator interface {
	ApplyDDL(sql string)
}

type DiscoveryConfigurer interface {
	SetDiscoveryMode(mode string)
}

type MetadataRefresher interface {
	TranslateMetadataRefresh(catalog any) ([]string, error)
}

func NewConnector(conf Config, adapter Adapter) driver.Connector {
	st, err := newState(conf, adapter)
	return &connector{state: st, initErr: err}
}

func CapturePath(dir, testName string) string {
	return filepath.Join(dir, sanitizeFileName(testName)+".jsonl")
}

type connector struct {
	state   *state
	initErr error
}

func (c *connector) Connect(context.Context) (driver.Conn, error) {
	if c.initErr != nil {
		return nil, c.initErr
	}
	return &conn{state: c.state}, nil
}

func (c *connector) Driver() driver.Driver {
	return &drv{state: c.state, initErr: c.initErr}
}

type drv struct {
	state   *state
	initErr error
}

func (d *drv) Open(string) (driver.Conn, error) {
	if d.initErr != nil {
		return nil, d.initErr
	}
	return &conn{state: d.state}, nil
}

type state struct {
	conf    Config
	adapter Adapter
	catalog any
	session Session
	mu      sync.Mutex
	qidMu   sync.Mutex // serializes query-id-producing execs + their capture
	seq     int64
	file    *os.File
	duck    *sql.DB
	duckErr error
}

func newState(conf Config, adapter Adapter) (*state, error) {
	if adapter == nil {
		return nil, fmt.Errorf("hostedemu: nil adapter")
	}
	if conf.SeedPath == "" && conf.SeedSQL == "" {
		conf.SeedPath = adapter.DefaultSeedPath()
	}
	if conf.TestName == "" {
		conf.TestName = adapter.Name()
	}
	if conf.RunID == "" {
		conf.RunID = time.Now().UTC().Format("20060102T150405.000000000Z")
	}
	conf.Backend = NormalizeBackend(conf.Backend)
	conf.Fallback = NormalizeFallback(conf.Fallback)
	conf.Discovery = NormalizeDiscovery(conf.Discovery)

	seedSQL := conf.SeedSQL
	if seedSQL == "" {
		data, err := os.ReadFile(conf.SeedPath)
		if err != nil {
			return nil, err
		}
		seedSQL = string(data)
	}
	catalog, err := adapter.ParseSeed(seedSQL)
	if err != nil {
		return nil, err
	}
	if cfg, ok := catalog.(DiscoveryConfigurer); ok {
		cfg.SetDiscoveryMode(conf.Discovery)
	}
	st := &state{
		conf:    conf,
		adapter: adapter,
		catalog: catalog,
		session: adapter.NewSession(catalog),
	}
	if st.session == nil {
		return nil, fmt.Errorf("hostedemu: adapter %q returned nil session", adapter.Name())
	}
	if conf.CaptureDir != "" {
		if err := os.MkdirAll(conf.CaptureDir, 0755); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(CapturePath(conf.CaptureDir, conf.TestName), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if err != nil {
			return nil, err
		}
		st.file = f
	}
	if conf.Backend == BackendDuckDB {
		st.duck, st.duckErr = openDuckDB(seedSQL, catalog, adapter, conf.DBPath)
		if st.duckErr != nil && conf.Fallback == FallbackStrict {
			_ = st.close()
			return nil, st.duckErr
		}
	}
	return st, nil
}

func (s *state) close() error {
	var err error
	if s.file != nil {
		err = s.file.Close()
		s.file = nil
	}
	if s.duck != nil {
		if e := s.duck.Close(); err == nil {
			err = e
		}
		s.duck = nil
	}
	return err
}

type conn struct {
	state *state
	// lastQueryID models Snowflake's per-session LAST_QUERY_ID(): real Snowflake
	// scopes it per connection, so the shared _gj_sf_session table cannot be
	// trusted under concurrent discovery. Captured per connection right after a
	// query-id-producing exec (e.g. SHOW), under state.qidMu.
	lastQueryID string
}

// QueryIDTracker is an optional Adapter capability for dialects (Snowflake) whose
// discovery reads a prior statement's result back via a session-scoped query id
// (LAST_QUERY_ID() + RESULT_SCAN). The harness captures the id per connection so
// concurrent discoveries don't race on shared emulator session state. Adapters
// that don't implement it (BigQuery) are unaffected.
type QueryIDTracker interface {
	// IsQueryIDProducer reports whether sql sets the session's last query id.
	IsQueryIDProducer(sql string) bool
	// IsQueryIDQuery reports whether sql reads the session's last query id.
	IsQueryIDQuery(sql string) bool
	// CaptureQueryIDSQL returns a one-row, one-column query for the current id.
	CaptureQueryIDSQL() string
}

func (c *conn) Prepare(query string) (driver.Stmt, error) {
	return &stmt{conn: c, query: query}, nil
}

func (c *conn) Close() error { return nil }

func (c *conn) Begin() (driver.Tx, error) { return tx{}, nil }

func (c *conn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return tx{}, nil
}

func (c *conn) Ping(context.Context) error { return nil }

func (c *conn) CheckNamedValue(*driver.NamedValue) error { return nil }

func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	phase := c.state.adapter.ClassifyPhase(query)

	// For query-id-producing execs (Snowflake SHOW), hold qidMu across the exec
	// and the per-connection id capture so a concurrent connection's SHOW can't
	// clobber the shared last_query_id between them.
	tracker, _ := c.state.adapter.(QueryIDTracker)
	produces := tracker != nil && tracker.IsQueryIDProducer(query)
	if produces {
		c.state.qidMu.Lock()
		defer c.state.qidMu.Unlock()
	}

	res, result, trace, err := c.state.exec(ctx, phase, query, args)
	c.state.capture("exec", phase, query, args, result, err, trace)
	if err != nil {
		return nil, err
	}
	if produces {
		c.captureQueryID(ctx, tracker.CaptureQueryIDSQL())
	}
	return res, nil
}

func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	phase := c.state.adapter.ClassifyPhase(query)

	// Serve LAST_QUERY_ID() from this connection's captured id rather than the
	// shared session table, matching real Snowflake's per-session semantics.
	if tracker, ok := c.state.adapter.(QueryIDTracker); ok && tracker.IsQueryIDQuery(query) {
		rows := NewRows([]string{"LAST_QUERY_ID"}, []driver.Value{c.lastQueryID})
		c.state.capture("query", phase, query, args, "rows:1", nil, backendTrace{Backend: c.state.conf.Backend})
		return rows, nil
	}

	rows, result, trace, err := c.state.query(ctx, phase, query, args)
	c.state.capture("query", phase, query, args, result, err, trace)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// captureQueryID reads the current session query id into this connection's
// per-connection slot. Called under state.qidMu right after a producing exec.
func (c *conn) captureQueryID(ctx context.Context, sql string) {
	if sql == "" || c.state.duck == nil {
		return
	}
	var qid string
	if err := c.state.duck.QueryRowContext(ctx, sql).Scan(&qid); err == nil {
		c.lastQueryID = qid
	}
}

type stmt struct {
	conn  *conn
	query string
}

func (s *stmt) Close() error { return nil }

func (s *stmt) NumInput() int { return -1 }

func (s *stmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.conn.ExecContext(context.Background(), s.query, valuesToNamed(args))
}

func (s *stmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.conn.QueryContext(context.Background(), s.query, valuesToNamed(args))
}

type tx struct{}

func (tx) Commit() error   { return nil }
func (tx) Rollback() error { return nil }

type resultInfo int64

func (r resultInfo) LastInsertId() (int64, error) { return 0, nil }
func (r resultInfo) RowsAffected() (int64, error) { return int64(r), nil }

type backendTrace struct {
	Backend          string
	TranslatedSQL    string
	TranslatedArgs   []driver.NamedValue
	TranslationError error
	ExecutionError   error
}

type Rows struct {
	cols []string
	vals [][]driver.Value
	pos  int
}

func NewRows(cols []string, vals ...[]driver.Value) *Rows {
	return &Rows{cols: cols, vals: vals}
}

func (r *Rows) Columns() []string { return r.cols }

func (r *Rows) Close() error { return nil }

func (r *Rows) Next(dest []driver.Value) error {
	if r.pos >= len(r.vals) {
		return io.EOF
	}
	row := r.vals[r.pos]
	r.pos++
	for i := range dest {
		if i < len(row) {
			dest[i] = row[i]
		} else {
			dest[i] = nil
		}
	}
	return nil
}

func (r *Rows) Len() int {
	if r == nil {
		return 0
	}
	return len(r.vals)
}

func (s *state) exec(ctx context.Context, phase, query string, args []driver.NamedValue) (driver.Result, string, backendTrace, error) {
	trace := backendTrace{Backend: s.conf.Backend}
	return s.execBackend(ctx, phase, query, args, trace)
}

func (s *state) query(ctx context.Context, phase, query string, args []driver.NamedValue) (*Rows, string, backendTrace, error) {
	trace := backendTrace{Backend: s.conf.Backend}
	return s.queryBackend(ctx, phase, query, args, trace)
}

func (s *state) execBackend(ctx context.Context, phase, query string, args []driver.NamedValue, trace backendTrace) (driver.Result, string, backendTrace, error) {
	if s.conf.Backend != BackendDuckDB {
		return resultInfo(1), "ok", trace, nil
	}
	translated, translatedArgs, err := s.translateExecSQL(phase, query, args)
	trace.TranslatedSQL = strings.Join(translated, ";\n")
	trace.TranslatedArgs = translatedArgs
	if err != nil {
		trace.TranslationError = err
		return s.fallbackExec(trace, err)
	}
	if s.duckErr != nil {
		trace.ExecutionError = s.duckErr
		return s.fallbackExec(trace, s.duckErr)
	}
	if s.duck == nil {
		err := fmt.Errorf("duckdb backend is not initialized")
		trace.ExecutionError = err
		return s.fallbackExec(trace, err)
	}
	var affected int64 = 1
	for i, stmt := range translated {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		stmtArgs := translatedArgs
		if i != 0 {
			stmtArgs = nil
		}
		res, err := s.duck.ExecContext(ctx, stmt, sqlArgs(stmtArgs)...)
		if err != nil {
			trace.ExecutionError = err
			return s.fallbackExec(trace, err)
		}
		if rows, err := res.RowsAffected(); err == nil {
			affected = rows
		}
	}
	if phase != "discovery" && isDDL(query) {
		if mutator, ok := s.session.(CatalogMutator); ok {
			mutator.ApplyDDL(query)
		}
		if refresher, ok := s.adapter.(MetadataRefresher); ok {
			if err := s.refreshMetadata(ctx, refresher); err != nil {
				trace.ExecutionError = err
				return s.fallbackExec(trace, err)
			}
		}
	}
	return resultInfo(affected), "ok", trace, nil
}

func (s *state) refreshMetadata(ctx context.Context, refresher MetadataRefresher) error {
	if s.duck == nil {
		return nil
	}
	stmts, err := refresher.TranslateMetadataRefresh(s.catalog)
	if err != nil {
		return err
	}
	for _, stmt := range stmts {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := s.duck.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("duckdb metadata refresh: %w\nSQL: %s", err, stmt)
		}
	}
	return nil
}

func isDDL(query string) bool {
	upper := strings.ToUpper(strings.TrimSpace(query))
	return strings.HasPrefix(upper, "CREATE ") ||
		strings.HasPrefix(upper, "ALTER ") ||
		strings.HasPrefix(upper, "DROP ")
}

func (s *state) queryBackend(ctx context.Context, phase, query string, args []driver.NamedValue, trace backendTrace) (*Rows, string, backendTrace, error) {
	if s.conf.Backend != BackendDuckDB {
		r, result, err := s.session.PlaceholderQuery(query)
		return r, result, trace, err
	}
	translated, translatedArgs, err := s.translateSQL(phase, query, args)
	trace.TranslatedSQL = translated
	trace.TranslatedArgs = translatedArgs
	if err != nil {
		trace.TranslationError = err
		return s.fallbackQuery(query, trace, err)
	}
	if s.duckErr != nil {
		trace.ExecutionError = s.duckErr
		return s.fallbackQuery(query, trace, s.duckErr)
	}
	if s.duck == nil {
		err := fmt.Errorf("duckdb backend is not initialized")
		trace.ExecutionError = err
		return s.fallbackQuery(query, trace, err)
	}
	sqlRows, err := s.duck.QueryContext(ctx, translated, sqlArgs(translatedArgs)...)
	if err != nil {
		trace.ExecutionError = err
		return s.fallbackQuery(query, trace, err)
	}
	defer sqlRows.Close()
	r, result, err := driverRowsFromSQLRows(sqlRows)
	if err != nil {
		trace.ExecutionError = err
		return s.fallbackQuery(query, trace, err)
	}
	return r, result, trace, nil
}

func (s *state) translateSQL(phase, query string, args []driver.NamedValue) (string, []driver.NamedValue, error) {
	switch phase {
	case "discovery":
		return s.adapter.TranslateDiscoveryQuery(query, args, s.catalog)
	case "runtime":
		return s.adapter.TranslateRuntime(query, args, s.catalog)
	default:
		return s.adapter.TranslateDirect(query, args, s.catalog)
	}
}

func (s *state) translateExecSQL(phase, query string, args []driver.NamedValue) ([]string, []driver.NamedValue, error) {
	switch phase {
	case "discovery":
		return s.adapter.TranslateDiscoveryExec(query, args, s.catalog)
	case "runtime":
		sql, translatedArgs, err := s.adapter.TranslateRuntime(query, args, s.catalog)
		return []string{sql}, translatedArgs, err
	default:
		sql, translatedArgs, err := s.adapter.TranslateDirect(query, args, s.catalog)
		return []string{sql}, translatedArgs, err
	}
}

func (s *state) fallbackExec(trace backendTrace, err error) (driver.Result, string, backendTrace, error) {
	if s.conf.Fallback == FallbackStrict {
		return nil, "", trace, err
	}
	return resultInfo(1), "ok", trace, nil
}

func (s *state) fallbackQuery(query string, trace backendTrace, err error) (*Rows, string, backendTrace, error) {
	if s.conf.Fallback == FallbackStrict {
		return nil, "", trace, err
	}
	r, result, placeholderErr := s.session.PlaceholderQuery(query)
	if placeholderErr != nil {
		return nil, "", trace, placeholderErr
	}
	return r, result, trace, nil
}

type captureEvent struct {
	RunID            string `json:"run_id"`
	Test             string `json:"test"`
	Seq              int64  `json:"seq"`
	Op               string `json:"op"`
	Phase            string `json:"phase"`
	SQL              string `json:"sql"`
	Args             []any  `json:"args"`
	NormalizedSQL    string `json:"normalized_sql"`
	SQLHash          string `json:"sql_hash"`
	Backend          string `json:"backend,omitempty"`
	TranslatedSQL    string `json:"translated_sql,omitempty"`
	TranslatedArgs   []any  `json:"translated_args,omitempty"`
	TranslationError string `json:"translation_error,omitempty"`
	ExecutionError   string `json:"execution_error,omitempty"`
	Result           string `json:"result"`
	Error            string `json:"error,omitempty"`
}

func (s *state) capture(op, phase, query string, args []driver.NamedValue, result string, err error, trace backendTrace) {
	if s == nil || s.file == nil {
		return
	}
	norm := NormalizeSQL(query)
	sum := sha256.Sum256([]byte(norm))
	ev := captureEvent{
		RunID:          s.conf.RunID,
		Test:           s.conf.TestName,
		Op:             op,
		Phase:          phase,
		SQL:            query,
		Args:           NamedValues(args),
		NormalizedSQL:  norm,
		SQLHash:        hex.EncodeToString(sum[:]),
		Backend:        trace.Backend,
		TranslatedSQL:  trace.TranslatedSQL,
		TranslatedArgs: NamedValues(trace.TranslatedArgs),
		Result:         result,
	}
	if err != nil {
		ev.Error = err.Error()
	}
	if trace.TranslationError != nil {
		ev.TranslationError = trace.TranslationError.Error()
	}
	if trace.ExecutionError != nil {
		ev.ExecutionError = trace.ExecutionError.Error()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	ev.Seq = s.seq
	data, jsonErr := json.Marshal(ev)
	if jsonErr != nil {
		return
	}
	_, _ = s.file.Write(append(data, '\n'))
	_ = s.file.Sync()
}

func NormalizeSQL(query string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(query)), " ")
}

func NamedValues(args []driver.NamedValue) []any {
	if len(args) == 0 {
		return nil
	}
	out := make([]any, len(args))
	for i, arg := range args {
		switch v := arg.Value.(type) {
		case []byte:
			out[i] = string(v)
		default:
			out[i] = v
		}
	}
	return out
}

func sqlArgs(args []driver.NamedValue) []any {
	if len(args) == 0 {
		return nil
	}
	out := make([]any, len(args))
	for i, arg := range args {
		if arg.Name != "" {
			out[i] = sql.Named(arg.Name, arg.Value)
			continue
		}
		out[i] = arg.Value
	}
	return out
}

func valuesToNamed(args []driver.Value) []driver.NamedValue {
	out := make([]driver.NamedValue, len(args))
	for i, arg := range args {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: arg}
	}
	return out
}

func NormalizeBackend(backend string) string {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "", BackendCapture:
		return BackendCapture
	case BackendDuckDB:
		return BackendDuckDB
	default:
		return BackendCapture
	}
}

func NormalizeFallback(fallback string) string {
	switch strings.ToLower(strings.TrimSpace(fallback)) {
	case "", FallbackPlaceholderOnError:
		return FallbackPlaceholderOnError
	case FallbackStrict:
		return FallbackStrict
	default:
		return FallbackPlaceholderOnError
	}
}

func NormalizeDiscovery(discovery string) string {
	switch strings.ToLower(strings.TrimSpace(discovery)) {
	case "", DiscoveryInformationSchema:
		return DiscoveryInformationSchema
	case DiscoveryShow:
		return DiscoveryShow
	default:
		return DiscoveryInformationSchema
	}
}

func openDuckDB(seedSQL string, catalog any, adapter Adapter, dbPath string) (*sql.DB, error) {
	if err := ensureDuckDBDriver(); err != nil {
		return nil, err
	}

	initialized := false
	if dbPath != "" {
		if st, err := os.Stat(dbPath); err == nil && st.Size() > 0 {
			initialized = true
		}
	}

	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		return nil, fmt.Errorf("duckdb open: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if initialized {
		return db, nil
	}
	stmts, err := adapter.TranslateSetup(seedSQL, catalog)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("duckdb translate setup: %w", err)
	}
	for _, stmt := range stmts {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("duckdb setup exec: %w\nSQL: %s", err, stmt)
		}
	}
	return db, nil
}

func driverRowsFromSQLRows(sqlRows *sql.Rows) (*Rows, string, error) {
	cols, err := sqlRows.Columns()
	if err != nil {
		return nil, "", err
	}
	var vals [][]driver.Value
	for sqlRows.Next() {
		scanVals := make([]any, len(cols))
		dest := make([]any, len(cols))
		for i := range scanVals {
			dest[i] = &scanVals[i]
		}
		if err := sqlRows.Scan(dest...); err != nil {
			return nil, "", err
		}
		row := make([]driver.Value, len(cols))
		for i, val := range scanVals {
			row[i] = driverValue(val)
		}
		vals = append(vals, row)
	}
	if err := sqlRows.Err(); err != nil {
		return nil, "", err
	}
	return NewRows(cols, vals...), fmt.Sprintf("rows:%d", len(vals)), nil
}

func driverValue(v any) driver.Value {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		return t
	case string:
		return t
	case bool:
		return t
	case int64:
		return t
	case int:
		return int64(t)
	case int32:
		return int64(t)
	case float64:
		return t
	case float32:
		return float64(t)
	case time.Time:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func sanitizeFileName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "hostedemu"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		return "hostedemu"
	}
	return out
}

var _ driver.Driver = (*drv)(nil)
var _ driver.Connector = (*connector)(nil)
var _ driver.Conn = (*conn)(nil)
var _ driver.ConnBeginTx = (*conn)(nil)
var _ driver.ExecerContext = (*conn)(nil)
var _ driver.QueryerContext = (*conn)(nil)
var _ driver.Pinger = (*conn)(nil)
var _ driver.NamedValueChecker = (*conn)(nil)
