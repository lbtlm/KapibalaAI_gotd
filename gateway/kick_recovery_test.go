package gateway

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestKickKeyReplayAfterRejoinNeverRemovesAgain(t *testing.T) {
	store := testPG(t)
	remote := &remoteTelegram{messages: map[int64]int{}, targetPresent: true}
	s, _, ts := updateTestService(t, store, store.DB, remote)
	in := map[string]string{"byAccountId": "account-1", "targetPlatformUserId": "90001", "idempotencyKey": "run-1:tu-1"}
	status, result := rpcCall(t, ts, "POST", "/groups/tg-321/kick", in)
	if status != 200 || result["kicked"] != true {
		ts.Close()
		s.Close()
		t.Fatal(status, result)
	}
	remote.mu.Lock()
	remote.targetPresent = true
	remote.mu.Unlock()
	status, _ = rpcCall(t, ts, "POST", "/admin/events", map[string]any{"type": "member_joined", "data": map[string]string{"groupId": "tg-321", "platformUserId": "90001"}})
	if status != 200 {
		ts.Close()
		s.Close()
		t.Fatal(status)
	}
	ts.Close()
	s.Close()
	store = testPG(t)
	adapter := newTestRPCConnection(t, remote, store.DB)
	s, e := New(context.Background(), Config{Mode: "fake", Store: store, Telegram: adapter})
	if e != nil {
		t.Fatal(e)
	}
	ts = httptest.NewServer(s)
	defer func() { ts.Close(); s.Close() }()
	_, _ = adapter.Connect(context.Background(), "account-1")
	status, result = rpcCall(t, ts, "POST", "/groups/tg-321/kick", in)
	if status != 200 || result["kicked"] != true {
		t.Fatal(status, result)
	}
	_, _ = rpcCall(t, ts, "POST", "/accounts/account-1/disconnect", nil)
	status, result = rpcCall(t, ts, "POST", "/groups/tg-321/kick", in)
	if status != 200 || result["kicked"] != true {
		t.Fatal("offline actor denied original completion", status, result)
	}
	changed := map[string]string{"byAccountId": "account-1", "targetPlatformUserId": "90002", "idempotencyKey": "run-1:tu-1"}
	status, _ = rpcCall(t, ts, "POST", "/groups/tg-321/kick", changed)
	if status != 409 {
		t.Fatal("key accepted changed target", status)
	}
	remote.mu.Lock()
	defer remote.mu.Unlock()
	if remote.kickCalls != 1 || !remote.targetPresent {
		t.Fatalf("replay kicked rejoined target: calls=%d present=%v", remote.kickCalls, remote.targetPresent)
	}
}

func TestKickUnknownResultCannotRetryAgainstRejoinedTarget(t *testing.T) {
	store := testPG(t)
	fault := &updateFailStore{PostgresStore: store}
	remote := &remoteTelegram{messages: map[int64]int{}, targetPresent: true}
	s, _, ts := updateTestService(t, fault, store.DB, remote)
	remote.mu.Lock()
	remote.onKickAccept = func() { fault.fail.Store(true) }
	remote.mu.Unlock()
	in := map[string]string{"byAccountId": "account-1", "targetPlatformUserId": "90001", "idempotencyKey": "unknown-kick"}
	status, _ := rpcCall(t, ts, "POST", "/groups/tg-321/kick", in)
	if status != 503 {
		ts.Close()
		s.Close()
		t.Fatal("injected commit did not fail", status)
	}
	remote.mu.Lock()
	remote.targetPresent = true
	remote.mu.Unlock()
	ts.Close()
	s.Close()
	store = testPG(t)
	adapter := newTestRPCConnection(t, remote, store.DB)
	s, e := New(context.Background(), Config{Mode: "fake", Store: store, Telegram: adapter})
	if e != nil {
		t.Fatal(e)
	}
	ts = httptest.NewServer(s)
	defer func() { ts.Close(); s.Close() }()
	status, result := rpcCall(t, ts, "POST", "/groups/tg-321/kick", in)
	if status != 504 || result["code"] != "NETWORK_TIMEOUT" {
		t.Fatal("unknown kick was not fail-closed", status, result)
	}
	remote.mu.Lock()
	defer remote.mu.Unlock()
	if remote.kickCalls != 1 || !remote.targetPresent {
		t.Fatal("unknown key removed target again")
	}
}
