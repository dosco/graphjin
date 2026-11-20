package bigquerydriver

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/bigquerydriver/bqtest"
	bq "google.golang.org/api/bigquery/v2"
	"google.golang.org/api/option"
)

var field = bqtest.Field

// open returns a *sql.DB on the driver, pointed at the stand-in.
func open(t *testing.T, fake *bqtest.Server, conf Config) *sql.DB {
	t.Helper()
	if conf.ProjectID == "" {
		conf.ProjectID = fake.Project
	}
	if conf.DatasetID == "" {
		conf.DatasetID = fake.Dataset
	}
	conf.ClientOptions = []option.ClientOption{
		option.WithEndpoint(fake.Endpoint()),
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

func TestQueryReturnsRowsTypedBySchema(t *testing.T) {
	fake := bqtest.New(t)
	when := time.Date(2026, 11, 19, 8, 30, 15, 123456000, time.UTC)
	fake.OnContains("FROM orders", bqtest.Reply{
		Fields: []*bq.TableFieldSchema{
			field("sku", "STRING"),
			field("qty", "INTEGER"),
			field("price", "FLOAT"),
			field("paid", "BOOLEAN"),
			field("placed_at", "TIMESTAMP"),
			field("total", "NUMERIC"),
			field("note", "STRING"),
			{Name: "tags", Type: "INTEGER", Mode: "REPEATED"},
			{Name: "buyer", Type: "RECORD", Mode: "NULLABLE", Fields: []*bq.TableFieldSchema{
				field("name", "STRING"), field("vip", "BOOLEAN"),
			}},
		},
		Rows: [][]any{{
			"007", int64(3), 1.5, true, when, "12.30", nil,
			[]any{int64(1), int64(2)},
			[]any{"Ada", true},
		}},
	})
	db := open(t, fake, Config{})

	var (
		sku, total, tags, buyer string
		qty                     int64
		price                   float64
		paid                    bool
		placedAt                time.Time
		note                    *string
	)
	err := db.QueryRow("SELECT * FROM orders").Scan(&sku, &qty, &price, &paid, &placedAt, &total, &note, &tags, &buyer)
	if err != nil {
		t.Fatal(err)
	}
	// A STRING of digits stays text; guessing the type from the text would make it 7.
	if sku != "007" {
		t.Errorf("sku = %q, want 007", sku)
	}
	if qty != 3 || price != 1.5 || !paid {
		t.Errorf("qty, price, paid = %d, %v, %v", qty, price, paid)
	}
	if !placedAt.Equal(when) {
		t.Errorf("placed_at = %s, want %s", placedAt.Format(time.RFC3339Nano), when.Format(time.RFC3339Nano))
	}
	if total != "12.30" {
		t.Errorf("NUMERIC total = %q, want the exact text 12.30", total)
	}
	if note != nil {
		t.Errorf("NULL note = %q, want nil", *note)
	}
	if tags != `[1,2]` {
		t.Errorf("array = %s, want [1,2]", tags)
	}
	if buyer != `{"name":"Ada","vip":true}` {
		t.Errorf("struct = %s", buyer)
	}

	// The fake sends TIMESTAMP as float seconds unless int64 is requested, so
	// an exact placed_at above also proves the request asked for it.
	if opts, _ := fake.LastQuery(t)["formatOptions"].(map[string]any); opts["useInt64Timestamp"] != true {
		t.Errorf("formatOptions = %v, want useInt64Timestamp", opts)
	}
}

func TestQuerySendsDefaultDatasetAndLocation(t *testing.T) {
	fake := bqtest.New(t)
	fake.OnContains("SELECT 1", bqtest.Reply{Fields: []*bq.TableFieldSchema{field("n", "INTEGER")}, Rows: [][]any{{int64(1)}}})
	// No location in the config: the driver must learn europe-west2 from the
	// dataset, or the fake (like BigQuery) will not find the job's results.
	db := open(t, fake, Config{})

	var n int64
	if err := db.QueryRow("SELECT 1").Scan(&n); err != nil {
		t.Fatal(err)
	}
	q := fake.LastQuery(t)
	if q["location"] != "europe-west2" {
		t.Errorf("location = %v, want the dataset's europe-west2", q["location"])
	}
	ds, _ := q["defaultDataset"].(map[string]any)
	if ds["projectId"] != "acme-prod" || ds["datasetId"] != "analytics" {
		t.Errorf("defaultDataset = %v", ds)
	}
	if _, sent := q["maximumBytesBilled"]; sent {
		t.Errorf("maximumBytesBilled sent without being configured: %v", q["maximumBytesBilled"])
	}
}

func TestQueryHonoursMaxBytesBilled(t *testing.T) {
	fake := bqtest.New(t)
	fake.OnContains("SELECT 1", bqtest.Reply{Fields: []*bq.TableFieldSchema{field("n", "INTEGER")}, Rows: [][]any{{int64(1)}}})
	db := open(t, fake, Config{MaxBytesBilled: 5_000_000_000})

	var n int64
	if err := db.QueryRow("SELECT 1").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if got := fake.LastQuery(t)["maximumBytesBilled"]; got != "5000000000" {
		t.Errorf("maximumBytesBilled = %v, want 5000000000", got)
	}
}

func TestPositionalParameters(t *testing.T) {
	fake := bqtest.New(t)
	fake.OnContains("FROM users", bqtest.Reply{Fields: []*bq.TableFieldSchema{field("n", "INTEGER")}, Rows: [][]any{{int64(1)}}})
	db := open(t, fake, Config{})

	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	var n int64
	err := db.QueryRow(
		"SELECT COUNT(*) FROM users WHERE id = ? AND name = ? AND nick = ? AND ref = ? AND seen > ? AND tags = ? AND note = '?'",
		int64(42), "Ada", "", nil, when, []string{"a", "b"},
	).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}

	q := fake.LastQuery(t)
	if q["parameterMode"] != "POSITIONAL" {
		t.Errorf("parameterMode = %v", q["parameterMode"])
	}
	params, _ := q["queryParameters"].([]any)
	type want struct {
		typ   string
		value any
	}
	wants := []want{
		{"INT64", "42"},
		{"STRING", "Ada"},
		{"STRING", ""}, // an empty string, not a missing value
		{"STRING", nil},
		{"TIMESTAMP", "2026-01-02 03:04:05+00:00"},
		{"STRING", `["a","b"]`},
	}
	if len(params) != len(wants) {
		t.Fatalf("sent %d parameters, want %d: %v", len(params), len(wants), params)
	}
	for i, w := range wants {
		p := params[i].(map[string]any)
		typ := p["parameterType"].(map[string]any)["type"]
		val, sent := p["parameterValue"].(map[string]any)["value"]
		if typ != w.typ || val != w.value || !sent {
			t.Errorf("parameter %d = %v %#v (value sent: %v), want %s %#v", i+1, typ, val, sent, w.typ, w.value)
		}
	}
}

func TestQueryReadsEveryPage(t *testing.T) {
	fake := bqtest.New(t)
	var rows [][]any
	for i := 0; i < 7; i++ {
		rows = append(rows, []any{int64(i)})
	}
	fake.OnContains("FROM big", bqtest.Reply{Fields: []*bq.TableFieldSchema{field("n", "INTEGER")}, Rows: rows, PageSize: 3})
	db := open(t, fake, Config{})

	res, err := db.Query("SELECT n FROM big")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close() //nolint:errcheck
	var got []int64
	for res.Next() {
		var n int64
		if err := res.Scan(&n); err != nil {
			t.Fatal(err)
		}
		got = append(got, n)
	}
	if err := res.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 7 || got[0] != 0 || got[6] != 6 {
		t.Fatalf("rows = %v, want 0..6 in order", got)
	}
	// 3 + 3 + 1 Rows: the first page came with the query, two more were fetched.
	if fake.ResultCalls() != 2 {
		t.Errorf("fetched %d further pages, want 2", fake.ResultCalls())
	}
}

func TestQueryWaitsForARunningJob(t *testing.T) {
	fake := bqtest.New(t)
	fake.OnContains("FROM slow", bqtest.Reply{
		Fields:       []*bq.TableFieldSchema{field("n", "INTEGER")},
		Rows:         [][]any{{int64(9)}},
		PendingPolls: 2,
	})
	db := open(t, fake, Config{})

	var n int64
	if err := db.QueryRow("SELECT n FROM slow").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 9 {
		t.Errorf("n = %d, want 9", n)
	}
	if fake.ResultCalls() != 3 {
		t.Errorf("asked for results %d times, want 3 (two still running, one finished)", fake.ResultCalls())
	}
}

func TestAbandonedQueryIsCancelled(t *testing.T) {
	fake := bqtest.New(t)
	fake.OnContains("FROM huge", bqtest.Reply{Block: true})
	db := open(t, fake, Config{})

	ctx, stop := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer stop()
	_, err := db.QueryContext(ctx, "SELECT n FROM huge")
	if err == nil {
		t.Fatal("query returned without an error after its context ended")
	}
	cancelled := fake.Cancelled()
	if len(cancelled) != 1 || cancelled[0] != "job_1" {
		t.Errorf("cancelled jobs = %v, want [job_1]: an abandoned query must not keep scanning", cancelled)
	}
}

func TestExecReportsRowsChanged(t *testing.T) {
	fake := bqtest.New(t)
	fake.OnContains("UPDATE users", bqtest.Reply{DMLRows: 4})
	db := open(t, fake, Config{})

	res, err := db.Exec("UPDATE users SET active = ? WHERE team = ?", false, "ops")
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := res.RowsAffected(); n != 4 {
		t.Errorf("RowsAffected = %d, want 4", n)
	}
}

func TestBeginIsRefused(t *testing.T) {
	fake := bqtest.New(t)
	db := open(t, fake, Config{})

	// GraphJin previews a write by running it in a transaction it rolls back.
	// A transaction that silently did nothing would commit that preview.
	tx, err := db.Begin()
	if !errors.Is(err, ErrNoTransactions) {
		if tx != nil {
			_ = tx.Rollback()
		}
		t.Fatalf("Begin error = %v, want ErrNoTransactions", err)
	}
	if len(fake.Queries()) != 0 {
		t.Errorf("Begin sent %d statement(s) to BigQuery", len(fake.Queries()))
	}
}

func TestPing(t *testing.T) {
	fake := bqtest.New(t)
	if err := open(t, fake, Config{}).Ping(); err != nil {
		t.Fatalf("ping of an existing dataset: %v", err)
	}
	err := open(t, fake, Config{DatasetID: "missing"}).Ping()
	if err == nil || !strings.Contains(err.Error(), "Not found: Dataset acme-prod:missing") {
		t.Fatalf("ping of a missing dataset = %v, want BigQuery's not-found message", err)
	}
}

func TestQueryErrorKeepsBigQueryMessageAndSQL(t *testing.T) {
	fake := bqtest.New(t)
	fake.OnContains("FROM oops", bqtest.Reply{Fail: &bqtest.Failure{Code: 400, Reason: "invalidQuery", Message: "Unrecognized name: nme at [1:8]"}})
	db := open(t, fake, Config{})

	_, err := db.Query("SELECT nme FROM oops")
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"Unrecognized name: nme at [1:8]", "SQL: SELECT nme FROM oops"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q is missing %q", err, want)
		}
	}
}

func TestWrongLocationFailsLikeBigQuery(t *testing.T) {
	fake := bqtest.New(t)
	fake.OnContains("SELECT 1", bqtest.Reply{Fields: []*bq.TableFieldSchema{field("n", "INTEGER")}, Rows: [][]any{{int64(1)}}})
	db := open(t, fake, Config{Location: "US"})

	var n int64
	err := db.QueryRow("SELECT 1").Scan(&n)
	if err == nil || !strings.Contains(err.Error(), "was not found in location US") {
		t.Fatalf("error = %v, want BigQuery's wrong-location message", err)
	}
}

func TestCatalogQueriesAreQualified(t *testing.T) {
	fake := bqtest.New(t)
	one := bqtest.Reply{Fields: []*bq.TableFieldSchema{field("table_name", "STRING")}, Rows: [][]any{{"users"}}}
	fake.On(func(sql string) bool { return strings.Contains(sql, "INFORMATION_SCHEMA") }, one)
	db := open(t, fake, Config{})

	// The SQL GraphJin's discovery sends. The fake refuses a bare
	// INFORMATION_SCHEMA view, as BigQuery does.
	const discovery = `SELECT col.table_name FROM information_schema.columns col
WHERE col.table_schema NOT IN ('INFORMATION_SCHEMA')
  AND (COALESCE(@@dataset_id, '') = '' OR LOWER(col.table_schema) = LOWER(COALESCE(@@dataset_id, '')))`
	var name string
	if err := db.QueryRow(discovery).Scan(&name); err != nil {
		t.Fatal(err)
	}
	sent, _ := fake.LastQuery(t)["query"].(string)
	for _, want := range []string{
		"FROM `acme-prod`.`analytics`.INFORMATION_SCHEMA.COLUMNS col",
		"NOT IN ('INFORMATION_SCHEMA')",
		"(COALESCE('analytics', '') = '' OR LOWER(col.table_schema) = LOWER(COALESCE('analytics', '')))",
	} {
		if !strings.Contains(sent, want) {
			t.Errorf("sent SQL is missing %q:\n%s", want, sent)
		}
	}
	if strings.Contains(sent, "@@") {
		t.Errorf("sent SQL still reads a system variable:\n%s", sent)
	}

	const storage = `SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLE_STORAGE WHERE LOWER(TABLE_NAME) = LOWER(?)`
	if err := db.QueryRow(storage, "users").Scan(&name); err != nil {
		t.Fatal(err)
	}
	sent, _ = fake.LastQuery(t)["query"].(string)
	if want := "FROM `acme-prod`.`region-europe-west2`.INFORMATION_SCHEMA.TABLE_STORAGE"; !strings.Contains(sent, want) {
		t.Errorf("sent SQL is missing %q:\n%s", want, sent)
	}
}

func TestQualifyLeavesLiteralsAndOrdinaryQueriesAlone(t *testing.T) {
	s := &state{conf: Config{ProjectID: "p", DatasetID: "d"}}

	plain := "SELECT TO_JSON_STRING(JSON_OBJECT('users', `users`.`id`)) FROM `users` WHERE `users`.`id` = ?"
	if got := s.qualify(plain, "US"); got != plain {
		t.Errorf("an ordinary query changed:\n%s", got)
	}

	cases := []struct{ in, want string }{
		{
			`SELECT 1 FROM information_schema.tables WHERE note = 'see information_schema.tables'`,
			"SELECT 1 FROM `p`.`d`.INFORMATION_SCHEMA.TABLES WHERE note = 'see information_schema.tables'",
		},
		{
			`SELECT "information_schema.columns", 'it\'s information_schema.columns' FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE`,
			`SELECT "information_schema.columns", 'it\'s information_schema.columns' FROM ` + "`p`.`d`.INFORMATION_SCHEMA.KEY_COLUMN_USAGE",
		},
		{
			"SELECT 1 FROM `x.information_schema.tables` WHERE a = COALESCE(@@project_id, '')",
			"SELECT 1 FROM `x.information_schema.tables` WHERE a = COALESCE('p', '')",
		},
		{
			`SELECT 1 FROM information_schema.table_storage`,
			"SELECT 1 FROM `p`.`region-us`.INFORMATION_SCHEMA.TABLE_STORAGE",
		},
	}
	for _, c := range cases {
		if got := s.qualify(c.in, "US"); got != c.want {
			t.Errorf("qualify(%s)\n got: %s\nwant: %s", c.in, got, c.want)
		}
	}
}

func TestParseDSN(t *testing.T) {
	conf, err := ParseDSN("bigquery://acme-prod/analytics?location=EU&credentials_file=/keys/sa.json&max_bytes_billed=1000")
	if err != nil {
		t.Fatal(err)
	}
	want := Config{ProjectID: "acme-prod", DatasetID: "analytics", Location: "EU", CredentialsFile: "/keys/sa.json", MaxBytesBilled: 1000}
	if conf.ProjectID != want.ProjectID || conf.DatasetID != want.DatasetID || conf.Location != want.Location ||
		conf.CredentialsFile != want.CredentialsFile || conf.MaxBytesBilled != want.MaxBytesBilled {
		t.Errorf("ParseDSN = %+v, want %+v", conf, want)
	}

	bad := map[string]string{
		"postgres://acme-prod/analytics":                      "must start with bigquery://",
		"bigquery://acme-prod":                                "dataset id is required",
		"bigquery:///analytics":                               "project id is required",
		"bigquery://acme-prod/analytics/users":                "project/dataset",
		"bigquery://acme-prod/analytics?locaton=EU":           "unknown connection string option",
		"bigquery://acme-prod/analytics?max_bytes_billed=1GB": "whole number of bytes",
		"bigquery://acme-prod/ana`lytics":                     "invalid dataset id",
	}
	for dsn, msg := range bad {
		if _, err := ParseDSN(dsn); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("ParseDSN(%q) error = %v, want one containing %q", dsn, err, msg)
		}
	}
}
