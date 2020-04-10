package core

import (
	"context"
	"database/sql"
)

func setLocalUserID(c context.Context, tx *sql.Tx) error {
	var err error
	if v := c.Value(UserIDKey); v != nil {
		_, err = tx.Exec(`SET LOCAL "user.id" = ?`, v)
	}

	return err
}
