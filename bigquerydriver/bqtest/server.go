// Package bqtest is an HTTP stand-in for the BigQuery REST API, for tests of
// the BigQuery driver and of the SQL GraphJin sends through it.
package bqtest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	bq "google.golang.org/api/bigquery/v2"
)

// Server is the stand-in. It does not run SQL; a test scripts what a statement
// returns. What it does enforce are the request rules the real service
// enforces, each from BigQuery's documentation, so a request the real service
// would refuse is refused here too:
//
//   - GoogleSQL only: useLegacySql must be sent, and be false.
//   - Every `?` needs a parameter and parameterMode POSITIONAL.
//   - INFORMATION_SCHEMA views must carry a dataset or region qualifier.
//   - A job lives in one location; asking another location for it is "not found".
//   - Scalars travel as text; TIMESTAMP is float seconds unless int64 is asked for.
type Server struct {
	// Project, Dataset and Location are the one dataset the server knows.
	Project  string
	Dataset  string
	Location string

	t   testing.TB
	url string

	mu        sync.Mutex
	scripts   []script
	jobs      map[string]*fakeJob
	queries   []map[string]any // decoded jobs.query bodies, in order
	pageCalls []url.Values     // getQueryResults query strings, in order
	cancelled []string
	refused   []Refusal
	seq       int
}

// Refusal is a request the server turned down, as BigQuery would have.
type Refusal struct {
	SQL     string
	Message string
}

type script struct {
	match func(sql string) bool
	reply Reply
}

// Reply is what a scripted statement returns. Row cells are Go values (int64,
// float64, bool, string, time.Time, []any for arrays and structs, nil); the
// server encodes them the way the REST API does.
type Reply struct {
	Fields       []*bq.TableFieldSchema
	Rows         [][]any
	PageSize     int   // rows per page; 0 means one page
	PendingPolls int   // times the job reports "still running" before finishing
	Block        bool  // the job never finishes; results wait for the client to give up
	DMLRows      int64 // numDmlAffectedRows
	Fail         *Failure
}

// Failure is an API error a scripted statement returns.
type Failure struct {
	Code    int
	Reason  string
	Message string
}

// Field describes a nullable scalar column.
func Field(name, typ string) *bq.TableFieldSchema {
	return &bq.TableFieldSchema{Name: name, Type: typ, Mode: "NULLABLE"}
}

type fakeJob struct {
	id    string
	reply Reply
	polls int
}

// New starts a server holding the dataset acme-prod.analytics in
// europe-west2, and stops it when the test ends.
func New(t testing.TB) *Server {
	t.Helper()
	f := &Server{
		Project:  "acme-prod",
		Dataset:  "analytics",
		Location: "europe-west2",
		t:        t,
		jobs:     map[string]*fakeJob{},
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	f.url = srv.URL
	return f
}

// Endpoint is the base URL to give the BigQuery client.
func (f *Server) Endpoint() string { return f.url + "/bigquery/v2/" }

// On scripts the reply to the first statement match accepts. Scripts are tried
// in the order they were added.
func (f *Server) On(match func(sql string) bool, r Reply) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scripts = append(f.scripts, script{match: match, reply: r})
}

// OnContains scripts the reply to a statement containing substr.
func (f *Server) OnContains(substr string, r Reply) {
	f.On(func(sql string) bool { return strings.Contains(sql, substr) }, r)
}

// Queries returns the decoded body of every statement received, in order.
func (f *Server) Queries() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.queries...)
}

// LastQuery returns the decoded body of the latest statement received.
func (f *Server) LastQuery(t testing.TB) map[string]any {
	t.Helper()
	q := f.Queries()
	if len(q) == 0 {
		t.Fatal("no query reached the BigQuery stand-in")
	}
	return q[len(q)-1]
}

// ResultCalls is how many times a client asked for a job's results.
func (f *Server) ResultCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.pageCalls)
}

// Cancelled returns the ids of the jobs a client cancelled.
func (f *Server) Cancelled() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.cancelled...)
}

// Refused returns every statement the server turned down, including ones
// whose error the caller chose to ignore.
func (f *Server) Refused() []Refusal {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Refusal(nil), f.refused...)
}

func (f *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rest, ok := strings.CutPrefix(r.URL.Path, "/bigquery/v2/projects/")
	if !ok {
		f.fail(w, 404, "notFound", "Not found: "+r.URL.Path)
		return
	}
	parts := strings.Split(rest, "/")
	if parts[0] != f.Project {
		f.fail(w, 404, "notFound", "Not found: Project "+parts[0])
		return
	}
	switch {
	case r.Method == http.MethodGet && len(parts) == 3 && parts[1] == "datasets":
		f.serveDataset(w, parts[2])
	case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "queries":
		f.serveQuery(w, r)
	case r.Method == http.MethodGet && len(parts) == 3 && parts[1] == "queries":
		f.serveResults(w, r, parts[2])
	case r.Method == http.MethodPost && len(parts) == 4 && parts[1] == "jobs" && parts[3] == "cancel":
		f.mu.Lock()
		f.cancelled = append(f.cancelled, parts[2])
		f.mu.Unlock()
		f.json(w, map[string]any{"kind": "bigquery#jobCancelResponse"})
	default:
		f.fail(w, 404, "notFound", "Not found: "+r.Method+" "+r.URL.Path)
	}
}

func (f *Server) serveDataset(w http.ResponseWriter, dataset string) {
	if dataset != f.Dataset {
		f.fail(w, 404, "notFound", fmt.Sprintf("Not found: Dataset %s:%s", f.Project, dataset))
		return
	}
	f.json(w, map[string]any{
		"kind":             "bigquery#dataset",
		"id":               f.Project + ":" + f.Dataset,
		"datasetReference": map[string]any{"projectId": f.Project, "datasetId": f.Dataset},
		"location":         f.Location,
	})
}

var (
	stringLiteral  = regexp.MustCompile(`'(?:[^'\\]|\\.)*'|"(?:[^"\\]|\\.)*"`)
	quotedIdent    = regexp.MustCompile("`[^`]*`")
	bareInfoSchema = regexp.MustCompile(`(?i)(^|[^.])\binformation_schema\.`)
)

func (f *Server) serveQuery(w http.ResponseWriter, r *http.Request) {
	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		f.fail(w, 400, "parseError", "Could not parse request body: "+err.Error())
		return
	}
	f.mu.Lock()
	f.queries = append(f.queries, raw)
	f.mu.Unlock()

	sqlText, _ := raw["query"].(string)
	if legacy, sent := raw["useLegacySql"].(bool); !sent || legacy {
		f.refuse(w, sqlText, 400, "invalidQuery", "this query is GoogleSQL; legacy SQL is the API default and was not turned off")
		return
	}
	if loc, _ := raw["location"].(string); loc != "" && !strings.EqualFold(loc, f.Location) {
		f.refuse(w, sqlText, 404, "notFound", fmt.Sprintf("Not found: Dataset %s:%s was not found in location %s", f.Project, f.Dataset, loc))
		return
	}

	code := stringLiteral.ReplaceAllString(sqlText, "''")
	if bareInfoSchema.MatchString(code) {
		f.refuse(w, sqlText, 400, "invalidQuery", "INFORMATION_SCHEMA view needs to be qualified with a dataset or region")
		return
	}
	params, _ := raw["queryParameters"].([]any)
	placeholders := strings.Count(quotedIdent.ReplaceAllString(code, ""), "?")
	if placeholders != len(params) {
		f.refuse(w, sqlText, 400, "invalidQuery", fmt.Sprintf("Query has %d positional parameter(s) but %d value(s) were supplied", placeholders, len(params)))
		return
	}
	if placeholders > 0 && raw["parameterMode"] != "POSITIONAL" {
		f.refuse(w, sqlText, 400, "invalidQuery", "Positional parameters require parameterMode POSITIONAL")
		return
	}
	for i, p := range params {
		typ, _ := p.(map[string]any)["parameterType"].(map[string]any)
		if name, _ := typ["type"].(string); name == "" {
			f.refuse(w, sqlText, 400, "invalid", fmt.Sprintf("Parameter %d has no type", i+1))
			return
		}
	}

	f.mu.Lock()
	var found *Reply
	for i := range f.scripts {
		if f.scripts[i].match(sqlText) {
			found = &f.scripts[i].reply
			break
		}
	}
	if found == nil {
		f.mu.Unlock()
		f.refuse(w, sqlText, 400, "invalidQuery", "no reply is scripted for: "+sqlText)
		return
	}
	if found.Fail != nil {
		f.mu.Unlock()
		f.fail(w, found.Fail.Code, found.Fail.Reason, found.Fail.Message)
		return
	}
	f.seq++
	job := &fakeJob{id: "job_" + strconv.Itoa(f.seq), reply: *found}
	f.jobs[job.id] = job
	f.mu.Unlock()

	int64Timestamps := false
	if opts, ok := raw["formatOptions"].(map[string]any); ok {
		int64Timestamps, _ = opts["useInt64Timestamp"].(bool)
	}
	if job.reply.Block || job.reply.PendingPolls > 0 {
		f.json(w, map[string]any{
			"kind":         "bigquery#queryResponse",
			"jobReference": f.jobRef(job),
			"jobComplete":  false,
		})
		return
	}
	f.json(w, f.resultPage(job, 0, int64Timestamps, "bigquery#queryResponse"))
}

func (f *Server) serveResults(w http.ResponseWriter, r *http.Request, jobID string) {
	q := r.URL.Query()
	f.mu.Lock()
	f.pageCalls = append(f.pageCalls, q)
	job := f.jobs[jobID]
	f.mu.Unlock()

	// A job outside the US and EU multi-regions is only found in its own location.
	if job == nil || !strings.EqualFold(q.Get("location"), f.Location) {
		f.fail(w, 404, "notFound", fmt.Sprintf("Not found: Job %s:%s", f.Project, jobID))
		return
	}
	if job.reply.Block {
		<-r.Context().Done()
		return
	}
	f.mu.Lock()
	pending := job.polls < job.reply.PendingPolls
	job.polls++
	f.mu.Unlock()
	if pending {
		f.json(w, map[string]any{
			"kind":         "bigquery#getQueryResultsResponse",
			"jobReference": f.jobRef(job),
			"jobComplete":  false,
		})
		return
	}
	offset := 0
	if token := q.Get("pageToken"); token != "" {
		n, err := strconv.Atoi(token)
		if err != nil {
			f.fail(w, 400, "invalid", "Invalid page token")
			return
		}
		offset = n
	}
	int64Timestamps := q.Get("formatOptions.useInt64Timestamp") == "true"
	f.json(w, f.resultPage(job, offset, int64Timestamps, "bigquery#getQueryResultsResponse"))
}

func (f *Server) jobRef(job *fakeJob) map[string]any {
	return map[string]any{"projectId": f.Project, "jobId": job.id, "location": f.Location}
}

func (f *Server) resultPage(job *fakeJob, offset int, int64Timestamps bool, kind string) map[string]any {
	rep := job.reply
	end := len(rep.Rows)
	if rep.PageSize > 0 && offset+rep.PageSize < end {
		end = offset + rep.PageSize
	}
	rows := make([]any, 0, end-offset)
	for _, row := range rep.Rows[offset:end] {
		cells := make([]any, len(rep.Fields))
		for i, field := range rep.Fields {
			cells[i] = map[string]any{"v": wireValue(field, row[i], int64Timestamps, false)}
		}
		rows = append(rows, map[string]any{"f": cells})
	}
	out := map[string]any{
		"kind":         kind,
		"jobReference": f.jobRef(job),
		"jobComplete":  true,
		"totalRows":    strconv.Itoa(len(rep.Rows)),
		"schema":       map[string]any{"fields": rep.Fields},
		"rows":         rows,
	}
	if end < len(rep.Rows) {
		out["pageToken"] = strconv.Itoa(end)
	}
	if rep.DMLRows != 0 {
		out["numDmlAffectedRows"] = strconv.FormatInt(rep.DMLRows, 10)
	}
	return out
}

// wireValue encodes a cell as the REST API does: scalars as text, arrays as a
// list of {"v": ...}, structs as {"f": [{"v": ...}]}.
func wireValue(f *bq.TableFieldSchema, v any, int64Timestamps, element bool) any {
	if v == nil {
		return nil
	}
	if f.Mode == "REPEATED" && !element {
		items := v.([]any)
		out := make([]any, len(items))
		for i, item := range items {
			out[i] = map[string]any{"v": wireValue(f, item, int64Timestamps, true)}
		}
		return out
	}
	if f.Type == "RECORD" {
		vals := v.([]any)
		cells := make([]any, len(f.Fields))
		for i, sub := range f.Fields {
			cells[i] = map[string]any{"v": wireValue(sub, vals[i], int64Timestamps, false)}
		}
		return map[string]any{"f": cells}
	}
	switch t := v.(type) {
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case time.Time:
		if int64Timestamps {
			return strconv.FormatInt(t.UnixMicro(), 10)
		}
		return strconv.FormatFloat(float64(t.UnixMicro())/1e6, 'E', -1, 64)
	case string:
		return t
	default:
		panic(fmt.Sprintf("bqtest: unsupported cell %T", v))
	}
}

func (f *Server) json(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		f.t.Errorf("bqtest: encode response: %v", err)
	}
}

// refuse records a statement the server turns down by its own rules, then
// fails the request.
func (f *Server) refuse(w http.ResponseWriter, sqlText string, code int, reason, message string) {
	f.mu.Lock()
	f.refused = append(f.refused, Refusal{SQL: sqlText, Message: message})
	f.mu.Unlock()
	f.fail(w, code, reason, message)
}

// fail writes an error in the shape the Google API client parses.
func (f *Server) fail(w http.ResponseWriter, code int, reason, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
			"errors":  []any{map[string]any{"message": message, "domain": "global", "reason": reason}},
		},
	})
}
