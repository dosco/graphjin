// Package bigquerydriver is a database/sql driver for Google BigQuery. It runs
// GoogleSQL through the BigQuery REST API (jobs.query), so GraphJin can open a
// BigQuery dataset the way it opens any other SQL source.
//
// The driver is deliberately small: positional `?` parameters, schema-typed
// rows, paging, and the dataset qualification BigQuery needs for
// INFORMATION_SCHEMA. It has no transactions, because a REST query is
// committed when it finishes; Begin fails instead of pretending.
package bigquerydriver

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	bq "google.golang.org/api/bigquery/v2"
	"google.golang.org/api/option"
)

// Config describes one BigQuery source.
type Config struct {
	// ProjectID owns the dataset and is billed for the queries.
	ProjectID string
	// DatasetID is the default dataset: unqualified tables resolve against it
	// and schema discovery is limited to it.
	DatasetID string
	// Location is the dataset's location (US, EU, us-central1, ...). Empty
	// means read it from the dataset on first use.
	Location string
	// CredentialsFile is a service-account key file, and CredentialsJSON the
	// contents of one; the JSON wins when both are set. With neither, the
	// driver uses Application Default Credentials.
	CredentialsFile string
	CredentialsJSON []byte
	// MaxBytesBilled fails a query that would bill more than this many bytes.
	// Zero leaves the project's own limits in charge.
	MaxBytesBilled int64
	// ClientOptions are appended to the options the driver builds, for callers
	// that bring their own HTTP client, endpoint or token source.
	ClientOptions []option.ClientOption
}

// ErrNoTransactions is returned by Begin. A caller that relies on rolling a
// write back must not be told a transaction started.
var ErrNoTransactions = errors.New("bigquery: transactions are not supported")

var (
	projectIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.:\-]*$`)
	datasetIDPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
	locationPattern  = regexp.MustCompile(`^[A-Za-z0-9\-]+$`)
)

// validate trims the identifiers and rejects anything that could not be a
// BigQuery project, dataset or location. The driver writes these into SQL when
// it qualifies INFORMATION_SCHEMA, so they are checked once, here.
func (c *Config) validate() error {
	c.ProjectID = strings.TrimSpace(c.ProjectID)
	c.DatasetID = strings.TrimSpace(c.DatasetID)
	c.Location = strings.TrimSpace(c.Location)
	c.CredentialsFile = strings.TrimSpace(c.CredentialsFile)

	switch {
	case c.ProjectID == "":
		return errors.New("bigquery: project id is required")
	case !projectIDPattern.MatchString(c.ProjectID):
		return fmt.Errorf("bigquery: invalid project id %q", c.ProjectID)
	case c.DatasetID == "":
		return errors.New("bigquery: dataset id is required")
	case !datasetIDPattern.MatchString(c.DatasetID):
		return fmt.Errorf("bigquery: invalid dataset id %q", c.DatasetID)
	case c.Location != "" && !locationPattern.MatchString(c.Location):
		return fmt.Errorf("bigquery: invalid location %q", c.Location)
	case c.MaxBytesBilled < 0:
		return errors.New("bigquery: max bytes billed cannot be negative")
	}
	return nil
}

// NewConnector returns a connector for sql.OpenDB. It reads credentials but
// sends nothing to BigQuery; the first Ping or query does.
//
// ctx is kept by the credential source to refresh tokens, so it must outlive
// the connector: pass context.Background(), not a request context.
func NewConnector(ctx context.Context, conf Config) (driver.Connector, error) {
	if err := conf.validate(); err != nil {
		return nil, err
	}
	opts := []option.ClientOption{
		option.WithScopes(bq.BigqueryScope),
		option.WithUserAgent("graphjin-bigquerydriver"),
	}
	// Service-account keys only: the other credential file types can point
	// the client at URLs of the file author's choosing.
	switch {
	case len(conf.CredentialsJSON) > 0:
		opts = append(opts, option.WithAuthCredentialsJSON(option.ServiceAccount, conf.CredentialsJSON))
	case conf.CredentialsFile != "":
		opts = append(opts, option.WithAuthCredentialsFile(option.ServiceAccount, conf.CredentialsFile))
	}
	opts = append(opts, conf.ClientOptions...)

	svc, err := bq.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("bigquery: %w", err)
	}
	return &connector{st: &state{svc: svc, conf: conf, location: conf.Location}}, nil
}

type connector struct {
	st *state
}

func (c *connector) Connect(context.Context) (driver.Conn, error) {
	return &conn{st: c.st}, nil
}

func (c *connector) Driver() driver.Driver { return drv{} }

type drv struct{}

func (drv) Open(string) (driver.Conn, error) {
	return nil, errors.New("bigquery: Open is not supported, use sql.OpenDB with NewConnector")
}

// state is shared by every connection of one connector. The BigQuery service
// client is safe for concurrent use, so connections carry no state of their
// own.
type state struct {
	svc  *bq.Service
	conf Config

	locMu    sync.Mutex
	location string
}

// loc returns the dataset's location, reading it from the dataset the first
// time when the config did not name one.
func (s *state) loc(ctx context.Context) (string, error) {
	s.locMu.Lock()
	defer s.locMu.Unlock()
	if s.location != "" {
		return s.location, nil
	}
	ds, err := s.svc.Datasets.Get(s.conf.ProjectID, s.conf.DatasetID).Context(ctx).Do()
	if err != nil {
		return "", apiError("dataset", err)
	}
	s.location = ds.Location
	return s.location, nil
}

// ping proves the credentials work and the dataset exists.
func (s *state) ping(ctx context.Context) error {
	ds, err := s.svc.Datasets.Get(s.conf.ProjectID, s.conf.DatasetID).Context(ctx).Do()
	if err != nil {
		return apiError("dataset", err)
	}
	s.locMu.Lock()
	if s.location == "" {
		s.location = ds.Location
	}
	s.locMu.Unlock()
	return nil
}

type conn struct {
	st *state
}

func (c *conn) Prepare(query string) (driver.Stmt, error) {
	return &stmt{c: c, query: query}, nil
}

func (c *conn) Close() error { return nil }

func (c *conn) Begin() (driver.Tx, error) { return nil, ErrNoTransactions }

func (c *conn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return nil, ErrNoTransactions
}

func (c *conn) Ping(ctx context.Context) error { return c.st.ping(ctx) }

// CheckNamedValue applies the standard conversions (Valuers, named types,
// pointers) and lets through what they reject, such as slices and maps, which
// are sent as JSON text.
func (c *conn) CheckNamedValue(nv *driver.NamedValue) error {
	if v, err := driver.DefaultParameterConverter.ConvertValue(nv.Value); err == nil {
		nv.Value = v
	}
	return nil
}

func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	res, err := c.st.run(ctx, query, args)
	if err != nil {
		return nil, err
	}
	return result(res.dmlRows), nil
}

func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	res, err := c.st.run(ctx, query, args)
	if err != nil {
		return nil, err
	}
	return newRows(ctx, c.st, res), nil
}

type stmt struct {
	c     *conn
	query string
}

func (s *stmt) Close() error { return nil }

// NumInput is unknown without parsing the query; database/sql skips its
// argument-count check for -1 and BigQuery reports a mismatch itself.
func (s *stmt) NumInput() int { return -1 }

func (s *stmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.c.ExecContext(context.Background(), s.query, positional(args))
}

func (s *stmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.c.QueryContext(context.Background(), s.query, positional(args))
}

func (s *stmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return s.c.ExecContext(ctx, s.query, args)
}

func (s *stmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.c.QueryContext(ctx, s.query, args)
}

func (s *stmt) CheckNamedValue(nv *driver.NamedValue) error {
	return s.c.CheckNamedValue(nv)
}

func positional(args []driver.Value) []driver.NamedValue {
	out := make([]driver.NamedValue, len(args))
	for i, arg := range args {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: arg}
	}
	return out
}

// result is the number of rows a DML statement changed.
type result int64

func (r result) LastInsertId() (int64, error) {
	return 0, errors.New("bigquery: LastInsertId is not supported")
}

func (r result) RowsAffected() (int64, error) { return int64(r), nil }

var (
	_ driver.Driver            = drv{}
	_ driver.Connector         = (*connector)(nil)
	_ driver.Conn              = (*conn)(nil)
	_ driver.ConnBeginTx       = (*conn)(nil)
	_ driver.ExecerContext     = (*conn)(nil)
	_ driver.QueryerContext    = (*conn)(nil)
	_ driver.Pinger            = (*conn)(nil)
	_ driver.NamedValueChecker = (*conn)(nil)
	_ driver.StmtExecContext   = (*stmt)(nil)
	_ driver.StmtQueryContext  = (*stmt)(nil)
	_ driver.NamedValueChecker = (*stmt)(nil)
)
