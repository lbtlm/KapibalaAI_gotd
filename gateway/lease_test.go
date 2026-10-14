package gateway_test

import (
	"context"
	"database/sql"
	gateway "github.com/lbtlm/KapibalaAI_gotd/gateway"
	"net/http/httptest"
	"os"
	"testing"
)

func TestLostPostgresWriterLeaseFailsClosed(t *testing.T) {
	dsn := os.Getenv("GATEWAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GATEWAY_TEST_DATABASE_URL required")
	}
	store, e := gateway.OpenPostgres(context.Background(), dsn)
	if e != nil {
		t.Fatal(e)
	}
	s, e := gateway.New(context.Background(), gateway.Config{Mode: "fake", Store: store, AdminEnabled: true})
	if e != nil {
		t.Fatal(e)
	}
	ts := httptest.NewServer(s)
	defer func() { ts.Close(); s.Close() }()
	call(t, ts, "POST", "/admin/reset", nil, 200)
	call(t, ts, "POST", "/accounts/account-1/connect", nil, 200)
	injector, e := sql.Open("pgx", dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer injector.Close()
	var killed bool
	e = injector.QueryRow(`SELECT pg_terminate_backend(pid) FROM pg_locks WHERE locktype='advisory' AND classid=0 AND objid=761231912 AND database=(SELECT oid FROM pg_database WHERE datname=current_database())`).Scan(&killed)
	if e != nil || !killed {
		t.Fatalf("inject lease disconnection: %v %v", killed, e)
	}
	call(t, ts, "POST", "/groups", map[string]string{"creatorAccountId": "account-1"}, 503)
	call(t, ts, "GET", "/health", nil, 503)
	replacement, e := gateway.OpenPostgres(context.Background(), dsn)
	if e != nil {
		t.Fatalf("replacement writer cannot acquire disconnected lease: %v", e)
	}
	defer replacement.Close()
}
