package gateway

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

type queuePathKey struct{}
type notifyingLease struct {
	*PostgresStore
	passed chan string
}

func (n notifyingLease) CheckLease(ctx context.Context) error {
	e := n.PostgresStore.CheckLease(ctx)
	if path, ok := ctx.Value(queuePathKey{}).(string); ok && e == nil {
		select {
		case n.passed <- path:
		default:
		}
	}
	return e
}

// This adapter is the external platform boundary; counts prove effects are not issued.
type queuedPlatform struct {
	mu      sync.Mutex
	effects map[string]int
	entered chan struct{}
	release chan struct{}
}

func (*queuedPlatform) Connect(context.Context, string) (string, error) { return "10001", nil }
func (*queuedPlatform) Disconnect(context.Context, string) error        { return nil }
func (*queuedPlatform) Close() error                                    { return nil }
func (p *queuedPlatform) Execute(ctx context.Context, op, gid string, in input, s *Service) (any, error) {
	switch op {
	case "create":
		s.state.Groups["queued-group"] = &group{Owner: "account-1", Members: map[string]bool{"10001": true}, Admins: map[string]bool{}, Writable: true}
		return map[string]string{"groupId": "queued-group"}, nil
	case "members":
		close(p.entered)
		<-p.release
		return []map[string]string{{"platformUserId": "10001"}}, nil
	default:
		p.mu.Lock()
		p.effects[op]++
		p.mu.Unlock()
		return map[string]bool{"accepted": true}, nil
	}
}
func TestQueuedEffectsCannotCrossWriterHandover(t *testing.T) {
	dsn := os.Getenv("GATEWAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GATEWAY_TEST_DATABASE_URL required")
	}
	db, e := OpenPostgres(context.Background(), dsn)
	if e != nil {
		t.Fatal(e)
	}
	lease := notifyingLease{db, make(chan string, 32)}
	platform := &queuedPlatform{effects: map[string]int{}, entered: make(chan struct{}), release: make(chan struct{})}
	s, e := New(context.Background(), Config{Mode: "fake", AdminEnabled: true, Store: lease, Telegram: platform})
	if e != nil {
		t.Fatal(e)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), queuePathKey{}, r.URL.Path)))
	}))
	defer func() { ts.Close(); s.Close() }()
	request := func(method, path string, body any) int {
		var b bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&b).Encode(body)
		}
		req, _ := http.NewRequest(method, ts.URL+path, &b)
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			return 0
		}
		defer res.Body.Close()
		return res.StatusCode
	}
	if request("POST", "/admin/reset", nil) != 200 || request("POST", "/accounts/account-1/connect", nil) != 200 || request("POST", "/groups", map[string]string{"creatorAccountId": "account-1"}) != 200 {
		t.Fatal("fixture setup")
	}
	whileDrain := true
	for whileDrain {
		select {
		case <-lease.passed:
		default:
			whileDrain = false
		}
	}
	first := make(chan int, 1)
	go func() { first <- request("GET", "/groups/queued-group/members", nil) }()
	select {
	case <-platform.entered:
	case <-time.After(time.Second):
		t.Fatal("first request did not hold platform queue")
	}
	results := make(chan int, 3)
	for _, op := range []string{"join", "kick", "leave"} {
		op := op
		go func() {
			results <- request("POST", "/groups/queued-group/"+op, map[string]string{"accountId": "account-1", "byAccountId": "account-1", "targetPlatformUserId": "90001", "inviteLink": "synthetic"})
		}()
	}
	passed := map[string]bool{}
	deadline := time.After(2 * time.Second)
	for len(passed) < 3 {
		select {
		case path := <-lease.passed:
			if path != "/groups/queued-group/members" {
				passed[path] = true
			}
		case <-deadline:
			close(platform.release)
			t.Fatal("queued requests did not pass initial lease validation")
		}
	}
	injector, e := sql.Open("pgx", dsn)
	if e != nil {
		close(platform.release)
		t.Fatal(e)
	}
	defer injector.Close()
	var killed bool
	e = injector.QueryRow(`SELECT pg_terminate_backend(pid) FROM pg_locks WHERE locktype='advisory' AND classid=0 AND objid=761231912 AND database=(SELECT oid FROM pg_database WHERE datname=current_database())`).Scan(&killed)
	if e != nil || !killed {
		close(platform.release)
		t.Fatal("cannot inject writer handover", e)
	}
	replacement, e := OpenPostgres(context.Background(), dsn)
	if e != nil {
		close(platform.release)
		t.Fatal(e)
	}
	defer replacement.Close()
	close(platform.release)
	<-first
	for i := 0; i < 3; i++ {
		select {
		case status := <-results:
			if status != 503 {
				t.Errorf("stale queued response=%d", status)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("queued response did not complete")
		}
	}
	platform.mu.Lock()
	defer platform.mu.Unlock()
	for op, count := range platform.effects {
		if count != 0 {
			t.Errorf("stale platform %s calls=%d, want zero", op, count)
		}
	}
}
