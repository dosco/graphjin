package tests_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/dosco/graphjin/clickhousedriver"
	core "github.com/dosco/graphjin/core/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	chDatabase = "graphjin_test"
	chUser     = "graphjin"
	chPass     = "graphjin"
)

// chCreatedAt mirrors the timestamp the other dialects seed.
var chCreatedAt = time.Date(2021, 1, 9, 16, 37, 1, 0, time.UTC)

// startClickHouseDB boots a clickhouse-server container, seeds the shared webshop
// schema, and returns a *sql.DB backed by the clickhousedriver connector (which
// wraps the clickhouse-go handle and intercepts GraphJin's JSON DSL).
func startClickHouseDB(ctx context.Context) (func(context.Context) error, *sql.DB, error) {
	// Reuse an already-running server for fast local iteration.
	if addr := os.Getenv("CLICKHOUSE_TEST_ADDR"); addr != "" {
		inner := clickhouse.OpenDB(&clickhouse.Options{
			Addr: strings.Split(addr, ","),
			Auth: clickhouse.Auth{Database: chDatabase, Username: chUser, Password: chPass},
		})
		if err := chReady(ctx, inner); err != nil {
			return nil, nil, err
		}
		if err := seedClickHouseWebshop(inner); err != nil {
			inner.Close()
			return nil, nil, err
		}
		db := sql.OpenDB(clickhousedriver.NewConnector(inner, chDatabase))
		return func(context.Context) error { db.Close(); inner.Close(); return nil }, db, nil
	}

	req := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "clickhouse/clickhouse-server:24.8",
			ExposedPorts: []string{"9000/tcp", "8123/tcp"},
			Env: map[string]string{
				"CLICKHOUSE_DB":                      chDatabase,
				"CLICKHOUSE_USER":                    chUser,
				"CLICKHOUSE_PASSWORD":                chPass,
				"CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT": "1",
			},
			WaitingFor: wait.ForListeningPort("9000/tcp").WithStartupTimeout(180 * time.Second),
		},
		Started: true,
	}
	container, err := testcontainers.GenericContainer(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("clickhouse container: %w", err)
	}
	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, nil, err
	}
	port, err := container.MappedPort(ctx, "9000")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, nil, err
	}

	inner := clickhouse.OpenDB(&clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%s", host, port.Port())},
		Auth: clickhouse.Auth{Database: chDatabase, Username: chUser, Password: chPass},
	})
	if err := chReady(ctx, inner); err != nil {
		inner.Close()
		_ = container.Terminate(ctx)
		return nil, nil, err
	}
	if err := seedClickHouseWebshop(inner); err != nil {
		inner.Close()
		_ = container.Terminate(ctx)
		return nil, nil, fmt.Errorf("clickhouse seed: %w", err)
	}

	db := sql.OpenDB(clickhousedriver.NewConnector(inner, chDatabase))
	cleanup := func(ctx context.Context) error {
		_ = db.Close()
		_ = inner.Close()
		return container.Terminate(ctx)
	}
	return cleanup, db, nil
}

func chReady(ctx context.Context, db *sql.DB) error {
	var lastErr error
	for i := 0; i < 60; i++ {
		if lastErr = db.PingContext(ctx); lastErr == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("clickhouse not ready: %w", lastErr)
}

func seedClickHouseWebshop(db *sql.DB) error {
	ddl := []string{
		`CREATE TABLE IF NOT EXISTS users (id Int32, full_name String, email String, phone Nullable(String), stripe_id String, disabled Bool, created_at DateTime) ENGINE = MergeTree ORDER BY id`,
		`CREATE TABLE IF NOT EXISTS products (id Int32, name String, description String, price Float64, owner_id Int32, country_code String, created_at DateTime) ENGINE = MergeTree ORDER BY id`,
		`CREATE TABLE IF NOT EXISTS purchases (id Int32, customer_id Int32, product_id Int32, quantity Int32, created_at DateTime) ENGINE = MergeTree ORDER BY id`,
		`CREATE TABLE IF NOT EXISTS comments (id Int32, body String, product_id Int32, commenter_id Int32, reply_to_id Int32, created_at DateTime) ENGINE = MergeTree ORDER BY id`,
		`CREATE TABLE IF NOT EXISTS categories (id Int32, name String, description String, created_at DateTime) ENGINE = MergeTree ORDER BY id`,
		`CREATE TABLE IF NOT EXISTS chats (id Int32, body String, created_at DateTime) ENGINE = MergeTree ORDER BY id`,
		`CREATE TABLE IF NOT EXISTS notifications (id Int32, verb String, subject_type String, subject_id Int32, user_id Int32, created_at DateTime) ENGINE = MergeTree ORDER BY id`,
	}
	for _, q := range ddl {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}

	if err := chSeed(db, `INSERT INTO users (id, full_name, email, phone, stripe_id, disabled, created_at)`, 7, func(add func(...any) error) error {
		for i := 1; i <= 100; i++ {
			// Mixed nulls in a Nullable column so keyset pagination over it is tested.
			var phone any
			if i <= 50 {
				phone = fmt.Sprintf("555-%04d", i)
			}
			if err := add(int32(i), fmt.Sprintf("User %d", i), fmt.Sprintf("user%d@test.com", i), phone,
				fmt.Sprintf("payment_id_%d", i+1000), i == 50, chCreatedAt); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	if err := chSeed(db, `INSERT INTO products (id, name, description, price, owner_id, country_code, created_at)`, 7, func(add func(...any) error) error {
		for i := 1; i <= 100; i++ {
			if err := add(int32(i), fmt.Sprintf("Product %d", i), fmt.Sprintf("Description for product %d", i),
				float64(i)+10.5, int32(i), "US", chCreatedAt); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	if err := chSeed(db, `INSERT INTO purchases (id, customer_id, product_id, quantity, created_at)`, 5, func(add func(...any) error) error {
		for i := 1; i <= 100; i++ {
			customerID := i + 1
			if i >= 100 {
				customerID = 1
			}
			if err := add(int32(i), int32(customerID), int32(i), int32(i*10), chCreatedAt); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	if err := chSeed(db, `INSERT INTO comments (id, body, product_id, commenter_id, reply_to_id, created_at)`, 6, func(add func(...any) error) error {
		for i := 1; i <= 100; i++ {
			replyTo := 0
			if i >= 2 {
				replyTo = i - 1
			}
			if err := add(int32(i), fmt.Sprintf("This is comment number %d", i), int32(i), int32(i), int32(replyTo), chCreatedAt); err != nil {
				return err
			}
		}
		// Extra comments so products 1 and 2 each have several (per-parent LIMIT test).
		for _, e := range [][2]int{{201, 1}, {202, 1}, {203, 2}, {204, 2}} {
			if err := add(int32(e[0]), fmt.Sprintf("Extra comment %d", e[0]), int32(e[1]), int32(1), int32(0), chCreatedAt); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	if err := chSeed(db, `INSERT INTO categories (id, name, description, created_at)`, 4, func(add func(...any) error) error {
		for i := 1; i <= 5; i++ {
			if err := add(int32(i), fmt.Sprintf("Category %d", i), fmt.Sprintf("Description for category %d", i), chCreatedAt); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	return chSeed(db, `INSERT INTO chats (id, body, created_at)`, 3, func(add func(...any) error) error {
		for i := 1; i <= 5; i++ {
			if err := add(int32(i), fmt.Sprintf("This is chat message number %d", i), chCreatedAt); err != nil {
				return err
			}
		}
		return nil
	})
}

// chSeed runs a clickhouse-go batch insert (Begin → Prepare → Exec rows → Commit).
func chSeed(db *sql.DB, insertPrefix string, ncols int, rows func(add func(...any) error) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	marks := strings.TrimSuffix(strings.Repeat("?, ", ncols), ", ")
	stmt, err := tx.Prepare(insertPrefix + " VALUES (" + marks + ")")
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := rows(func(vals ...any) error { _, e := stmt.Exec(vals...); return e }); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// TestClickHouseMutations validates insert (read-after-write), best-effort
// synchronous update, and lightweight delete against a live ClickHouse.
func TestClickHouseMutations(t *testing.T) {
	if dbType != "clickhouse" {
		t.Skipf("skipping for %s", dbType)
	}
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// INSERT + read-after-write via the returning selection.
	ins := json.RawMessage(`{"data":{"id":5001,"name":"CH Insert","description":"d","price":99.5,"owner_id":1,"country_code":"US"}}`)
	res, err := gj.GraphQL(ctx, `mutation { products(insert: $data) { id name price } }`, ins, nil)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	var ir struct {
		Products []struct {
			ID    int     `json:"id"`
			Name  string  `json:"name"`
			Price float64 `json:"price"`
		} `json:"products"`
	}
	if err := json.Unmarshal(res.Data, &ir); err != nil {
		t.Fatalf("unmarshal insert %s: %v", res.Data, err)
	}
	if len(ir.Products) != 1 || ir.Products[0].ID != 5001 || ir.Products[0].Name != "CH Insert" || ir.Products[0].Price != 99.5 {
		t.Errorf("insert returned %+v, want [{5001 CH Insert 99.5}]", ir.Products)
	}
	if rr, _ := gj.GraphQL(ctx, `query { products(id: 5001) { name } }`, nil, nil); !strings.Contains(string(rr.Data), "CH Insert") {
		t.Errorf("read-after-insert: %s", rr.Data)
	}

	// UPDATE (synchronous mutation) + read-after-write.
	upd := json.RawMessage(`{"data":{"name":"CH Updated"}}`)
	if _, err := gj.GraphQL(ctx, `mutation { products(update: $data, where: {id: {eq: 5001}}) { id name } }`, upd, nil); err != nil {
		t.Fatalf("update: %v", err)
	}
	if rr, _ := gj.GraphQL(ctx, `query { products(id: 5001) { name } }`, nil, nil); !strings.Contains(string(rr.Data), "CH Updated") {
		t.Errorf("read-after-update: %s", rr.Data)
	}

	// DELETE (lightweight) + confirm gone.
	if _, err := gj.GraphQL(ctx, `mutation { products(delete: true, where: {id: {eq: 5001}}) { id } }`, nil, nil); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if rr, _ := gj.GraphQL(ctx, `query { products(id: 5001) { id } }`, nil, nil); !strings.Contains(string(rr.Data), "null") {
		t.Errorf("read-after-delete should be null: %s", rr.Data)
	}
}

// TestClickHousePagination validates offset pagination (and probes cursor/keyset).
func TestClickHousePagination(t *testing.T) {
	if dbType != "clickhouse" {
		t.Skipf("skipping for %s", dbType)
	}
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Offset pagination.
	ores, err := gj.GraphQL(ctx, `query { products(limit: 2, offset: 2, order_by: { id: asc }) { id } }`, nil, nil)
	if err != nil {
		t.Fatalf("offset: %v", err)
	}
	var op struct {
		Products []struct {
			ID int `json:"id"`
		} `json:"products"`
	}
	if err := json.Unmarshal(ores.Data, &op); err != nil {
		t.Fatalf("unmarshal %s: %v", ores.Data, err)
	}
	if len(op.Products) != 2 || op.Products[0].ID != 3 || op.Products[1].ID != 4 {
		t.Errorf("offset = %+v, want ids [3 4]", op.Products)
	}

	// Cursor / keyset — probe current behavior.
	cres, cerr := gj.GraphQL(ctx, `query { products(first: 2, order_by: { id: asc }) { id } }`, nil, nil)
	if cerr != nil {
		t.Logf("cursor (first:2) ERR: %v", cerr)
	} else {
		t.Logf("cursor (first:2) OK: %s", cres.Data)
	}
}

// TestClickHouseCursor validates keyset cursor pagination — paging through
// products id 1..6, two per page, must yield exactly [1 2 3 4 5 6].
func TestClickHouseCursor(t *testing.T) {
	if dbType != "clickhouse" {
		t.Skipf("skipping for %s", dbType)
	}
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, SecretKey: "ch_test_secret"})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	gql := `query($cursor: Cursor) {
		products(first: 2, after: $cursor, where: { id: { lteq: 6 } }, order_by: { id: asc }) {
			id
		}
		products_cursor
	}`

	cursorVar := "null"
	var ids []int
	for i := 0; i < 6; i++ { // safety bound
		vars := json.RawMessage(`{"cursor": ` + cursorVar + `}`)
		res, err := gj.GraphQL(ctx, gql, vars, nil)
		if err != nil {
			t.Fatalf("page %d: %v", i, err)
		}
		var page struct {
			Products []struct {
				ID int `json:"id"`
			} `json:"products"`
			Cursor string `json:"products_cursor"`
		}
		if err := json.Unmarshal(res.Data, &page); err != nil {
			t.Fatalf("unmarshal %s: %v", res.Data, err)
		}
		if len(page.Products) == 0 {
			break
		}
		for _, p := range page.Products {
			ids = append(ids, p.ID)
		}
		if page.Cursor == "" {
			break
		}
		cursorVar = `"` + page.Cursor + `"`
	}

	want := []int{1, 2, 3, 4, 5, 6}
	if len(ids) != len(want) {
		t.Fatalf("cursor paging yielded %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("cursor paging yielded %v, want %v", ids, want)
		}
	}
}

// TestClickHouseExprAggregate validates an expression aggregate: sum(quantity * 2).
func TestClickHouseExprAggregate(t *testing.T) {
	if dbType != "clickhouse" {
		t.Skipf("skipping for %s", dbType)
	}
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	gql := `query { purchases { total: sum(expr: { mul: [quantity, 2] }) } }`
	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		t.Fatalf("expr aggregate: %v", err)
	}
	var out struct {
		Purchases []struct {
			Total int `json:"total"`
		} `json:"purchases"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("unmarshal %s: %v", res.Data, err)
	}
	if len(out.Purchases) != 1 || out.Purchases[0].Total != 101000 { // 2 * sum(10+20+...+1000)
		t.Errorf("sum(quantity*2) = %+v, want 101000", out.Purchases)
	}
}

// TestClickHouseWindow validates analytic window directives over products 1..3
// (prices ascend with id: 11.5, 12.5, 13.5). It covers the ranking path
// (rank() OVER) and the aggregate-frame path (sum() OVER … ROWS UNBOUNDED
// PRECEDING), the latter exercising ClickHouse's single-bound frame shorthand.
func TestClickHouseWindow(t *testing.T) {
	if dbType != "clickhouse" {
		t.Skipf("skipping for %s", dbType)
	}
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("rank", func(t *testing.T) {
		gql := `query {
			products(where: { id: { lt: 4 } }, order_by: { id: asc }) {
				id
				score: price @rank(order: desc)
			}
		}`
		res, err := gj.GraphQL(context.Background(), gql, nil, nil)
		if err != nil {
			t.Fatalf("rank: %v", err)
		}
		var out struct {
			Products []struct {
				ID    int `json:"id"`
				Score int `json:"score"`
			} `json:"products"`
		}
		if err := json.Unmarshal(res.Data, &out); err != nil {
			t.Fatalf("unmarshal %s: %v", res.Data, err)
		}
		want := map[int]int{1: 3, 2: 2, 3: 1} // descending rank of ascending price
		if len(out.Products) != 3 {
			t.Fatalf("products = %d, want 3", len(out.Products))
		}
		for _, p := range out.Products {
			if want[p.ID] != p.Score {
				t.Errorf("product %d rank = %d, want %d", p.ID, p.Score, want[p.ID])
			}
		}
	})

	t.Run("running_sum", func(t *testing.T) {
		gql := `query {
			products(where: { id: { lt: 4 } }, order_by: { id: asc }) {
				id
				rt: price @running(orderBy: { id: asc }, aggregate: sum)
			}
		}`
		res, err := gj.GraphQL(context.Background(), gql, nil, nil)
		if err != nil {
			t.Fatalf("running: %v", err)
		}
		var out struct {
			Products []struct {
				ID int     `json:"id"`
				RT float64 `json:"rt"`
			} `json:"products"`
		}
		if err := json.Unmarshal(res.Data, &out); err != nil {
			t.Fatalf("unmarshal %s: %v", res.Data, err)
		}
		want := map[int]float64{1: 11.5, 2: 24.0, 3: 37.5} // cumulative sum of price
		if len(out.Products) != 3 {
			t.Fatalf("products = %d, want 3", len(out.Products))
		}
		for _, p := range out.Products {
			if want[p.ID] != p.RT {
				t.Errorf("product %d running sum = %v, want %v", p.ID, p.RT, want[p.ID])
			}
		}
	})
}

// TestClickHouseNullableCursor validates keyset pagination over a NULLABLE column:
// paging users by phone (non-null for ids 1-50, NULL for 51-100) must return every
// user exactly once — no skips/dupes across the null boundary.
func TestClickHouseNullableCursor(t *testing.T) {
	if dbType != "clickhouse" {
		t.Skipf("skipping for %s", dbType)
	}
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, SecretKey: "ch_test_secret"})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	gql := `query($cursor: Cursor) {
		users(first: 13, after: $cursor, order_by: { phone: asc }) { id }
		users_cursor
	}`

	cursorVar := "null"
	seen := map[int]int{}
	for i := 0; i < 12; i++ { // ~8 pages for 100 rows; bounded
		vars := json.RawMessage(`{"cursor": ` + cursorVar + `}`)
		res, err := gj.GraphQL(ctx, gql, vars, nil)
		if err != nil {
			t.Fatalf("page %d: %v", i, err)
		}
		var page struct {
			Users []struct {
				ID int `json:"id"`
			} `json:"users"`
			Cursor string `json:"users_cursor"`
		}
		if err := json.Unmarshal(res.Data, &page); err != nil {
			t.Fatalf("unmarshal %s: %v", res.Data, err)
		}
		if len(page.Users) == 0 {
			break
		}
		for _, u := range page.Users {
			seen[u.ID]++
		}
		if page.Cursor == "" {
			break
		}
		cursorVar = `"` + page.Cursor + `"`
	}
	if len(seen) != 100 {
		t.Fatalf("nullable keyset returned %d unique users, want 100 (no skips/dupes across null boundary)", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("user %d returned %d times, want 1", id, n)
		}
	}
}

// TestClickHouseNestedAggregate validates an aggregate child (grouped by join col).
func TestClickHouseNestedAggregate(t *testing.T) {
	if dbType != "clickhouse" {
		t.Skipf("skipping for %s", dbType)
	}
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	gql := `query {
		products(where: { id: { lt: 3 } }, order_by: { id: asc }) {
			id
			comments { count_id }
		}
	}`
	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		t.Fatalf("nested aggregate: %v", err)
	}
	var out struct {
		Products []struct {
			ID       int `json:"id"`
			Comments []struct {
				CountID int `json:"count_id"`
			} `json:"comments"`
		} `json:"products"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("unmarshal %s: %v", res.Data, err)
	}
	if len(out.Products) != 2 {
		t.Fatalf("products = %d, want 2", len(out.Products))
	}
	for _, p := range out.Products {
		if len(p.Comments) != 1 || p.Comments[0].CountID != 3 {
			t.Errorf("product %d comments aggregate = %+v, want count_id 3", p.ID, p.Comments)
		}
	}
}

// TestClickHouseNestedLimit validates per-parent nested LIMIT: each parent keeps
// its own first N children (not N across the whole IN-chunk). Products 1 and 2
// each have 3 comments; limit 2 must yield 2 per product.
func TestClickHouseNestedLimit(t *testing.T) {
	if dbType != "clickhouse" {
		t.Skipf("skipping for %s", dbType)
	}
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	gql := `query {
		products(where: { id: { lt: 3 } }, order_by: { id: asc }) {
			id
			comments(limit: 2, order_by: { id: asc }) { id }
		}
	}`
	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		t.Fatalf("nested limit: %v", err)
	}
	var out struct {
		Products []struct {
			ID       int `json:"id"`
			Comments []struct {
				ID int `json:"id"`
			} `json:"comments"`
		} `json:"products"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("unmarshal %s: %v", res.Data, err)
	}
	if len(out.Products) != 2 {
		t.Fatalf("products = %d, want 2", len(out.Products))
	}
	for _, p := range out.Products {
		if len(p.Comments) != 2 {
			t.Errorf("product %d has %d comments, want 2 (per-parent limit)", p.ID, len(p.Comments))
		}
	}
	if out.Products[0].Comments[0].ID != 1 || out.Products[0].Comments[1].ID != 201 {
		t.Errorf("product 1 comments = %+v, want [1 201]", out.Products[0].Comments)
	}
}

// TestClickHouseReads validates introspection + dialect + driver + N+1 assembly
// end-to-end against a live ClickHouse.
func TestClickHouseReads(t *testing.T) {
	if dbType != "clickhouse" {
		t.Skipf("skipping for %s", dbType)
	}
	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}

	gql := `query {
		products(id: 1) {
			id
			name
			owner { id full_name }
			comments { id }
		}
	}`
	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	if err != nil {
		t.Fatalf("graphql: %v", err)
	}

	var out struct {
		Products struct {
			ID    int    `json:"id"`
			Name  string `json:"name"`
			Owner struct {
				ID       int    `json:"id"`
				FullName string `json:"full_name"`
			} `json:"owner"`
			Comments []struct {
				ID int `json:"id"`
			} `json:"comments"`
		} `json:"products"`
	}
	if err := json.Unmarshal(res.Data, &out); err != nil {
		t.Fatalf("unmarshal %s: %v", res.Data, err)
	}
	if out.Products.ID != 1 || out.Products.Name != "Product 1" {
		t.Errorf("product = %+v, want id 1 / 'Product 1'", out.Products)
	}
	if out.Products.Owner.ID != 1 || out.Products.Owner.FullName != "User 1" {
		t.Errorf("owner (one-to-one) = %+v, want id 1 / 'User 1'", out.Products.Owner)
	}
	hasComment1 := false
	for _, c := range out.Products.Comments {
		if c.ID == 1 {
			hasComment1 = true
		}
	}
	if len(out.Products.Comments) == 0 || !hasComment1 {
		t.Errorf("comments (one-to-many) = %+v, want to include id 1", out.Products.Comments)
	}

	// Analytics (OLAP) — global aggregate.
	ares, err := gj.GraphQL(context.Background(), `query { purchases { count_id sum_quantity avg_quantity } }`, nil, nil)
	if err != nil {
		t.Fatalf("global aggregate: %v", err)
	}
	var agg struct {
		Purchases []struct {
			CountID     int     `json:"count_id"`
			SumQuantity int     `json:"sum_quantity"`
			AvgQuantity float64 `json:"avg_quantity"`
		} `json:"purchases"`
	}
	if err := json.Unmarshal(ares.Data, &agg); err != nil {
		t.Fatalf("unmarshal %s: %v", ares.Data, err)
	}
	if len(agg.Purchases) != 1 || agg.Purchases[0].CountID != 100 ||
		agg.Purchases[0].SumQuantity != 50500 || agg.Purchases[0].AvgQuantity != 505 {
		t.Errorf("global aggregate = %+v, want count 100 / sum 50500 / avg 505", agg.Purchases)
	}

	// Analytics — group-by with ordering + limit.
	gres, err := gj.GraphQL(context.Background(),
		`query { purchases(distinct: [product_id], order_by: { product_id: asc }, limit: 3) { product_id sum_quantity } }`, nil, nil)
	if err != nil {
		t.Fatalf("group-by: %v", err)
	}
	var grp struct {
		Purchases []struct {
			ProductID   int `json:"product_id"`
			SumQuantity int `json:"sum_quantity"`
		} `json:"purchases"`
	}
	if err := json.Unmarshal(gres.Data, &grp); err != nil {
		t.Fatalf("unmarshal %s: %v", gres.Data, err)
	}
	if len(grp.Purchases) != 3 || grp.Purchases[0].ProductID != 1 || grp.Purchases[0].SumQuantity != 10 {
		t.Errorf("group-by = %+v, want 3 rows starting {1, 10}", grp.Purchases)
	}
}
