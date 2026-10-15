package gateway

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
)

// The external Adapter seam observes the durable restore handed to Resume.
// It never constructs a Telegram client or reads a Session/manifest.
type upgradeConnection struct {
	*delayedLifecycleReply
	store Store
	mu    sync.Mutex
	calls map[string][]string
}

func (a *upgradeConnection) Connect(ctx context.Context, id string) (string, error) {
	cid, _ := ctx.Value(connectionIDKey{}).(string)
	a.mu.Lock()
	a.calls[id] = append(a.calls[id], cid)
	a.mu.Unlock()
	return "10001", nil
}
func (a *upgradeConnection) Resume(s *Service) {
	b, err := a.store.Load(context.Background())
	if err != nil {
		return
	}
	var snapshot struct {
		Accounts map[string]struct {
			Status       string `json:"status"`
			ConnectionID string `json:"connectionId"`
		} `json:"accounts"`
	}
	if json.Unmarshal(b, &snapshot) != nil {
		return
	}
	for id, account := range snapshot.Accounts {
		if account.Status == "online" {
			_, _ = a.Connect(context.WithValue(s.ctx, connectionIDKey{}, account.ConnectionID), id)
		}
	}
}
func (a *upgradeConnection) Inspect(context.Context, string, string) (any, error) {
	return AccountProfile{Source: "simulated", PlatformUserID: "10001"}, nil
}
func (a *upgradeConnection) connected(id string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.calls[id]...)
}

func TestPostgresLegacyOnlineUpgradeStopsBeforeResumeAndAcceptsExplicitNewID(t *testing.T) {
	dsn := os.Getenv("GATEWAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GATEWAY_TEST_DATABASE_URL required")
	}
	store, err := OpenPostgres(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	// Old-version JSON deliberately omits connectionId on legacy. Other rows,
	// membership and accepted work are fixture input at the public Store seam.
	legacy := []byte(`{"mode":"fake","accounts":{"legacy":{"platformId":"10001","status":"online"},"identified":{"platformId":"10002","status":"online","connectionId":"kept-id"},"terminal":{"platformId":"10003","status":"suspended"},"manual":{"platformId":"10004","status":"offline"}},"groups":{"preserved":{"owner":"legacy","members":{"10001":true,"10002":true},"admins":{},"writable":true}},"effects":{"pending":{"id":"pending","accountId":"legacy","groupId":"preserved","clientId":"queued-before-upgrade","state":"prepared"}},"createKeys":{},"seenUpdates":{}}`)
	if err = store.Save(context.Background(), legacy); err != nil {
		store.Close()
		t.Fatal(err)
	}
	var s *Service
	var ts *httptest.Server
	defer func() {
		if ts != nil {
			ts.Close()
		}
		if s != nil {
			s.Close()
		}
		cleanup, e := OpenPostgres(context.Background(), dsn)
		if e == nil {
			defer cleanup.Close()
			_, _ = cleanup.DB.Exec("DELETE FROM gateway_state")
		}
	}()
	for phase := 0; phase < 2; phase++ {
		adapter := &upgradeConnection{delayedLifecycleReply: &delayedLifecycleReply{}, store: store, calls: map[string][]string{}}
		s, err = New(context.Background(), Config{Mode: "fake", Store: store, Telegram: adapter})
		if err != nil {
			store.Close()
			t.Fatal(err)
		}
		ts = httptest.NewServer(s)
		if calls := adapter.connected("legacy"); len(calls) != 0 {
			t.Errorf("phase%d resumed legacy without a shared ID: %v", phase, calls)
		}
		if calls := adapter.connected("identified"); len(calls) != 1 || calls[0] != "kept-id" {
			t.Errorf("phase%d changed identified resume: %v", phase, calls)
		}
		if status, _ := rpcCall(t, ts, "POST", "/accounts/legacy/check", nil); status != 409 {
			t.Errorf("phase%d legacy remained online: %d", phase, status)
		}
		if status, _ := rpcCall(t, ts, "POST", "/accounts/identified/check", nil); status != 200 {
			t.Errorf("phase%d identified stopped: %d", phase, status)
		}
		if status, _ := rpcCall(t, ts, "POST", "/accounts/terminal/connect", nil); status != 403 {
			t.Errorf("terminal revived: %d", status)
		}
		b, e := store.Load(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		var persisted struct {
			Accounts map[string]struct {
				Status       string `json:"status"`
				ConnectionID string `json:"connectionId"`
			} `json:"accounts"`
			Groups map[string]struct {
				Members map[string]bool `json:"members"`
			} `json:"groups"`
			Effects map[string]struct {
				State string `json:"state"`
			} `json:"effects"`
		}
		if e = json.Unmarshal(b, &persisted); e != nil {
			t.Fatal(e)
		}
		if persisted.Accounts["legacy"].Status != "offline" || persisted.Accounts["legacy"].ConnectionID != "" || persisted.Accounts["manual"].Status != "offline" || persisted.Accounts["terminal"].Status != "suspended" {
			t.Errorf("phase%d unexpected durable upgrade: %+v", phase, persisted.Accounts)
		}
		if !persisted.Groups["preserved"].Members["10001"] || !persisted.Groups["preserved"].Members["10002"] || persisted.Effects["pending"].State != "prepared" {
			t.Fatal("upgrade discarded membership or pending effect")
		}
		if phase == 0 {
			ts.Close()
			s.Close()
			ts = nil
			s = nil
			store, err = OpenPostgres(context.Background(), dsn)
			if err != nil {
				t.Fatal(err)
			}
		} else {
			if status, body := rpcCall(t, ts, "POST", "/accounts/legacy/connect", map[string]string{"connectionId": "explicit-new"}); status != 200 || body["connectionId"] != "explicit-new" {
				t.Fatalf("explicit new connection failed: %d %v", status, body)
			}
			if status, _ := rpcCall(t, ts, "POST", "/accounts/legacy/disconnect", nil); status != 200 {
				t.Fatal(status)
			}
			if status, _ := rpcCall(t, ts, "POST", "/accounts/legacy/connect", map[string]string{"connectionId": "explicit-new"}); status != 409 {
				t.Fatal("ended ID revived", status)
			}
		}
	}
}
