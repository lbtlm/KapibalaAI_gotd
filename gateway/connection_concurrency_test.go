package gateway

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPostgresSlowConnectDoesNotBlockOtherAccounts(t *testing.T) {
	dsn := os.Getenv("GATEWAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GATEWAY_TEST_DATABASE_URL required")
	}
	if !strings.Contains(dsn, "inspection") && !strings.Contains(dsn, "test") {
		t.Fatal("isolated test database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p, err := OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	a := &TelegramAdapter{ctx: ctx, cancel: cancel, db: p.DB, configs: map[string]AccountConfig{"slow": {ID: "slow"}}, sessions: map[string]*onlineSession{}}
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "LOCK TABLE gateway_sessions IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() {
		_, err := a.Connect(context.WithValue(ctx, connectionIDKey{}, "slow-cid"), "slow")
		finished <- err
	}()
	defer func() { cancel(); tx.Rollback(); <-finished; a.Close() }()
	// Wait for the actual production LoadSession query to block on PostgreSQL.
	for {
		var waiting bool
		err = p.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'SELECT data FROM gateway_sessions%')").Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("Connect never reached storage")
		case <-time.After(10 * time.Millisecond):
		}
	}
	other := make(chan error, 1)
	go func() { _, err := a.Connect(context.Background(), "unregistered-other"); other <- err }()
	select {
	case err := <-other:
		if e := platformError(err); e.Code != "ACCOUNT_NOT_FOUND" {
			t.Fatalf("other account: %s", e.Code)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("one account's storage wait blocked another account's Connect")
	}
	duplicate := make(chan error, 1)
	go func() {
		_, err := a.Connect(context.WithValue(ctx, connectionIDKey{}, "slow-cid"), "slow")
		duplicate <- err
	}()
	select {
	case err := <-duplicate:
		if e := platformError(err); e.Code != "ACCOUNT_IN_USE" || failedConnectionStopped(err) {
			t.Fatal("duplicate acquired ownership of the pending connection")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("duplicate connection was not bounded")
	}
	stopCtx, stop := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer stop()
	if err := a.Disconnect(stopCtx, "slow"); err != nil {
		t.Fatalf("disconnect could not cancel pending storage: %v", err)
	}
}
