package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCanceledTelegramLifecycleWaiterCannotChangeLiveSession(t *testing.T) {
	for _, operation := range []string{"disconnect", "connect"} {
		t.Run(operation, func(t *testing.T) {
			store := testPG(t)
			base, err := NewTelegramAdapter(context.Background(), store.DB, []AccountConfig{
				{ID: "account-1", APIID: 1, APIHash: "synthetic", SessionPath: "never-opened"},
				{ID: "account-2", APIID: 1, APIHash: "synthetic", SessionPath: "never-opened"},
			})
			if err != nil {
				t.Fatal(err)
			}
			adapter := &acceptanceConnection{base, &remoteTelegram{}}
			s, err := New(context.Background(), Config{Mode: "fake", Store: store, AdminEnabled: true, Telegram: adapter})
			if err != nil {
				t.Fatal(err)
			}
			ts := httptest.NewServer(s)
			defer func() { ts.Close(); s.Close() }()
			if status, _ := rpcCall(t, ts, "POST", "/admin/reset", nil); status != 200 {
				t.Fatal(status)
			}
			if status, _ := rpcCall(t, ts, "POST", "/accounts/account-1/connect", nil); status != 200 {
				t.Fatal(status)
			}
			// The real Session SELECT holds the production lifecycle lock. Only the
			// test database is locked; the SELECT is canceled before any file/network use.
			tx, err := store.DB.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err := tx.Exec("LOCK TABLE gateway_sessions IN ACCESS EXCLUSIVE MODE"); err != nil {
				t.Fatal(err)
			}
			blockedCtx, cancelBlocked := context.WithCancel(context.Background())
			defer cancelBlocked()
			blocked := make(chan error, 1)
			go func() { _, err := base.Connect(blockedCtx, "account-2"); blocked <- err }()
			deadline := time.Now().Add(2 * time.Second)
			for {
				var waiting bool
				if err := store.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'SELECT data FROM gateway_sessions%')`).Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("production Session SELECT did not wait on injected table lock")
				}
				time.Sleep(10 * time.Millisecond)
			}
			oldCtx, cancelOld := context.WithCancel(context.Background())
			defer cancelOld()
			started := make(chan struct{})
			oldResult := make(chan error, 1)
			go func() {
				close(started)
				if operation == "disconnect" {
					oldResult <- base.Disconnect(oldCtx, "account-1")
				} else {
					_, err := base.Connect(oldCtx, "account-1")
					oldResult <- err
				}
			}()
			<-started
			select {
			case err := <-oldResult:
				t.Fatalf("lifecycle waiter bypassed active operation: %v", err)
			case <-time.After(20 * time.Millisecond):
			}
			cancelOld()
			cancelBlocked()
			select {
			case err := <-blocked:
				if err == nil {
					t.Fatal("blocking authentication did not cancel")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("blocking authentication did not stop")
			}
			_ = tx.Rollback()
			select {
			case err := <-oldResult:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("canceled waiter returned %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("canceled lifecycle waiter did not return")
			}
			if status, body := rpcCall(t, ts, "POST", "/groups", map[string]string{"creatorAccountId": "account-1"}); status != 200 {
				t.Errorf("canceled %s altered live session: %d %v", operation, status, body)
			}
			if status, _ := rpcCall(t, ts, "GET", "/health", nil); status != 200 {
				t.Fatal("canceling one request stopped healthy gateway", status)
			}
		})
	}
}

// The platform reply can lag cancellation even though its effect has finished.
// This is the external Adapter boundary; no Service lock or state is manipulated.
type delayedLifecycleReply struct {
	operation    string
	armed        atomic.Bool
	entered      chan struct{}
	release      chan struct{}
	connectCalls atomic.Int64
	effectCalls  atomic.Int64
}

func (a *delayedLifecycleReply) wait(operation string) {
	if operation == a.operation && a.armed.CompareAndSwap(true, false) {
		close(a.entered)
		<-a.release
	}
}
func (a *delayedLifecycleReply) Connect(context.Context, string) (string, error) {
	a.connectCalls.Add(1)
	a.wait("connect")
	return "10001", nil
}
func (a *delayedLifecycleReply) Disconnect(context.Context, string) error {
	a.wait("disconnect")
	return nil
}
func (a *delayedLifecycleReply) Execute(_ context.Context, op, _ string, _ input, _ *Service) (any, error) {
	a.effectCalls.Add(1)
	a.wait(op)
	return map[string]string{"groupId": "synthetic-created"}, nil
}
func (*delayedLifecycleReply) Close() error { return nil }

func TestCanceledHTTPAccountResultCannotOverwriteNewerLifecycleState(t *testing.T) {
	for _, operation := range []string{"disconnect", "connect"} {
		t.Run(operation, func(t *testing.T) {
			platform := &delayedLifecycleReply{operation: operation, entered: make(chan struct{}), release: make(chan struct{})}
			s, err := New(context.Background(), Config{Mode: "fake", Store: NewMemoryStore(), Telegram: platform})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			connect := httptest.NewRecorder()
			s.ServeHTTP(connect, httptest.NewRequest("POST", "/accounts/account-1/connect", nil))
			if connect.Code != 200 {
				t.Fatal(connect.Code)
			}
			platform.armed.Store(true)
			oldCtx, cancelOld := context.WithCancel(context.Background())
			defer cancelOld()
			oldResponse := httptest.NewRecorder()
			oldDone := make(chan struct{})
			go func() {
				s.ServeHTTP(oldResponse, httptest.NewRequest("POST", "/accounts/account-1/"+operation, nil).WithContext(oldCtx))
				close(oldDone)
			}()
			select {
			case <-platform.entered:
			case <-time.After(time.Second):
				t.Fatal("old lifecycle did not enter platform")
			}
			cancelOld()
			newOperation, wantEffect := "connect", 200
			if operation == "connect" {
				newOperation, wantEffect = "disconnect", 409
			}
			newResponse := httptest.NewRecorder()
			s.ServeHTTP(newResponse, httptest.NewRequest("POST", "/accounts/account-1/"+newOperation, nil))
			if newResponse.Code != 200 {
				t.Fatal("new lifecycle failed", newResponse.Code)
			}
			close(platform.release)
			select {
			case <-oldDone:
			case <-time.After(time.Second):
				t.Fatal("old HTTP handler did not return")
			}
			// Creating a group observes the HTTP account state through its public gate.
			create := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/groups", strings.NewReader(`{"creatorAccountId":"account-1"}`))
			s.ServeHTTP(create, req)
			if create.Code != wantEffect {
				t.Errorf("late %s overwrote newer %s: create=%d want=%d body=%s", operation, newOperation, create.Code, wantEffect, create.Body.String())
			}
			health := httptest.NewRecorder()
			s.ServeHTTP(health, httptest.NewRequest("GET", "/health", nil))
			if health.Code != 200 {
				t.Fatal("canceled lifecycle stopped gateway", health.Code)
			}
		})
	}
}

func TestCanceledAccountHTTPRequestDoesNotRevokePostgresWriter(t *testing.T) {
	store := testPG(t)
	s, err := New(context.Background(), Config{Mode: "fake", Store: store, AdminEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	reset := httptest.NewRecorder()
	s.ServeHTTP(reset, httptest.NewRequest("POST", "/admin/reset", nil))
	if reset.Code != 200 {
		t.Fatal(reset.Code)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response := httptest.NewRecorder()
	s.ServeHTTP(response, httptest.NewRequest("POST", "/accounts/account-1/connect", nil).WithContext(ctx))
	health := httptest.NewRecorder()
	s.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	if health.Code != 200 {
		t.Fatalf("a canceled account request revoked healthy writer: health=%d body=%s", health.Code, health.Body.String())
	}
}

func TestCanceledHTTPWorkDoesNotExecuteAfterServiceQueue(t *testing.T) {
	for _, queuedPath := range []string{"/accounts/account-1/connect", "/groups"} {
		t.Run(queuedPath, func(t *testing.T) {
			platform := &delayedLifecycleReply{operation: "create", entered: make(chan struct{}), release: make(chan struct{})}
			s, err := New(context.Background(), Config{Mode: "fake", Store: NewMemoryStore(), Telegram: platform})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			connected := httptest.NewRecorder()
			s.ServeHTTP(connected, httptest.NewRequest("POST", "/accounts/account-1/connect", nil))
			if connected.Code != 200 {
				t.Fatal(connected.Code)
			}
			platform.armed.Store(true)
			createDone := make(chan struct{})
			go func() {
				s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/groups", strings.NewReader(`{"creatorAccountId":"account-1"}`)))
				close(createDone)
			}()
			select {
			case <-platform.entered:
			case <-time.After(time.Second):
				t.Fatal("first public effect did not hold service queue")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started := make(chan struct{})
			accountDone := make(chan struct{})
			go func() {
				close(started)
				s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", queuedPath, strings.NewReader(`{"creatorAccountId":"account-1"}`)).WithContext(ctx))
				close(accountDone)
			}()
			<-started
			select {
			case <-accountDone:
				t.Fatal("account bypassed service queue")
			case <-time.After(20 * time.Millisecond):
			}
			cancel()
			close(platform.release)
			<-createDone
			select {
			case <-accountDone:
			case <-time.After(time.Second):
				t.Fatal("canceled account did not return")
			}
			if calls := platform.connectCalls.Load(); calls != 1 {
				t.Fatalf("canceled queued connect reached platform: calls=%d want1 initial call", calls)
			}
			if calls := platform.effectCalls.Load(); calls != 1 {
				t.Fatalf("canceled queued group creation reached platform: calls=%d want1 initial call", calls)
			}
		})
	}
}
