package bigquerylive

import (
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dosco/graphjin/tests/v3/hostedemu"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	bq "google.golang.org/api/bigquery/v2"
	"google.golang.org/api/option"
)

type Config struct {
	ProjectID  string
	DatasetID  string
	Location   string
	CaptureDir string
	TestName   string
	RunID      string
	TableRows  map[string]uint64
}

type connector struct {
	state *state
}

type state struct {
	svc       *bq.Service
	projectID string
	datasetID string
	location  string
	mu        sync.Mutex
	seq       int64
	file      *os.File
	testName  string
	runID     string
	rowsMu    sync.Mutex
	tableRows map[string]uint64
}

type conn struct {
	state *state
}

type stmt struct {
	conn  *conn
	query string
}

type tx struct{}

type resultInfo int64

type rows struct {
	cols []string
	vals [][]driver.Value
	pos  int
}

func NewConnector(ctx context.Context, conf Config) (driver.Connector, error) {
	if strings.TrimSpace(conf.ProjectID) == "" {
		return nil, fmt.Errorf("bigquery live: project id is required")
	}
	if strings.TrimSpace(conf.DatasetID) == "" {
		return nil, fmt.Errorf("bigquery live: dataset id is required")
	}
	if strings.TrimSpace(conf.Location) == "" {
		conf.Location = "US"
	}
	if strings.TrimSpace(conf.TestName) == "" {
		conf.TestName = "bigquery-live"
	}
	if strings.TrimSpace(conf.RunID) == "" {
		conf.RunID = time.Now().UTC().Format("20060102T150405.000000000Z")
	}
	svc, err := NewService(ctx)
	if err != nil {
		return nil, err
	}
	var file *os.File
	if conf.CaptureDir != "" {
		if err := os.MkdirAll(conf.CaptureDir, 0755); err != nil {
			return nil, err
		}
		file, err = os.OpenFile(hostedemu.CapturePath(conf.CaptureDir, conf.TestName), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if err != nil {
			return nil, err
		}
	}
	return &connector{state: &state{
		svc:       svc,
		projectID: conf.ProjectID,
		datasetID: conf.DatasetID,
		location:  conf.Location,
		file:      file,
		testName:  conf.TestName,
		runID:     conf.RunID,
		tableRows: seedTableRows(conf.DatasetID, conf.TableRows),
	}}, nil
}

func seedTableRows(datasetID string, rows map[string]uint64) map[string]uint64 {
	out := make(map[string]uint64, len(rows))
	for table, n := range rows {
		table = strings.ToLower(strings.TrimSpace(strings.Trim(table, "`")))
		if table == "" {
			continue
		}
		if strings.Contains(table, ".") {
			out[table] = n
			continue
		}
		out[strings.ToLower(datasetID)+"."+table] = n
	}
	return out
}

func NewService(ctx context.Context) (*bq.Service, error) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("GRAPHJIN_BIGQUERY_AUTH")), "gcloud") {
		return newGcloudService(ctx)
	}
	if _, err := google.FindDefaultCredentials(ctx, bq.BigqueryScope); err == nil {
		return bq.NewService(ctx, option.WithScopes(bq.BigqueryScope))
	}
	return newGcloudService(ctx)
}

func newGcloudService(ctx context.Context) (*bq.Service, error) {
	client := oauth2.NewClient(ctx, &gcloudTokenSource{
		account: strings.TrimSpace(os.Getenv("GRAPHJIN_BIGQUERY_ACCOUNT")),
	})
	return bq.NewService(ctx, option.WithHTTPClient(client))
}

func (c *connector) Connect(context.Context) (driver.Conn, error) {
	return &conn{state: c.state}, nil
}

func (c *connector) Driver() driver.Driver {
	return &drv{state: c.state}
}

type drv struct {
	state *state
}

func (d *drv) Open(string) (driver.Conn, error) {
	return &conn{state: d.state}, nil
}

func (c *conn) Prepare(query string) (driver.Stmt, error) {
	return &stmt{conn: c, query: query}, nil
}

func (c *conn) Close() error { return nil }

func (c *conn) Begin() (driver.Tx, error) { return tx{}, nil }

func (c *conn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return tx{}, nil
}

func (c *conn) Ping(ctx context.Context) error {
	return c.state.ping(ctx)
}

func (c *conn) CheckNamedValue(*driver.NamedValue) error { return nil }

func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	res, err := c.state.run(ctx, query, args)
	result := "ok"
	if res != nil && res.NumDmlAffectedRows != 0 {
		result = "rows_affected:" + strconv.FormatInt(res.NumDmlAffectedRows, 10)
	}
	c.state.capture("exec", classifyPhase(query), query, args, result, err)
	if err != nil {
		return nil, err
	}
	if res.NumDmlAffectedRows != 0 {
		return resultInfo(res.NumDmlAffectedRows), nil
	}
	return resultInfo(1), nil
}

func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	res, err := c.state.run(ctx, query, args)
	result := "rows:0"
	if res != nil {
		result = "rows:" + strconv.Itoa(len(res.Rows))
	}
	c.state.capture("query", classifyPhase(query), query, args, result, err)
	if err != nil {
		return nil, err
	}
	return rowsFromResponse(res), nil
}

func (s *stmt) Close() error { return nil }

func (s *stmt) NumInput() int { return -1 }

func (s *stmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.conn.ExecContext(context.Background(), s.query, valuesToNamed(args))
}

func (s *stmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.conn.QueryContext(context.Background(), s.query, valuesToNamed(args))
}

func (tx) Commit() error   { return nil }
func (tx) Rollback() error { return nil }

func (r resultInfo) LastInsertId() (int64, error) { return 0, nil }
func (r resultInfo) RowsAffected() (int64, error) { return int64(r), nil }

func (r *rows) Columns() []string { return r.cols }

func (r *rows) Close() error { return nil }

func (r *rows) Next(dest []driver.Value) error {
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

func (s *state) ping(ctx context.Context) error {
	_, err := s.svc.Datasets.Get(s.projectID, s.datasetID).Context(ctx).Do()
	return err
}

func (s *state) run(ctx context.Context, query string, args []driver.NamedValue) (*bq.QueryResponse, error) {
	query = s.rewriteQuery(query)
	if res, handled, err := s.tableStorageResponse(ctx, query, args); handled {
		if err != nil {
			return nil, fmt.Errorf("bigquery table metadata: %w\nSQL: %s", err, query)
		}
		return res, nil
	}
	params, mode := queryParameters(args)
	useLegacy := false
	req := &bq.QueryRequest{
		Query:        query,
		UseLegacySql: &useLegacy,
		DefaultDataset: &bq.DatasetReference{
			ProjectId: s.projectID,
			DatasetId: s.datasetID,
		},
		Location:           s.location,
		MaxResults:         10000,
		TimeoutMs:          30000,
		UseQueryCache:      boolPtr(false),
		QueryParameters:    params,
		ParameterMode:      mode,
		MaximumBytesBilled: 1000000000,
		Labels: map[string]string{
			"graphjin": "bigquery_live_test",
		},
	}
	res, err := s.svc.Jobs.Query(s.projectID, req).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("bigquery query: %w\nSQL: %s", err, query)
	}
	if err := firstQueryError(res.Errors); err != nil {
		return nil, fmt.Errorf("bigquery query: %w\nSQL: %s", err, query)
	}
	if res.JobComplete {
		return s.readAllPages(ctx, res)
	}
	if res.JobReference == nil {
		return res, nil
	}
	for {
		gr, err := s.svc.Jobs.GetQueryResults(res.JobReference.ProjectId, res.JobReference.JobId).
			Location(s.location).
			MaxResults(10000).
			Context(ctx).
			Do()
		if err != nil {
			return nil, fmt.Errorf("bigquery get results: %w\nSQL: %s", err, query)
		}
		if err := firstQueryError(gr.Errors); err != nil {
			return nil, fmt.Errorf("bigquery get results: %w\nSQL: %s", err, query)
		}
		if gr.JobComplete {
			return s.queryResponseFromGet(ctx, gr, query)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (s *state) readAllPages(ctx context.Context, res *bq.QueryResponse) (*bq.QueryResponse, error) {
	if res.JobReference == nil || res.PageToken == "" {
		return res, nil
	}
	token := res.PageToken
	for token != "" {
		gr, err := s.svc.Jobs.GetQueryResults(res.JobReference.ProjectId, res.JobReference.JobId).
			Location(s.location).
			PageToken(token).
			MaxResults(10000).
			Context(ctx).
			Do()
		if err != nil {
			return nil, err
		}
		res.Rows = append(res.Rows, gr.Rows...)
		token = gr.PageToken
	}
	return res, nil
}

func (s *state) queryResponseFromGet(ctx context.Context, gr *bq.GetQueryResultsResponse, query string) (*bq.QueryResponse, error) {
	res := &bq.QueryResponse{
		JobComplete:        gr.JobComplete,
		JobReference:       gr.JobReference,
		Rows:               gr.Rows,
		Schema:             gr.Schema,
		NumDmlAffectedRows: gr.NumDmlAffectedRows,
		PageToken:          gr.PageToken,
	}
	if gr.JobReference == nil || gr.PageToken == "" {
		return res, nil
	}
	token := gr.PageToken
	for token != "" {
		next, err := s.svc.Jobs.GetQueryResults(gr.JobReference.ProjectId, gr.JobReference.JobId).
			Location(s.location).
			PageToken(token).
			MaxResults(10000).
			Context(ctx).
			Do()
		if err != nil {
			return nil, fmt.Errorf("bigquery get results page: %w\nSQL: %s", err, query)
		}
		res.Rows = append(res.Rows, next.Rows...)
		token = next.PageToken
	}
	return res, nil
}

func (s *state) rewriteQuery(query string) string {
	repls := []struct {
		from string
		to   string
	}{
		{`(?i)\binformation_schema\.table_storage\b`, s.regionInfoSchema("TABLE_STORAGE")},
		{`(?i)\binformation_schema\.columns\b`, s.infoSchema("COLUMNS")},
		{`(?i)\binformation_schema\.key_column_usage\b`, s.infoSchema("KEY_COLUMN_USAGE")},
		{`(?i)\binformation_schema\.table_constraints\b`, s.infoSchema("TABLE_CONSTRAINTS")},
		{`(?i)\binformation_schema\.constraint_column_usage\b`, s.infoSchema("CONSTRAINT_COLUMN_USAGE")},
		{`(?i)\binformation_schema\.tables\b`, s.infoSchema("TABLES")},
	}
	out := query
	for _, repl := range repls {
		out = regexp.MustCompile(repl.from).ReplaceAllString(out, repl.to)
	}
	out = regexp.MustCompile(`(?i)COALESCE\(@@dataset_id,\s*''\)`).ReplaceAllString(out, quoteSQL(s.datasetID))
	out = regexp.MustCompile(`(?i)COALESCE\(@@project_id,\s*''\)`).ReplaceAllString(out, quoteSQL(s.projectID))
	return out
}

func (s *state) infoSchema(view string) string {
	return "`" + strings.ReplaceAll(s.projectID, "`", "") + "." + strings.ReplaceAll(s.datasetID, "`", "") + ".INFORMATION_SCHEMA." + view + "`"
}

func (s *state) regionInfoSchema(view string) string {
	region := "region-" + strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s.location), "_", "-"))
	return "`" + strings.ReplaceAll(s.projectID, "`", "") + "." + strings.ReplaceAll(region, "`", "") + ".INFORMATION_SCHEMA." + view + "`"
}

func (s *state) tableStorageResponse(ctx context.Context, query string, args []driver.NamedValue) (*bq.QueryResponse, bool, error) {
	upper := strings.ToUpper(hostedemu.NormalizeSQL(query))
	if !strings.Contains(upper, "INFORMATION_SCHEMA.TABLE_STORAGE") {
		return nil, false, nil
	}
	datasetID := s.datasetID
	if strings.Contains(upper, "TABLE_SCHEMA") {
		datasetID = datasetArg(args, s.datasetID)
	}
	if strings.Contains(upper, "GROUP BY TABLE_SCHEMA") || strings.Contains(upper, "COUNT(*) AS TABLE_COUNT") {
		res, err := s.tableStorageNamespaceRows(datasetID)
		return res, true, err
	}
	if strings.Contains(upper, "SELECT TABLE_NAME") {
		res, err := s.tableStorageRows(ctx, datasetID)
		return res, true, err
	}
	res, err := s.tableStorageRow(ctx, datasetID, lastStringArg(args))
	return res, true, err
}

func (s *state) tableStorageRows(_ context.Context, datasetID string) (*bq.QueryResponse, error) {
	res := &bq.QueryResponse{
		JobComplete: true,
		Schema: &bq.TableSchema{Fields: []*bq.TableFieldSchema{
			{Name: "table_name", Type: "STRING"},
			{Name: "total_rows", Type: "INT64"},
		}},
	}
	prefix := strings.ToLower(datasetID) + "."
	s.rowsMu.Lock()
	defer s.rowsMu.Unlock()
	for key, n := range s.tableRows {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		tableID := strings.TrimPrefix(key, prefix)
		res.Rows = append(res.Rows, &bq.TableRow{F: []*bq.TableCell{
			{V: tableID},
			{V: strconv.FormatUint(n, 10)},
		}})
	}
	return res, nil
}

func (s *state) tableStorageNamespaceRows(datasetID string) (*bq.QueryResponse, error) {
	res := &bq.QueryResponse{
		JobComplete: true,
		Schema: &bq.TableSchema{Fields: []*bq.TableFieldSchema{
			{Name: "db", Type: "STRING"},
			{Name: "sch", Type: "STRING"},
			{Name: "table_count", Type: "INT64"},
			{Name: "approx_row_total", Type: "INT64"},
		}},
	}
	prefix := strings.ToLower(datasetID) + "."
	var tableCount uint64
	var rowTotal uint64
	s.rowsMu.Lock()
	defer s.rowsMu.Unlock()
	for key, n := range s.tableRows {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		tableCount++
		rowTotal += n
	}
	if tableCount == 0 {
		return res, nil
	}
	res.Rows = append(res.Rows, &bq.TableRow{F: []*bq.TableCell{
		{V: s.projectID},
		{V: strings.ToLower(datasetID)},
		{V: strconv.FormatUint(tableCount, 10)},
		{V: strconv.FormatUint(rowTotal, 10)},
	}})
	return res, nil
}

func (s *state) tableStorageRow(_ context.Context, datasetID, tableID string) (*bq.QueryResponse, error) {
	res := &bq.QueryResponse{
		JobComplete: true,
		Schema: &bq.TableSchema{Fields: []*bq.TableFieldSchema{
			{Name: "total_rows", Type: "INT64"},
		}},
	}
	if tableID == "" {
		return res, nil
	}
	n, ok := s.lookupTableRows(datasetID, tableID)
	if !ok {
		return res, nil
	}
	res.Rows = append(res.Rows, &bq.TableRow{F: []*bq.TableCell{
		{V: strconv.FormatUint(n, 10)},
	}})
	return res, nil
}

func (s *state) lookupTableRows(datasetID, tableID string) (uint64, bool) {
	key := strings.ToLower(strings.TrimSpace(datasetID)) + "." + strings.ToLower(strings.TrimSpace(tableID))
	s.rowsMu.Lock()
	defer s.rowsMu.Unlock()
	if n, ok := s.tableRows[key]; ok {
		return n, true
	}
	return 0, false
}

func datasetArg(args []driver.NamedValue, fallback string) string {
	if len(args) == 0 {
		return fallback
	}
	if s, ok := args[0].Value.(string); ok && s != "" {
		return s
	}
	return fallback
}

func lastStringArg(args []driver.NamedValue) string {
	if len(args) == 0 {
		return ""
	}
	if s, ok := args[len(args)-1].Value.(string); ok {
		return s
	}
	return fmt.Sprint(args[len(args)-1].Value)
}

type captureEvent struct {
	RunID         string `json:"run_id"`
	Test          string `json:"test"`
	Seq           int64  `json:"seq"`
	Op            string `json:"op"`
	Phase         string `json:"phase"`
	SQL           string `json:"sql"`
	Args          []any  `json:"args"`
	NormalizedSQL string `json:"normalized_sql"`
	SQLHash       string `json:"sql_hash"`
	Backend       string `json:"backend,omitempty"`
	Result        string `json:"result"`
	Error         string `json:"error,omitempty"`
}

func (s *state) capture(op, phase, query string, args []driver.NamedValue, result string, err error) {
	if s == nil || s.file == nil {
		return
	}
	norm := hostedemu.NormalizeSQL(query)
	sum := sha256.Sum256([]byte(norm))
	ev := captureEvent{
		RunID:         s.runID,
		Test:          s.testName,
		Op:            op,
		Phase:         phase,
		SQL:           query,
		Args:          hostedemu.NamedValues(args),
		NormalizedSQL: norm,
		SQLHash:       hex.EncodeToString(sum[:]),
		Backend:       "live",
		Result:        result,
	}
	if err != nil {
		ev.Error = err.Error()
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

func classifyPhase(sql string) string {
	upper := strings.ToUpper(hostedemu.NormalizeSQL(sql))
	switch {
	case strings.Contains(upper, "INFORMATION_SCHEMA") ||
		strings.Contains(upper, "@@DATASET_ID") ||
		strings.Contains(upper, "@@PROJECT_ID"):
		return "discovery"
	case strings.Contains(upper, "JSON_OBJECT") ||
		strings.Contains(upper, "ARRAY_AGG") ||
		strings.Contains(upper, "_GJ_IDS") ||
		strings.Contains(upper, "PARSE_JSON("):
		return "runtime"
	case strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "WITH") ||
		strings.HasPrefix(upper, "INSERT") || strings.HasPrefix(upper, "UPDATE") ||
		strings.HasPrefix(upper, "DELETE") || strings.HasPrefix(upper, "CREATE") ||
		strings.HasPrefix(upper, "DROP") || strings.HasPrefix(upper, "ALTER"):
		return "direct"
	default:
		return "unknown"
	}
}

func queryParameters(args []driver.NamedValue) ([]*bq.QueryParameter, string) {
	if len(args) == 0 {
		return nil, ""
	}
	mode := "POSITIONAL"
	params := make([]*bq.QueryParameter, 0, len(args))
	for _, arg := range args {
		p := queryParameter(arg.Value)
		if arg.Name != "" {
			mode = "NAMED"
			p.Name = arg.Name
		}
		params = append(params, p)
	}
	return params, mode
}

func queryParameter(v any) *bq.QueryParameter {
	typ, val := queryParameterValue(v)
	return &bq.QueryParameter{
		ParameterType:  typ,
		ParameterValue: val,
	}
}

func queryParameterValue(v any) (*bq.QueryParameterType, *bq.QueryParameterValue) {
	switch t := v.(type) {
	case nil:
		return &bq.QueryParameterType{Type: "STRING"}, &bq.QueryParameterValue{NullFields: []string{"Value"}}
	case bool:
		return &bq.QueryParameterType{Type: "BOOL"}, &bq.QueryParameterValue{Value: strconv.FormatBool(t)}
	case int:
		return &bq.QueryParameterType{Type: "INT64"}, &bq.QueryParameterValue{Value: strconv.FormatInt(int64(t), 10)}
	case int32:
		return &bq.QueryParameterType{Type: "INT64"}, &bq.QueryParameterValue{Value: strconv.FormatInt(int64(t), 10)}
	case int64:
		return &bq.QueryParameterType{Type: "INT64"}, &bq.QueryParameterValue{Value: strconv.FormatInt(t, 10)}
	case float32:
		return &bq.QueryParameterType{Type: "FLOAT64"}, &bq.QueryParameterValue{Value: strconv.FormatFloat(float64(t), 'f', -1, 64)}
	case float64:
		return &bq.QueryParameterType{Type: "FLOAT64"}, &bq.QueryParameterValue{Value: strconv.FormatFloat(t, 'f', -1, 64)}
	case []byte:
		return &bq.QueryParameterType{Type: "STRING"}, &bq.QueryParameterValue{Value: string(t)}
	case string:
		return &bq.QueryParameterType{Type: "STRING"}, &bq.QueryParameterValue{Value: t}
	case time.Time:
		return &bq.QueryParameterType{Type: "TIMESTAMP"}, &bq.QueryParameterValue{Value: t.Format(time.RFC3339Nano)}
	default:
		data, _ := json.Marshal(t)
		return &bq.QueryParameterType{Type: "STRING"}, &bq.QueryParameterValue{Value: string(data)}
	}
}

func rowsFromResponse(res *bq.QueryResponse) *rows {
	var cols []string
	if res != nil && res.Schema != nil {
		for _, field := range res.Schema.Fields {
			cols = append(cols, field.Name)
		}
	}
	vals := make([][]driver.Value, 0, len(res.Rows))
	for _, row := range res.Rows {
		out := make([]driver.Value, len(cols))
		for i, cell := range row.F {
			if i < len(out) {
				out[i] = driverValue(cell.V)
			}
		}
		vals = append(vals, out)
	}
	return &rows{cols: cols, vals: vals}
}

func driverValue(v any) driver.Value {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		if i, err := strconv.ParseInt(t, 10, 64); err == nil {
			return i
		}
		if f, err := strconv.ParseFloat(t, 64); err == nil && strings.ContainsAny(t, ".eE") {
			return f
		}
		return t
	case bool:
		return t
	case float64:
		return t
	case int64:
		return t
	case []byte:
		return t
	default:
		data, err := json.Marshal(t)
		if err == nil {
			return string(data)
		}
		return fmt.Sprint(t)
	}
}

func firstQueryError(errs []*bq.ErrorProto) error {
	if len(errs) == 0 {
		return nil
	}
	e := errs[0]
	return fmt.Errorf("%s: %s", e.Reason, e.Message)
}

func valuesToNamed(args []driver.Value) []driver.NamedValue {
	out := make([]driver.NamedValue, len(args))
	for i, arg := range args {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: arg}
	}
	return out
}

func boolPtr(v bool) *bool {
	return &v
}

func quoteSQL(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

type gcloudTokenSource struct {
	account string
	mu      sync.Mutex
	token   *oauth2.Token
}

func (s *gcloudTokenSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != nil && s.token.Valid() {
		return s.token, nil
	}
	args := []string{"auth", "print-access-token"}
	if s.account != "" {
		args = append(args, "--account", s.account)
	}
	out, err := exec.Command("gcloud", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gcloud auth print-access-token: %w: %s", err, strings.TrimSpace(string(out)))
	}
	s.token = &oauth2.Token{
		AccessToken: strings.TrimSpace(string(out)),
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(50 * time.Minute),
	}
	return s.token, nil
}

var _ driver.Driver = (*drv)(nil)
var _ driver.Connector = (*connector)(nil)
var _ driver.Conn = (*conn)(nil)
var _ driver.ConnBeginTx = (*conn)(nil)
var _ driver.ExecerContext = (*conn)(nil)
var _ driver.QueryerContext = (*conn)(nil)
var _ driver.Pinger = (*conn)(nil)
var _ driver.NamedValueChecker = (*conn)(nil)
