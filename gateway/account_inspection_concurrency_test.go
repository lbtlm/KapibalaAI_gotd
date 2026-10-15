package gateway

import (
	"context"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gotd/td/tg"
)

type startupInspectionConnection struct {
	*inspectionConnection
	armed   atomic.Bool
	entered chan struct{}
	release chan struct{}
	ready   chan struct{}
}

func (a *startupInspectionConnection) Connect(ctx context.Context, id string) (string, error) {
	pid, err := a.inspectionConnection.Connect(ctx, id)
	if err != nil || !a.armed.CompareAndSwap(true, false) {
		return pid, err
	}
	close(a.entered)
	<-a.release
	// Actual durable update processing uses the same Service lock as SDK startup.
	pipeline := a.Updates(id, tg.NewClient(a.remote), func() {})
	if err = pipeline.Handler.Handle(ctx, &tg.Updates{}); err != nil {
		return "", err
	}
	close(a.ready)
	return pid, nil
}

func TestAccountInspectionDoesNotDeadlockAnotherAccountStartupUpdates(t *testing.T) {
	for _, operation := range []string{"check", "joined-groups"} {
		t.Run(operation, func(t *testing.T) {
			remote := &inspectionRemote{}
			base, err := NewTelegramAdapter(context.Background(), nil, []AccountConfig{{ID: "account-1", APIID: 1, APIHash: "synthetic", SessionPath: "unused"}, {ID: "account-2", APIID: 1, APIHash: "synthetic", SessionPath: "unused"}})
			if err != nil {
				t.Fatal(err)
			}
			a := &startupInspectionConnection{inspectionConnection: &inspectionConnection{base, remote}, entered: make(chan struct{}), release: make(chan struct{}), ready: make(chan struct{})}
			s, err := New(context.Background(), Config{Mode: "fake", Store: NewMemoryStore(), Telegram: a})
			if err != nil {
				t.Fatal(err)
			}
			ts := httptest.NewServer(s)
			defer func() { ts.Close(); s.Close() }()
			if status, _ := rpcCall(t, ts, "POST", "/accounts/account-1/connect", nil); status != 200 {
				t.Fatal(status)
			}
			inspecting := make(chan struct{}, 1)
			ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
			defer cancel()
			waitStartup := func(ctx context.Context) error {
				select {
				case inspecting <- struct{}{}:
				default:
				}
				select {
				case <-a.ready:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			remote.mu.Lock()
			if operation == "check" {
				remote.onSelf = waitStartup
			} else {
				remote.onPage = func(*tg.MessagesGetDialogsRequest) error { return waitStartup(ctx) }
			}
			remote.mu.Unlock()
			a.armed.Store(true)
			connectResult := make(chan int, 1)
			go func() {
				r := httptest.NewRecorder()
				s.ServeHTTP(r, httptest.NewRequest("POST", "/accounts/account-2/connect", nil).WithContext(ctx))
				connectResult <- r.Code
			}()
			<-a.entered
			readResult := make(chan int, 1)
			go func() {
				method, path := "POST", "/accounts/account-1/check"
				if operation == "joined-groups" {
					method, path = "GET", "/accounts/account-1/joined-groups"
				}
				r := httptest.NewRecorder()
				s.ServeHTTP(r, httptest.NewRequest(method, path, nil).WithContext(ctx))
				readResult <- r.Code
			}()
			select {
			case <-inspecting:
			case <-ctx.Done():
				close(a.release)
				t.Fatal("inspection never reached remote")
			}
			close(a.release)
			select {
			case code := <-readResult:
				if code != 200 {
					t.Errorf("inspection blocked startup: %d", code)
				}
			case <-time.After(time.Second):
				t.Fatal("inspection deadlock")
			}
			select {
			case code := <-connectResult:
				if code != 200 {
					t.Errorf("startup blocked inspection: %d", code)
				}
			case <-time.After(time.Second):
				t.Fatal("startup deadlock")
			}
		})
	}
}
