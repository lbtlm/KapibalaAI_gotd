package gateway_test

import (
	"context"
	"errors"
	sdkstorage "github.com/lbtlm/KapibalaAI_gotd/gateway/internal/storage"
	"testing"
	"time"

	gateway "github.com/lbtlm/KapibalaAI_gotd/gateway/internal/gateway"
)

func TestCancelledStoreOperationDoesNotRetireHealthyWriter(t *testing.T) {
	for _, operation := range []string{"check", "save", "update", "load"} {
		t.Run(operation, func(t *testing.T) {
			p := isolatedStore(t)
			s, server := realServer(t, p)
			defer s.Close()
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			var err error
			switch operation {
			case "check":
				err = p.CheckLease(ctx)
			case "save":
				err = p.Save(ctx, []byte(`{"cancelled":true}`))
			case "update":
				_, err = p.ExecFenced(ctx, "SELECT 1")
			case "load":
				_, err = p.Load(ctx)
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled %s must return caller cancellation, got %v", operation, err)
			}
			request(t, server, "GET", "/health", nil, 200)
		})
	}
}

func TestBusyStoreDoesNotRetireHealthyWriter(t *testing.T) {
	p := isolatedStore(t)
	s, server := realServer(t, p)
	defer s.Close()
	defer server.Close()
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := p.ExecFenced(ctx, "SELECT pg_sleep(3) /* lease_queue_regression */")
		done <- err
	}()
	deadline := time.Now().Add(time.Second)
	for {
		var busy bool
		if err := p.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND query='SELECT pg_sleep(3) /* lease_queue_regression */' AND wait_event='PgSleep')`).Scan(&busy); err != nil {
			t.Fatal(err)
		}
		if busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("writer did not enter the controlled database wait")
		}
		time.Sleep(5 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := p.CheckLease(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expired queue wait must return caller deadline, got %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// Let the watchdog observe its own expired queue wait before checking service life.
	time.Sleep(300 * time.Millisecond)
	request(t, server, "GET", "/health", nil, 200)
}

func TestTimedOutDatabaseIOStillRetiresWriter(t *testing.T) {
	p := isolatedStore(t)
	s, server := realServer(t, p)
	defer s.Close()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := p.ExecFenced(ctx, "SELECT pg_sleep(2)")
	if !errors.Is(err, sdkstorage.ErrWriterLeaseLost) {
		t.Fatalf("uncertain database I/O must retire the writer, got %v", err)
	}
	request(t, server, "GET", "/health", nil, 503)
}

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
	request(t, server, "DELETE", "/accounts/absent", nil, 503)
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
