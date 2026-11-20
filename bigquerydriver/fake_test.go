package bigquerydriver

import (
	"context"
	"database/sql"
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
	"google.golang.org/api/option"
)

// fakeBigQuery is an HTTP stand-in for the BigQuery REST API. It does not run
// SQL; a test scripts what a statement returns. What it does enforce are the
// request rules the real service enforces, each from BigQuery's documentation,
// so a driver request the real service would refuse is refused here too:
//
//   - GoogleSQL only: useLegacySql must be sent, and be false.
//   - Every `?` needs a parameter and parameterMode POSITIONAL.
//   - INFORMATION_SCHEMA views must carry a dataset or region qualifier.
//   - A job lives in one location; asking another location for it is "not found".
//   - Scalars travel as text; TIMESTAMP is float seconds unless int64 is asked for.
type fakeBigQuery struct {
	t        *testing.T
	project  string
	dataset  string
	location string

	mu        sync.Mutex
	scripts   []script
	jobs      map[string]*fakeJob
	queries   []map[string]any // decoded jobs.query bodies, in order
	pageCalls []url.Values     // getQueryResults query strings, in order
	cancelled []string
	seq       int
}

type script struct {
	match func(sql string) bool
	reply reply
}

// reply is what a scripted statement returns. Row cells are Go values (int64,
// float64, bool, string, time.Time, []any for arrays and structs, nil); the
// fake encodes them the way the REST API does.
type reply struct {
	fields       []*bq.TableFieldSchema
	rows         [][]any
	pageSize     int   // rows per page; 0 means one page
	pendingPolls int   // times the job reports "still running" before finishing
	block        bool  // the job never finishes; results wait for the client to give up
	dmlRows      int64 // numDmlAffectedRows
	fail         *apiFailure
}

type apiFailure struct {
	code    int
	reason  string
	message string
}

type fakeJob struct {
	id    string
	reply reply
	polls int
}

func newFakeBigQuery(t *testing.T) (*fakeBigQuery, *httptest.Server) {
	t.Helper()
	f := &fakeBigQuery{
		t:        t,
		project:  "acme-prod",
		dataset:  "analytics",
		location: "europe-west2",
		jobs:     map[string]*fakeJob{},
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

// open returns a *sql.DB on the driver, pointed at the fake.
func (f *fakeBigQuery) open(t *testing.T, srv *httptest.Server, conf Config) *sql.DB {
	t.Helper()
	if conf.ProjectID == "" {
		conf.ProjectID = f.project
	}
	if conf.DatasetID == "" {
		conf.DatasetID = f.dataset
	}
	conf.ClientOptions = []option.ClientOption{
		option.WithEndpoint(srv.URL + "/bigquery/v2/"),
		option.WithoutAuthentication(),
	}
	c, err := NewConnector(context.Background(), conf)
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}
	db := sql.OpenDB(c)
	t.Cleanup(func() { db.Close() }) //nolint:errcheck
	return db
}

func (f *fakeBigQuery) on(match func(sql string) bool, r reply) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scripts = append(f.scripts, script{match: match, reply: r})
}

func (f *fakeBigQuery) onContains(substr string, r reply) {
	f.on(func(sql string) bool { return strings.Contains(sql, substr) }, r)
}

func (f *fakeBigQuery) lastQuery(t *testing.T) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queries) == 0 {
		t.Fatal("no query reached the fake")
	}
	return f.queries[len(f.queries)-1]
}

func (f *fakeBigQuery) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rest, ok := strings.CutPrefix(r.URL.Path, "/bigquery/v2/projects/")
	if !ok {
		f.fail(w, 404, "notFound", "Not found: "+r.URL.Path)
		return
	}
	parts := strings.Split(rest, "/")
	if parts[0] != f.project {
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

func (f *fakeBigQuery) serveDataset(w http.ResponseWriter, dataset string) {
	if dataset != f.dataset {
		f.fail(w, 404, "notFound", fmt.Sprintf("Not found: Dataset %s:%s", f.project, dataset))
		return
	}
	f.json(w, map[string]any{
		"kind":             "bigquery#dataset",
		"id":               f.project + ":" + f.dataset,
		"datasetReference": map[string]any{"projectId": f.project, "datasetId": f.dataset},
		"location":         f.location,
	})
}

var (
	stringLiteral  = regexp.MustCompile(`'(?:[^'\\]|\\.)*'|"(?:[^"\\]|\\.)*"`)
	quotedIdent    = regexp.MustCompile("`[^`]*`")
	bareInfoSchema = regexp.MustCompile(`(?i)(^|[^.])\binformation_schema\.`)
)

func (f *fakeBigQuery) serveQuery(w http.ResponseWriter, r *http.Request) {
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
		f.fail(w, 400, "invalidQuery", "this query is GoogleSQL; legacy SQL is the API default and was not turned off")
		return
	}
	if loc, _ := raw["location"].(string); loc != "" && !strings.EqualFold(loc, f.location) {
		f.fail(w, 404, "notFound", fmt.Sprintf("Not found: Dataset %s:%s was not found in location %s", f.project, f.dataset, loc))
		return
	}

	code := stringLiteral.ReplaceAllString(sqlText, "''")
	if bareInfoSchema.MatchString(code) {
		f.fail(w, 400, "invalidQuery", "INFORMATION_SCHEMA view needs to be qualified with a dataset or region")
		return
	}
	params, _ := raw["queryParameters"].([]any)
	placeholders := strings.Count(quotedIdent.ReplaceAllString(code, ""), "?")
	if placeholders != len(params) {
		f.fail(w, 400, "invalidQuery", fmt.Sprintf("Query has %d positional parameter(s) but %d value(s) were supplied", placeholders, len(params)))
		return
	}
	if placeholders > 0 && raw["parameterMode"] != "POSITIONAL" {
		f.fail(w, 400, "invalidQuery", "Positional parameters require parameterMode POSITIONAL")
		return
	}
	for i, p := range params {
		typ, _ := p.(map[string]any)["parameterType"].(map[string]any)
		if name, _ := typ["type"].(string); name == "" {
			f.fail(w, 400, "invalid", fmt.Sprintf("Parameter %d has no type", i+1))
			return
		}
	}

	f.mu.Lock()
	var found *reply
	for i := range f.scripts {
		if f.scripts[i].match(sqlText) {
			found = &f.scripts[i].reply
			break
		}
	}
	if found == nil {
		f.mu.Unlock()
		f.fail(w, 400, "invalidQuery", "fake has no script for: "+sqlText)
		return
	}
	if found.fail != nil {
		f.mu.Unlock()
		f.fail(w, found.fail.code, found.fail.reason, found.fail.message)
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
	if job.reply.block || job.reply.pendingPolls > 0 {
		f.json(w, map[string]any{
			"kind":         "bigquery#queryResponse",
			"jobReference": f.jobRef(job),
			"jobComplete":  false,
		})
		return
	}
	f.json(w, f.resultPage(job, 0, int64Timestamps, "bigquery#queryResponse"))
}

func (f *fakeBigQuery) serveResults(w http.ResponseWriter, r *http.Request, jobID string) {
	q := r.URL.Query()
	f.mu.Lock()
	f.pageCalls = append(f.pageCalls, q)
	job := f.jobs[jobID]
	f.mu.Unlock()

	// A job outside the US and EU multi-regions is only found in its own location.
	if job == nil || !strings.EqualFold(q.Get("location"), f.location) {
		f.fail(w, 404, "notFound", fmt.Sprintf("Not found: Job %s:%s", f.project, jobID))
		return
	}
	if job.reply.block {
		<-r.Context().Done()
		return
	}
	f.mu.Lock()
	pending := job.polls < job.reply.pendingPolls
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

func (f *fakeBigQuery) jobRef(job *fakeJob) map[string]any {
	return map[string]any{"projectId": f.project, "jobId": job.id, "location": f.location}
}

func (f *fakeBigQuery) resultPage(job *fakeJob, offset int, int64Timestamps bool, kind string) map[string]any {
	rep := job.reply
	end := len(rep.rows)
	if rep.pageSize > 0 && offset+rep.pageSize < end {
		end = offset + rep.pageSize
	}
	rows := make([]any, 0, end-offset)
	for _, row := range rep.rows[offset:end] {
		cells := make([]any, len(rep.fields))
		for i, field := range rep.fields {
			cells[i] = map[string]any{"v": wireValue(field, row[i], int64Timestamps, false)}
		}
		rows = append(rows, map[string]any{"f": cells})
	}
	out := map[string]any{
		"kind":         kind,
		"jobReference": f.jobRef(job),
		"jobComplete":  true,
		"totalRows":    strconv.Itoa(len(rep.rows)),
		"schema":       map[string]any{"fields": rep.fields},
		"rows":         rows,
	}
	if end < len(rep.rows) {
		out["pageToken"] = strconv.Itoa(end)
	}
	if rep.dmlRows != 0 {
		out["numDmlAffectedRows"] = strconv.FormatInt(rep.dmlRows, 10)
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
		panic(fmt.Sprintf("fake: unsupported cell %T", v))
	}
}

func (f *fakeBigQuery) json(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		f.t.Errorf("fake: encode response: %v", err)
	}
}

// fail writes an error in the shape the Google API client parses.
func (f *fakeBigQuery) fail(w http.ResponseWriter, code int, reason, message string) {
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
