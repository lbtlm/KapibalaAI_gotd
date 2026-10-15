package gateway

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	_ "github.com/jackc/pgx/v5/stdlib"
	"log/slog"
	"sync"
)

// ErrWriterLeaseLost requires a process restart before further gateway effects.
var ErrWriterLeaseLost = errors.New("gateway PostgreSQL writer lease lost")

// PostgresStore owns one gateway writer lease and persists atomic snapshots.
// The gateway has one active platform client per account, so a second writer
// fails startup rather than duplicating Telegram connections or effects.
type PostgresStore struct {
	DB    *sql.DB
	lease *sql.Conn
	mu    sync.Mutex
	lost  bool
}

// OpenPostgres migrates only gateway-prefixed tables and acquires a process lease.
func OpenPostgres(ctx context.Context, dsn string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(12)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("gateway database: %w", err)
	}
	lease, err := db.Conn(ctx)
	if err != nil {
		db.Close()
		return nil, err
	}
	var ok bool
	if err = lease.QueryRowContext(ctx, "SELECT pg_try_advisory_lock(761231912)").Scan(&ok); err != nil || !ok {
		lease.Close()
		db.Close()
		return nil, errors.New("gateway writer already running or lease unavailable")
	}
	for _, query := range []string{
		`CREATE TABLE IF NOT EXISTS gateway_state (id integer PRIMARY KEY CHECK (id=1), data bytea NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS gateway_sessions (account_id text PRIMARY KEY, data bytea NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS gateway_retired_accounts (account_id text PRIMARY KEY)`,
		`CREATE TABLE IF NOT EXISTS gateway_account_configs (account_id text PRIMARY KEY, authorization_fingerprint text UNIQUE NOT NULL, configuration_fingerprint text NOT NULL, config bytea NOT NULL)`,
		`ALTER TABLE gateway_account_configs ADD COLUMN IF NOT EXISTS source text NOT NULL DEFAULT 'upload' CHECK (source IN ('upload','manifest'))`,
		`CREATE TABLE IF NOT EXISTS gateway_update_state (account_id text NOT NULL, user_id bigint NOT NULL, pts integer NOT NULL, qts integer NOT NULL, date integer NOT NULL, seq integer NOT NULL, PRIMARY KEY(account_id,user_id))`,
		`CREATE TABLE IF NOT EXISTS gateway_channel_state (account_id text NOT NULL, user_id bigint NOT NULL, channel_id bigint NOT NULL, pts integer NOT NULL DEFAULT 0, access_hash bigint NOT NULL DEFAULT 0, PRIMARY KEY(account_id,user_id,channel_id))`,
	} {
		if _, err = db.ExecContext(ctx, query); err != nil {
			lease.Close()
			db.Close()
			return nil, fmt.Errorf("gateway migration: %w", err)
		}
	}
	return &PostgresStore{DB: db, lease: lease}, nil
}
func (p *PostgresStore) Load(ctx context.Context) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lost {
		return nil, ErrWriterLeaseLost
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var b []byte
	err := p.lease.QueryRowContext(ctx, "SELECT data FROM gateway_state WHERE id=1").Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return b, err
}
func (p *PostgresStore) Save(ctx context.Context, b []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lost {
		return ErrWriterLeaseLost
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	result, err := p.lease.ExecContext(ctx, `INSERT INTO gateway_state(id,data) SELECT 1,$1 WHERE EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND pid=pg_backend_pid() AND classid=0 AND objid=761231912 AND database=(SELECT oid FROM pg_database WHERE datname=current_database())) ON CONFLICT(id) DO UPDATE SET data=EXCLUDED.data`, b)
	if err != nil {
		return p.loseLease("save", err)
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return p.loseLease("save_fence", err)
	}
	return nil
}

// CheckLease verifies ownership on the fixed connection, never a pooled replacement.
func (p *PostgresStore) CheckLease(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.checkLeaseLocked(ctx)
}
func (p *PostgresStore) checkLeaseLocked(ctx context.Context) error {
	if p.lost {
		return ErrWriterLeaseLost
	}
	// Waiting for another fenced write can consume the caller's deadline.
	// No SQL was attempted: cancellation says nothing about lease ownership.
	if err := ctx.Err(); err != nil {
		return err
	}
	var owned bool
	err := p.lease.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND pid=pg_backend_pid() AND classid=0 AND objid=761231912 AND database=(SELECT oid FROM pg_database WHERE datname=current_database()))`).Scan(&owned)
	if err != nil || !owned {
		return p.loseLease("check", err)
	}
	return nil
}

// ExecFenced writes SDK state on the same session that owns the gateway lease.
func (p *PostgresStore) ExecFenced(ctx context.Context, query string, args ...any) (sql.Result, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e := p.checkLeaseLocked(ctx); e != nil {
		return nil, e
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result, e := p.lease.ExecContext(ctx, query, args...)
	if e != nil {
		return nil, p.loseLease("update", e)
	}
	return result, nil
}

// Caller holds mu. Log classifications only: SQL errors can contain live data.
func (p *PostgresStore) loseLease(operation string, err error) error {
	p.lost = true
	cause := fmt.Sprintf("%T", err)
	switch {
	case err == nil:
		cause = "ownership_missing"
	case errors.Is(err, context.Canceled):
		cause = "context_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		cause = "deadline_exceeded"
	case errors.Is(err, sql.ErrConnDone), errors.Is(err, driver.ErrBadConn):
		cause = "connection_closed"
	}
	slog.Error("gateway writer lease lost", "operation", operation, "cause", cause)
	return fmt.Errorf("%w: %s", ErrWriterLeaseLost, cause)
}
func (p *PostgresStore) Close() error {
	if p.lease != nil {
		_, _ = p.lease.ExecContext(context.Background(), "SELECT pg_advisory_unlock(761231912)")
		_ = p.lease.Close()
	}
	return p.DB.Close()
}
