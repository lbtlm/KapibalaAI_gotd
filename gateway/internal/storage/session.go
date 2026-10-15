package storage

import (
	"context"
	"database/sql"
	"github.com/gotd/td/session"
)

type Session struct {
	DB        *sql.DB
	AccountID string
}

func (d Session) LoadSession(ctx context.Context) ([]byte, error) {
	var b []byte
	err := d.DB.QueryRowContext(ctx, "SELECT data FROM gateway_sessions WHERE account_id=$1", d.AccountID).Scan(&b)
	if err == sql.ErrNoRows {
		return nil, session.ErrNotFound
	}
	return b, err
}
func (d Session) StoreSession(ctx context.Context, b []byte) error {
	result, err := d.DB.ExecContext(ctx, "UPDATE gateway_sessions SET data=$2 WHERE account_id=$1", d.AccountID, b)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return session.ErrNotFound
	}
	return err
}
