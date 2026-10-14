package gateway

import (
	"context"
	"errors"
	"github.com/gotd/td/tg"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTooLongResumesAfterLastPersistedBatch(t *testing.T) {
	store := testPG(t)
	remote := &remoteTelegram{messages: map[int64]int{}, omitHistoryID: 150, difference: &tg.UpdatesChannelDifferenceTooLong{Final: true, Dialog: &tg.Dialog{Peer: &tg.PeerChannel{ChannelID: 321}, TopMessage: 205, Pts: 90, NotifySettings: tg.PeerNotifySettings{}}}}
	s, adapter, ts := updateTestService(t, store, store.DB, remote)
	pipeline := adapter.Updates("account-1", tg.NewClient(remote), func() {})
	req := &tg.UpdatesGetChannelDifferenceRequest{Channel: &tg.InputChannel{ChannelID: 321, AccessHash: 64}, Filter: &tg.ChannelMessagesFilterEmpty{}, Pts: 7, Limit: 100}
	if _, e := pipeline.API.UpdatesGetChannelDifference(context.Background(), req); e == nil {
		t.Fatal("partial second batch acknowledged")
	}
	_, body := rpcCall(t, ts, "GET", "/groups/tg-321/recovery", nil)
	if rec := body["recovery"].(map[string]any); rec["throughId"] != float64(100) || rec["status"] != "blocked" {
		t.Fatal(body)
	}
	ts.Close()
	s.Close()
	store = testPG(t)
	remote.omitHistoryID = 0
	adapter = newTestRPCConnection(t, remote, store.DB)
	var e error
	s, e = New(context.Background(), Config{Mode: "fake", Store: store, Telegram: adapter})
	if e != nil {
		t.Fatal(e)
	}
	ts = httptest.NewServer(s)
	defer func() { ts.Close(); s.Close() }()
	pipeline = adapter.Updates("account-1", tg.NewClient(remote), func() {})
	if _, e = pipeline.API.UpdatesGetChannelDifference(context.Background(), req); e != nil {
		t.Fatal(e)
	}
	_, body = rpcCall(t, ts, "GET", "/groups/tg-321/recovery", nil)
	if rec := body["recovery"].(map[string]any); rec["throughId"] != float64(205) || rec["fromId"] != float64(101) {
		t.Fatal(body)
	}
	remote.mu.Lock()
	defer remote.mu.Unlock()
	if len(remote.historyRequests) != 4 || remote.historyRequests[2][0] != 101 || len(remote.historyRequests[3]) != 5 {
		t.Fatalf("resumed outside persisted batch boundary: %v", remote.historyRequests)
	}
}

func TestTooLongMembershipSnapshotDoesNotHoldServiceQueue(t *testing.T) {
	store := testPG(t)
	remote := &remoteTelegram{messages: map[int64]int{}, peerTop: 0}
	s, adapter, ts := updateTestService(t, store, store.DB, remote)
	defer func() { ts.Close(); s.Close() }()
	started := make(chan struct{})
	remote.mu.Lock()
	remote.onParticipants = func(ctx context.Context) error { close(started); <-ctx.Done(); return ctx.Err() }
	remote.mu.Unlock()
	pipeline := adapter.Updates("account-1", tg.NewClient(remote), func() {})
	pipeline.OnChannelTooLong(321)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("snapshot did not start")
	}
	client := &http.Client{Timeout: 200 * time.Millisecond}
	res, e := client.Get(ts.URL + "/groups/tg-321/messages/by-client-id/absent")
	s.cancel()
	if e != nil {
		t.Fatalf("one channel snapshot blocked unrelated query request: %v", e)
	}
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Fatal(res.StatusCode)
	}
}
func TestNewGapFailureReplacesOldCompletedEvidence(t *testing.T) {
	store := testPG(t)
	remote := &remoteTelegram{messages: map[int64]int{}, peerTop: 0}
	s, adapter, ts := updateTestService(t, store, store.DB, remote)
	defer func() { ts.Close(); s.Close() }()
	pipeline := adapter.Updates("account-1", tg.NewClient(remote), func() {})
	s.mu.Lock()
	s.state.Groups["tg-321"].Recovery = &HistoryRecovery{AccountID: "account-1", Status: "recovered_with_limits", ThroughID: 1}
	s.mu.Unlock()
	deadlineSeen := make(chan bool, 1)
	remote.mu.Lock()
	remote.onPeerDialogs = func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		deadlineSeen <- ok && time.Until(deadline) <= 30*time.Second
		return errors.New("unavailable channel")
	}
	remote.mu.Unlock()
	pipeline.OnChannelTooLong(321)
	select {
	case bounded := <-deadlineSeen:
		if !bounded {
			t.Error("metadata RPC lacks bounded recovery deadline")
		}
	case <-time.After(time.Second):
		t.Fatal("metadata RPC did not run")
	}
	deadline := time.Now().Add(time.Second)
	for {
		_, body := rpcCall(t, ts, "GET", "/groups/tg-321/recovery", nil)
		if rec, ok := body["recovery"].(map[string]any); ok && rec["status"] == "blocked" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("new gap hidden by previous completed evidence: %v", body)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTooLongRecoversExplicitMessageRangeOnce(t *testing.T) {
	store := testPG(t)
	remote := &remoteTelegram{messages: map[int64]int{}, difference: &tg.UpdatesChannelDifferenceTooLong{Final: true, Dialog: &tg.Dialog{Peer: &tg.PeerChannel{ChannelID: 321}, TopMessage: 3, Pts: 80, NotifySettings: tg.PeerNotifySettings{}}}, history: []tg.MessageClass{&tg.Message{ID: 1, PeerID: &tg.PeerChannel{ChannelID: 321}, FromID: &tg.PeerUser{UserID: 90001}, Date: 1700000000, Message: "history-one"}, &tg.Message{ID: 3, PeerID: &tg.PeerChannel{ChannelID: 321}, FromID: &tg.PeerUser{UserID: 90001}, Date: 1700000000, Message: "history-three"}}}
	s, adapter, ts := updateTestService(t, store, store.DB, remote)
	defer func() { ts.Close(); s.Close() }()
	pipeline := adapter.Updates("account-1", tg.NewClient(remote), func() {})
	req := &tg.UpdatesGetChannelDifferenceRequest{Channel: &tg.InputChannel{ChannelID: 321, AccessHash: 64}, Filter: &tg.ChannelMessagesFilterEmpty{}, Pts: 7, Limit: 100}
	for i := 0; i < 2; i++ {
		if _, e := pipeline.API.UpdatesGetChannelDifference(context.Background(), req); e != nil {
			t.Fatal(e)
		}
	}
	_, body := rpcCall(t, ts, "GET", "/groups/tg-321/recovery", nil)
	rec, ok := body["recovery"].(map[string]any)
	if !ok || rec["status"] != "recovered_with_limits" || rec["fromId"] != float64(1) || rec["throughId"] != float64(3) {
		t.Fatalf("channel gap silently skipped: %v", body)
	}
	assertMessageReplay(t, ts, "history-one")
	assertMessageReplay(t, ts, "history-three")
	remote.mu.Lock()
	defer remote.mu.Unlock()
	if len(remote.historyRequests) != 1 {
		t.Fatalf("reconciled range read again: %v", remote.historyRequests)
	}
}
func TestTooLongMissingRangeRemainsVisibleAndCannotCheckpoint(t *testing.T) {
	store := testPG(t)
	remote := &remoteTelegram{messages: map[int64]int{}, omitHistoryID: 2, difference: &tg.UpdatesChannelDifferenceTooLong{Final: true, Dialog: &tg.Dialog{Peer: &tg.PeerChannel{ChannelID: 321}, TopMessage: 3, Pts: 80, NotifySettings: tg.PeerNotifySettings{}}}}
	s, adapter, ts := updateTestService(t, store, store.DB, remote)
	defer func() { ts.Close(); s.Close() }()
	pipeline := adapter.Updates("account-1", tg.NewClient(remote), func() {})
	_, e := pipeline.API.UpdatesGetChannelDifference(context.Background(), &tg.UpdatesGetChannelDifferenceRequest{Channel: &tg.InputChannel{ChannelID: 321, AccessHash: 64}, Filter: &tg.ChannelMessagesFilterEmpty{}, Pts: 7, Limit: 100})
	if e == nil {
		t.Fatal("incomplete history treated as recovered")
	}
	if e = pipeline.Storage.SetChannelPts(context.Background(), 10001, 321, 80); e == nil {
		t.Fatal("incomplete gap advanced checkpoint")
	}
	_, body := rpcCall(t, ts, "GET", "/groups/tg-321/recovery", nil)
	rec, ok := body["recovery"].(map[string]any)
	if !ok || rec["status"] != "blocked" || rec["code"] != "CHANNEL_HISTORY_INCOMPLETE" {
		t.Fatal(body)
	}
}

func TestLiveTooLongCallbackRecoversRatherThanOnlyLogging(t *testing.T) {
	store := testPG(t)
	remote := &remoteTelegram{messages: map[int64]int{}, peerTop: 1, history: []tg.MessageClass{&tg.Message{ID: 1, PeerID: &tg.PeerChannel{ChannelID: 321}, FromID: &tg.PeerUser{UserID: 90001}, Date: 1700000000, Message: "callback-history"}}}
	s, adapter, ts := updateTestService(t, store, store.DB, remote)
	defer func() { ts.Close(); s.Close() }()
	pipeline := adapter.Updates("account-1", tg.NewClient(remote), func() {})
	pipeline.OnChannelTooLong(321)
	deadline := time.Now().Add(time.Second)
	var body map[string]any
	for {
		_, body = rpcCall(t, ts, "GET", "/groups/tg-321/recovery", nil)
		if rec, ok := body["recovery"].(map[string]any); ok && rec["status"] == "recovered_with_limits" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("live too-long callback did not reconcile: %v", body)
		}
		time.Sleep(10 * time.Millisecond)
	}
	assertMessageReplay(t, ts, "callback-history")
}
func TestGlobalTooLongRecoversOnlyRegisteredChannel(t *testing.T) {
	store := testPG(t)
	remote := &remoteTelegram{messages: map[int64]int{}, globalDifference: &tg.UpdatesDifferenceTooLong{Pts: 90}, peerTop: 1, history: []tg.MessageClass{&tg.Message{ID: 1, PeerID: &tg.PeerChannel{ChannelID: 321}, FromID: &tg.PeerUser{UserID: 90001}, Date: 1700000000, Message: "global-gap-history"}}}
	s, adapter, ts := updateTestService(t, store, store.DB, remote)
	defer func() { ts.Close(); s.Close() }()
	pipeline := adapter.Updates("account-1", tg.NewClient(remote), func() {})
	if _, e := pipeline.API.UpdatesGetDifference(context.Background(), &tg.UpdatesGetDifferenceRequest{Pts: 7, Qts: 0, Date: 1700000000}); e != nil {
		t.Fatal(e)
	}
	_, body := rpcCall(t, ts, "GET", "/groups/tg-321/recovery", nil)
	if rec, ok := body["recovery"].(map[string]any); !ok || rec["status"] != "recovered_with_limits" {
		t.Fatalf("global gap checkpoint could skip channel history: %v", body)
	}
	assertMessageReplay(t, ts, "global-gap-history")
}
