package bigquerydriver

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	bq "google.golang.org/api/bigquery/v2"
)

const (
	// pageRows caps the rows per response page; BigQuery also caps a page by
	// size, so a page can be smaller.
	pageRows = 10000
	// waitMs is how long one call waits for a running query before the driver
	// asks again.
	waitMs = 10000
	// timestampParamLayout is the canonical TIMESTAMP text BigQuery accepts
	// for a parameter value.
	timestampParamLayout = "2006-01-02 15:04:05.999999-07:00"
)

// queryResult is the first page of a finished query.
type queryResult struct {
	schema  *bq.TableSchema
	rows    []*bq.TableRow
	token   string
	job     *bq.JobReference
	dmlRows int64
}

// run sends one statement and waits until BigQuery finishes it.
func (s *state) run(ctx context.Context, query string, args []driver.NamedValue) (*queryResult, error) {
	loc, err := s.loc(ctx)
	if err != nil {
		return nil, err
	}
	query = s.qualify(query, loc)
	params, mode := queryParameters(args)
	standardSQL := false

	req := &bq.QueryRequest{
		Query:        query,
		UseLegacySql: &standardSQL,
		DefaultDataset: &bq.DatasetReference{
			ProjectId: s.conf.ProjectID,
			DatasetId: s.conf.DatasetID,
		},
		Location:           loc,
		MaxResults:         pageRows,
		TimeoutMs:          waitMs,
		QueryParameters:    params,
		ParameterMode:      mode,
		MaximumBytesBilled: s.conf.MaxBytesBilled,
		// Exact microseconds instead of a float of seconds.
		FormatOptions: &bq.DataFormatOptions{UseInt64Timestamp: true},
		Labels:        map[string]string{"client": "graphjin"},
	}

	res, err := s.svc.Jobs.Query(s.conf.ProjectID, req).Context(ctx).Do()
	if err != nil {
		return nil, queryError("query", err, query)
	}
	if err := firstError(res.Errors); err != nil {
		return nil, queryError("query", err, query)
	}
	if res.JobComplete {
		return &queryResult{
			schema:  res.Schema,
			rows:    res.Rows,
			token:   res.PageToken,
			job:     res.JobReference,
			dmlRows: res.NumDmlAffectedRows,
		}, nil
	}
	if res.JobReference == nil {
		return nil, queryError("query", errors.New("still running, and BigQuery returned no job to wait on"), query)
	}

	// The query outlived the first call. getQueryResults waits server-side, so
	// this loop asks again only when a wait runs out.
	for {
		gr, err := s.page(ctx, res.JobReference, "")
		if err != nil {
			if ctx.Err() != nil {
				s.cancel(res.JobReference)
			}
			return nil, queryError("results", err, query)
		}
		if err := firstError(gr.Errors); err != nil {
			return nil, queryError("results", err, query)
		}
		if gr.JobComplete {
			job := gr.JobReference
			if job == nil {
				job = res.JobReference
			}
			return &queryResult{
				schema:  gr.Schema,
				rows:    gr.Rows,
				token:   gr.PageToken,
				job:     job,
				dmlRows: gr.NumDmlAffectedRows,
			}, nil
		}
		if err := ctx.Err(); err != nil {
			s.cancel(res.JobReference)
			return nil, err
		}
	}
}

// page reads one page of a job's rows, or waits for the job when token is
// empty and it is still running.
func (s *state) page(ctx context.Context, job *bq.JobReference, token string) (*bq.GetQueryResultsResponse, error) {
	call := s.svc.Jobs.GetQueryResults(job.ProjectId, job.JobId).
		MaxResults(pageRows).
		TimeoutMs(waitMs).
		FormatOptionsUseInt64Timestamp(true).
		Context(ctx)
	if loc := s.jobLocation(job); loc != "" {
		call = call.Location(loc)
	}
	if token != "" {
		call = call.PageToken(token)
	}
	return call.Do()
}

func (s *state) jobLocation(job *bq.JobReference) string {
	if job.Location != "" {
		return job.Location
	}
	s.locMu.Lock()
	defer s.locMu.Unlock()
	return s.location
}

// cancel stops a job nobody is waiting for any more, so an abandoned query
// does not keep scanning. Best effort: the caller's context is already done.
func (s *state) cancel(job *bq.JobReference) {
	ctx, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	call := s.svc.Jobs.Cancel(job.ProjectId, job.JobId).Context(ctx)
	if loc := s.jobLocation(job); loc != "" {
		call = call.Location(loc)
	}
	_, _ = call.Do()
}

// queryParameters maps database/sql arguments to BigQuery parameters. Unnamed
// arguments bind to `?` in order; named ones bind to `@name`.
func queryParameters(args []driver.NamedValue) ([]*bq.QueryParameter, string) {
	if len(args) == 0 {
		return nil, ""
	}
	mode := "POSITIONAL"
	params := make([]*bq.QueryParameter, 0, len(args))
	for _, arg := range args {
		typ, val := parameterValue(arg.Value)
		p := &bq.QueryParameter{
			ParameterType:  &bq.QueryParameterType{Type: typ},
			ParameterValue: val,
		}
		if arg.Name != "" {
			mode = "NAMED"
			p.Name = arg.Name
		}
		params = append(params, p)
	}
	return params, mode
}

// parameterValue picks the BigQuery type for a Go value. Anything without a
// scalar BigQuery type is sent as its JSON text.
func parameterValue(v any) (string, *bq.QueryParameterValue) {
	switch t := v.(type) {
	case nil:
		return "STRING", &bq.QueryParameterValue{NullFields: []string{"Value"}}
	case bool:
		return "BOOL", &bq.QueryParameterValue{Value: strconv.FormatBool(t)}
	case int:
		return "INT64", &bq.QueryParameterValue{Value: strconv.FormatInt(int64(t), 10)}
	case int32:
		return "INT64", &bq.QueryParameterValue{Value: strconv.FormatInt(int64(t), 10)}
	case int64:
		return "INT64", &bq.QueryParameterValue{Value: strconv.FormatInt(t, 10)}
	case float32:
		return "FLOAT64", &bq.QueryParameterValue{Value: strconv.FormatFloat(float64(t), 'f', -1, 64)}
	case float64:
		return "FLOAT64", &bq.QueryParameterValue{Value: strconv.FormatFloat(t, 'f', -1, 64)}
	case []byte:
		return "STRING", stringValue(string(t))
	case string:
		return "STRING", stringValue(t)
	case time.Time:
		return "TIMESTAMP", &bq.QueryParameterValue{Value: t.Format(timestampParamLayout)}
	default:
		data, err := json.Marshal(t)
		if err != nil {
			return "STRING", stringValue(fmt.Sprint(t))
		}
		return "STRING", stringValue(string(data))
	}
}

// stringValue keeps an empty string on the wire. The generated client drops
// empty fields, and a parameter with no value is not an empty string.
func stringValue(s string) *bq.QueryParameterValue {
	v := &bq.QueryParameterValue{Value: s}
	if s == "" {
		v.ForceSendFields = []string{"Value"}
	}
	return v
}

// firstError turns the errors BigQuery lists on a response into one error.
func firstError(errs []*bq.ErrorProto) error {
	if len(errs) == 0 || errs[0] == nil {
		return nil
	}
	e := errs[0]
	if e.Reason == "" {
		return errors.New(e.Message)
	}
	return fmt.Errorf("%s: %s", e.Reason, e.Message)
}

// apiError names the step that failed and keeps BigQuery's own error, which
// says what to fix (a missing dataset, a denied permission).
func apiError(step string, err error) error {
	return fmt.Errorf("bigquery %s: %w", step, err)
}

// queryError is apiError with the statement attached, because the message
// BigQuery returns points at a position in it.
func queryError(step string, err error, query string) error {
	return fmt.Errorf("%w\nSQL: %s", apiError(step, err), query)
}
