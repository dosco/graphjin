package tests_test

import (
	"testing"
)

func resetDB(t *testing.T) {
	var err error
	switch dbType {
	case "postgres":
		_, err = db.Exec(`
			TRUNCATE TABLE users, products, purchases, comments, customers RESTART IDENTITY CASCADE;
		`)
	case "mysql":
		_, err = db.Exec(`
			SET FOREIGN_KEY_CHECKS = 0;
			TRUNCATE TABLE users;
			TRUNCATE TABLE products;
			TRUNCATE TABLE purchases;
			TRUNCATE TABLE comments;
			TRUNCATE TABLE customers;
			SET FOREIGN_KEY_CHECKS = 1;
		`)
	case "sqlite":
		_, err = db.Exec(`
			DELETE FROM users;
			DELETE FROM products;
			DELETE FROM purchases;
			DELETE FROM comments;
			DELETE FROM customers;
			DELETE FROM sqlite_sequence;
		`)
	case "oracle":
		// Oracle doesn't support multi-statement execution in a single Exec call
		// Delete in reverse order of foreign key dependencies
		db.Exec(`DELETE FROM quotations`)
		db.Exec(`DELETE FROM graph_edge`)
		db.Exec(`DELETE FROM graph_node`)
		db.Exec(`DELETE FROM chats`)
		db.Exec(`DELETE FROM comments`)
		db.Exec(`DELETE FROM notifications`)
		db.Exec(`DELETE FROM purchases`)
		db.Exec(`DELETE FROM products`)
		db.Exec(`DELETE FROM categories`)
		_, err = db.Exec(`DELETE FROM users`)
	case "mssql":
		// MSSQL: Delete in reverse order of foreign key dependencies
		db.Exec(`DELETE FROM quotations`)
		db.Exec(`DELETE FROM graph_edge`)
		db.Exec(`DELETE FROM graph_node`)
		db.Exec(`DELETE FROM chats`)
		db.Exec(`DELETE FROM comments`)
		db.Exec(`DELETE FROM notifications`)
		db.Exec(`DELETE FROM purchases`)
		db.Exec(`DELETE FROM products`)
		db.Exec(`DELETE FROM categories`)
		_, err = db.Exec(`DELETE FROM users`)
	}

	if err != nil {
		t.Logf("Failed to reset DB: %v", err)
	}
}
