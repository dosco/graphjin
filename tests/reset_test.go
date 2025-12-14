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
	}

	if err != nil {
		t.Logf("Failed to reset DB: %v", err)
	}
}
