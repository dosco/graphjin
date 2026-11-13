package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// A lock held on the demo database must not be able to hold a write forever.
//
// The demo opened its SQLite file with cache=shared. In that mode a write that
// meets a table lock waits in the driver for the holder to finish, with no
// timeout, and neither busy_timeout nor the caller's context can end the wait.
// One statement left open by a bug therefore froze every later write to its
// table for the life of the process, which is how a benchmark run stopped on
// 2026-11-13. The bug that left the statement open is fixed; this holds the
// property that made it fatal. Without a shared cache the same situation is an
// ordinary "database is locked" once the busy timeout runs out.
func TestDemoSQLiteWriteBehindAHeldLockReturns(t *testing.T) {
	ctx := context.Background()
	cleanup, info, err := startSQLiteDemo(ctx, "app", t.TempDir(), demoStatus{})
	if err != nil {
		t.Fatal(err)
	}
	db := info.DB
	if _, err := db.ExecContext(ctx, `CREATE TABLE tickets (id INTEGER PRIMARY KEY, status TEXT NOT NULL);
INSERT INTO tickets VALUES (1, 'open'), (2, 'open');`); err != nil {
		t.Fatal(err)
	}

	// A reader that stops halfway: its statement stays open, as the abandoned
	// one did.
	reader, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := reader.QueryContext(ctx, `SELECT id FROM tickets`)
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal("the reader found no rows to hold open")
	}

	writer, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The demo waits five seconds for a busy database. The test only needs to
	// know that the wait ends.
	if _, err := writer.ExecContext(ctx, `PRAGMA busy_timeout=300`); err != nil {
		t.Fatal(err)
	}
	wrote := make(chan error, 1)
	go func() {
		_, err := writer.ExecContext(ctx, `UPDATE tickets SET status = 'closed' WHERE id = 2`)
		wrote <- err
	}()
	select {
	case err := <-wrote:
		// Either answer is fine: the write went through, or it was told the
		// database is locked. What it may not do is wait without end.
		t.Logf("the write behind a held read lock returned: %v", err)
		_ = rows.Close()
		_ = reader.Close()
		_ = writer.Close()
		_ = cleanup(ctx)
	case <-time.After(10 * time.Second):
		// Nothing is closed: the writer's connection will never come back.
		t.Fatal("a write behind a held read lock is still waiting after 10s, with a 300ms busy timeout: " +
			"the demo database is waiting on a lock without a deadline")
	}
}

// The demo database serves reads and writes at once without lock errors.
//
// Dropping the shared cache moves SQLite from locking a table to locking the
// file, so a write now waits for every reader, not only for readers of its own
// table. This is the check that the wait stays inside the busy timeout under
// more pressure than a benchmark applies: readers and writers together, a watch
// polling the same tables, and the world being reset in between, as it is
// between episodes that write.
//
// GJ_SQLITE_STRESS_SECONDS lengthens the run; the default is short enough for
// every test run.
func TestDemoSQLiteConcurrentReadsAndWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	seconds := 3
	if raw := os.Getenv("GJ_SQLITE_STRESS_SECONDS"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			t.Fatalf("GJ_SQLITE_STRESS_SECONDS=%q is not a positive number of seconds", raw)
		}
		seconds = parsed
	}
	const (
		readers = 3 // a benchmark's concurrency
		writers = 2
		rounds  = 2 // load, reset, load
	)

	project := t.TempDir()
	if err := extractDefaultDemo(project); err != nil {
		t.Fatal(err)
	}
	originalPath, originalConf, originalDB, originalOpened := cpath, conf, db, dbOpened
	defer func() {
		cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened
	}()
	t.Setenv("GO_ENV", "dev")

	client := &evalScriptClient{code: `await final({status:"blocked",answer:"not configured"});`}
	environment := evalEnvironment{
		ClientFactory: func(gjagent.Config) (ax.AIClient, error) { return client, nil },
	}
	instance, err := environment.Start(context.Background(), gjeval.EnvSpec{
		Target: gjeval.TargetDemo, ConfigPath: project, Seed: 23,
		Writable: true, Reactive: true, Resettable: true, FreezeTime: "2026-08-01T12:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	stuck := false
	defer func() {
		// Closing waits for requests in flight, so a stuck one would turn a
		// failure into a hang.
		if !stuck {
			_ = instance.Close()
		}
	}()
	resettable, ok := instance.(gjeval.ResettableInstance)
	if !ok {
		t.Fatal("a resettable spec produced an instance that cannot reset")
	}

	reads := []string{
		`query { invoices(where: { status: { eq: "failed" } }) { count_id sum_amount_cents } }`,
		`query { accounts(order_by: { mrr_cents: desc }, limit: 5) { id name mrr_cents support_tickets(limit: 3) { id status severity } } }`,
		`query { support_tickets(where: { status: { eq: "open" } }, order_by: { id: asc }, limit: 20) { id subject accounts { name plan } } }`,
		`query { subscriptions(where: { status: { eq: "active" } }) { count_id sum_mrr_cents } }`,
		`query { usage_events(order_by: { id: desc }, limit: 25) { id kind quantity accounts { name } } }`,
	}
	write := func(worker, n int) string {
		id := 1 + (worker*7+n)%12
		if n%2 == 0 {
			return fmt.Sprintf(`mutation { support_tickets(where: { id: { eq: %d } }, update: { resolution_note: "stress %d-%d" }) { id resolution_note } }`, id, worker, n)
		}
		return fmt.Sprintf(`mutation { invoices(where: { id: { eq: %d } }, update: { attempts: %d }) { id attempts } }`, id, n%9)
	}

	var (
		mu       sync.Mutex
		readLat  []time.Duration
		writeLat []time.Duration
		failures []string
		timeouts int
	)
	record := func(kind, query string, started time.Time, status int, payload []byte, err error) {
		elapsed := time.Since(started)
		mu.Lock()
		defer mu.Unlock()
		switch {
		case err != nil:
			timeouts++
			failures = append(failures, fmt.Sprintf("%s after %s never returned: %v", kind, elapsed.Round(time.Millisecond), err))
		case status >= 300 || strings.Contains(string(payload), `"errors"`):
			failures = append(failures, fmt.Sprintf("%s after %s: %d %s (%s)", kind, elapsed.Round(time.Millisecond), status, payload, query))
		case kind == "read":
			readLat = append(readLat, elapsed)
		default:
			writeLat = append(writeLat, elapsed)
		}
	}

	for round := 0; round < rounds; round++ {
		// A watch keeps the subscription poller reading the same tables the
		// writers change. A reset removes it, so it is installed every round.
		installEvalWatch(t, instance)
		deadline := time.Now().Add(time.Duration(seconds) * time.Second / rounds)
		var wg sync.WaitGroup
		for worker := 0; worker < readers; worker++ {
			wg.Add(1)
			go func(worker int) {
				defer wg.Done()
				for n := 0; time.Now().Before(deadline); n++ {
					query := reads[(worker+n)%len(reads)]
					started := time.Now()
					status, payload, err := postEvalGraphQL(instance, query, 20*time.Second)
					record("read", query, started, status, payload, err)
					if err != nil {
						return
					}
				}
			}(worker)
		}
		for worker := 0; worker < writers; worker++ {
			wg.Add(1)
			go func(worker int) {
				defer wg.Done()
				for n := 0; time.Now().Before(deadline); n++ {
					query := write(worker, n)
					started := time.Now()
					status, payload, err := postEvalGraphQL(instance, query, 20*time.Second)
					record("write", query, started, status, payload, err)
					if err != nil {
						return
					}
				}
			}(worker)
		}
		wg.Wait()
		if timeouts != 0 || round == rounds-1 {
			break
		}
		if err := resettable.Reset(context.Background()); err != nil {
			t.Fatalf("reset after round %d: %v", round+1, err)
		}
	}

	t.Logf("reads: %s", latencySummary(readLat))
	t.Logf("writes: %s", latencySummary(writeLat))
	if timeouts != 0 {
		stuck = true
	}
	if len(failures) != 0 {
		shown := failures
		if len(shown) > 5 {
			shown = shown[:5]
		}
		t.Fatalf("%d of %d requests failed, %d of them by never returning; first %d:\n%s",
			len(failures), len(failures)+len(readLat)+len(writeLat), timeouts, len(shown), strings.Join(shown, "\n"))
	}
	if len(readLat) == 0 || len(writeLat) == 0 {
		t.Fatalf("the load did not run: %d reads, %d writes", len(readLat), len(writeLat))
	}
}

func latencySummary(samples []time.Duration) string {
	if len(samples) == 0 {
		return "none"
	}
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	at := func(q float64) time.Duration {
		return sorted[int(q*float64(len(sorted)-1))].Round(100 * time.Microsecond)
	}
	return fmt.Sprintf("%d ok, p50 %s, p99 %s, max %s", len(sorted), at(0.5), at(0.99), at(1))
}
