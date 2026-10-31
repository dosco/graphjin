package discovery

import (
	"context"
	"database/sql"
	"testing"

	core "github.com/dosco/graphjin/core/v3"
	_ "github.com/mattn/go-sqlite3"
)

func TestSQLiteDiscoveryCountsTablesInsteadOfIndexes(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		"CREATE TABLE orders(id INTEGER PRIMARY KEY, account_id INTEGER, state TEXT)",
		"CREATE TABLE customers(id INTEGER PRIMARY KEY)",
		"CREATE INDEX aa_full ON orders(account_id)",
		"CREATE INDEX bb_full ON orders(state)",
		"CREATE INDEX zz_partial ON orders(state) WHERE id <= 2",
		"INSERT INTO orders VALUES(1,1,'open'),(2,2,'open'),(3,0,'open'),(4,1,'open'),(5,2,'open'),(6,0,'open'),(7,1,'open'),(8,2,'open'),(9,0,'open'),(10,1,'open')",
		"INSERT INTO customers VALUES(1),(2),(3)",
		"ANALYZE",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	var orders, customers int64
	if err := db.QueryRow("SELECT COUNT(*) FROM orders").Scan(&orders); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM customers").Scan(&customers); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	t.Run("namespace rollup", func(t *testing.T) {
		rows, err := sqliteNamespaceRollup(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].TableCount != 2 || rows[0].ApproxRowTotal != orders+customers {
			t.Fatalf("got %+v, want two tables with %d rows", rows, orders+customers)
		}
	})
	t.Run("namespace table estimates", func(t *testing.T) {
		counts, err := sqliteNamespaceRowCounts(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if counts["orders"] != orders || counts["customers"] != customers {
			t.Fatalf("counts = %v, want orders=%d customers=%d", counts, orders, customers)
		}
	})
	t.Run("single table estimate", func(t *testing.T) {
		got, available := sqliteRowCount(ctx, db, &core.TableSchema{Name: "orders"})
		if !available || got != orders {
			t.Fatalf("orders count = %d available=%v, want %d", got, available, orders)
		}
	})
}
