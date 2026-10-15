package gateway

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gotd/td/tg"
)

func TestConnectionIntentIsDurableBeforeAdapterAndResultLossReusesClient(t *testing.T) {
	store := &interruptedStore{MemoryStore: NewMemoryStore()}
	remote := &inspectionRemote{}
	base, err := NewTelegramAdapter(context.Background(), nil, []AccountConfig{{ID: "account-1", APIID: 1, APIHash: "synthetic", SessionPath: "not-opened"}})
	if err != nil {
		t.Fatal(err)
	}
	adapter := &inspectionConnection{base, remote}
	s, err := New(context.Background(), Config{Mode: "fake", Store: store, Telegram: adapter, AdminEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s)
	defer func() { ts.Close(); s.Close() }()
	store.failBeforeRPC.Store(true)
	if status, _ := rpcCall(t, ts, "POST", "/accounts/account-1/connect", map[string]string{"connectionId": "retry"}); status != 503 {
		t.Fatal(status)
	}
	if status, _ := rpcCall(t, ts, "POST", "/accounts/account-1/check", nil); status != 409 {
		t.Fatal("failed intent started platform client", status)
	}
	store.failResult.Store(true)
	if status, _ := rpcCall(t, ts, "POST", "/accounts/account-1/connect", map[string]string{"connectionId": "retry"}); status != 503 {
		t.Fatal(status)
	}
	if status, _ := rpcCall(t, ts, "POST", "/accounts/account-1/connect", map[string]string{"connectionId": "retry"}); status != 200 {
		t.Fatal(status)
	}
	// Production Connect sees the same session populated by the external fixture,
	// so the retry must not read any Session source or start a second client.
	if pid, err := base.Connect(context.WithValue(context.Background(), connectionIDKey{}, "retry"), "account-1"); err != nil || pid != "10001" {
		t.Fatalf("did not reuse managed client: %s %v", pid, err)
	}
	if status, _ := rpcCall(t, ts, "POST", "/accounts/account-1/check", nil); status != 200 {
		t.Fatal(status)
	}
}

func TestUncancelledOldDisconnectReplyCannotOfflineNewGeneration(t *testing.T) {
	platform := &delayedLifecycleReply{operation: "disconnect", entered: make(chan struct{}), release: make(chan struct{})}
	s, err := New(context.Background(), Config{Mode: "fake", Store: NewMemoryStore(), Telegram: platform})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	connect := func(cid string) int {
		r := httptest.NewRecorder()
		s.ServeHTTP(r, httptest.NewRequest("POST", "/accounts/account-1/connect", strings.NewReader(`{"connectionId":"`+cid+`"}`)))
		return r.Code
	}
	if connect("old") != 200 {
		t.Fatal("initial connect")
	}
	platform.armed.Store(true)
	old := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		s.ServeHTTP(old, httptest.NewRequest("POST", "/accounts/account-1/disconnect", nil))
		close(done)
	}()
	<-platform.entered
	current := httptest.NewRecorder()
	s.ServeHTTP(current, httptest.NewRequest("POST", "/accounts/account-1/disconnect", nil))
	if current.Code != 200 || connect("new") != 200 {
		close(platform.release)
		<-done
		t.Fatal("new lifecycle failed")
	}
	close(platform.release)
	<-done
	if old.Code != 409 {
		t.Fatalf("old result acknowledged new state: %d %s", old.Code, old.Body.String())
	}
	result := httptest.NewRecorder()
	s.ServeHTTP(result, httptest.NewRequest("POST", "/groups", strings.NewReader(`{"creatorAccountId":"account-1"}`)))
	if result.Code != 200 {
		t.Fatalf("old disconnect replaced new state: %d", result.Code)
	}
}

func TestJoinedGroupsDiscoveryDoesNotManageGroupsAndStopsAfterLeaseLoss(t *testing.T) {
	store := testPG(t)
	remote := &inspectionRemote{}
	base, err := NewTelegramAdapter(context.Background(), store.DB, []AccountConfig{{ID: "account-1", APIID: 1, APIHash: "synthetic", SessionPath: "not-opened"}})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	adapter := &inspectionConnection{base, remote}
	s, err := New(context.Background(), Config{Mode: "fake", Store: store, Telegram: adapter, AdminEnabled: true})
	if err != nil {
		store.Close()
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
	if status, _ := rpcCall(t, ts, "GET", "/accounts/account-1/joined-groups", nil); status != 200 {
		t.Fatal(status)
	}
	if status, _ := rpcCall(t, ts, "GET", "/groups/tg-11/members", nil); status != 404 {
		t.Fatal("discovery implicitly managed a group", status)
	}
	remote.mu.Lock()
	remote.onPage = func(req *tg.MessagesGetDialogsRequest) error {
		if req.OffsetID == 0 {
			var killed bool
			return store.DB.QueryRow(`SELECT pg_terminate_backend(pid) FROM pg_locks WHERE locktype='advisory' AND classid=0 AND objid=761231912 AND database=(SELECT oid FROM pg_database WHERE datname=current_database())`).Scan(&killed)
		}
		return nil
	}
	before := remote.dialogCalls
	remote.mu.Unlock()
	if status, body := rpcCall(t, ts, "GET", "/accounts/account-1/joined-groups", nil); status != 503 || body["items"] != nil {
		t.Fatalf("lease-lost partial snapshot: %d %v", status, body)
	}
	remote.mu.Lock()
	after := remote.dialogCalls
	remote.mu.Unlock()
	if after-before != 1 {
		t.Fatalf("page RPC crossed lost writer fence: %d", after-before)
	}
}
