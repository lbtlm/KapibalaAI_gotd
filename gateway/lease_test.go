package gateway_test

import (
	"context"
	"testing"

	gateway "github.com/lbtlm/KapibalaAI_gotd/gateway"
)

func TestLostPostgresWriterLeaseFailsClosed(t *testing.T) {
	p := isolatedStore(t)
	s, server := realServer(t, p)
	defer s.Close()
	defer server.Close()
	var killed bool
	err := p.DB.QueryRow(`SELECT pg_terminate_backend(pid) FROM pg_locks WHERE locktype='advisory' AND classid=0 AND objid=761231912 AND database=(SELECT oid FROM pg_database WHERE datname=current_database())`).Scan(&killed)
	if err != nil || !killed {
		t.Fatal("could not terminate the actual test writer connection")
	}
	request(t, server, "GET", "/accounts/absent/registration", nil, 503)
	request(t, server, "GET", "/health", nil, 503)
	replacement, err := gateway.OpenPostgres(context.Background(), testDSN(t))
	if err != nil {
		t.Fatal("replacement writer could not acquire the lost lease")
	}
	replacement.Close()
}

func TestPostgresOfflineCommandCannotAcquireRunningGatewayLease(t *testing.T) {
	p := isolatedStore(t)
	s, server := realServer(t, p)
	defer s.Close()
	defer server.Close()
	other, err := gateway.OpenPostgres(context.Background(), testDSN(t))
	if err == nil {
		other.Close()
		t.Fatal("offline command acquired an active gateway writer lease")
	}
	request(t, server, "GET", "/health", nil, 200)
}
