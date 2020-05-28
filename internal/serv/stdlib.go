package serv

import (
	"context"
	"database/sql"
	"sync"
	"time"

	errors "golang.org/x/xerrors"

	"github.com/jackc/pgx/v4"
)

type ctxKey int

var ctxKeyFakeTx ctxKey = 0

var errNotPgx = errors.New("not pgx *sql.DB")

var (
	fakeTxMutex sync.Mutex
	fakeTxConns map[*pgx.Conn]*sql.Tx
)

func acquireConn(db *sql.DB) (*pgx.Conn, error) {
	var conn *pgx.Conn
	ctx := context.WithValue(context.Background(), ctxKeyFakeTx, &conn)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if conn == nil {
		if err := tx.Rollback(); err != nil {
			return nil, err
		}
		return nil, errNotPgx
	}

	fakeTxMutex.Lock()
	fakeTxConns[conn] = tx
	fakeTxMutex.Unlock()

	return conn, nil
}

func releaseConn(db *sql.DB, conn *pgx.Conn) error {
	var tx *sql.Tx
	var ok bool

	if conn.PgConn().IsBusy() || conn.PgConn().TxStatus() != 'I' {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		conn.Close(ctx)
	}

	fakeTxMutex.Lock()
	tx, ok = fakeTxConns[conn]
	if ok {
		delete(fakeTxConns, conn)
		fakeTxMutex.Unlock()
	} else {
		fakeTxMutex.Unlock()
		return errors.Errorf("can't release conn that is not acquired")
	}

	return tx.Rollback()
}
