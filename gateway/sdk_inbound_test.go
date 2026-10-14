package gateway

import (
	"context"
	"errors"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"
	"net/http/httptest"
	"testing"
	"time"
)

// This runs the real pinned gotd manager, including its startup/channel workers.
type subscriptionRemote struct {
	*remoteTelegram
	requests chan int
}

func (r *subscriptionRemote) Invoke(ctx context.Context, in bin.Encoder, out bin.Decoder) error {
	if req, ok := in.(*tg.UpdatesGetChannelDifferenceRequest); ok {
		select {
		case r.requests <- req.Pts:
		default:
		}
		if req.Pts == 0 {
			return errors.New("PERSISTENT_TIMESTAMP_EMPTY")
		}
		var b bin.Buffer
		response := &tg.UpdatesChannelDifferenceEmpty{Final: true, Pts: req.Pts}
		if e := response.Encode(&b); e != nil {
			return e
		}
		return out.Decode(&b)
	}
	return r.remoteTelegram.Invoke(ctx, in, out)
}
func TestRealSDKManagerRestoresRegisteredHashOnlyChannelBeforeSubscription(t *testing.T) {
	for _, mode := range []string{"first-hash-only", "legacy-zero-after-restart", "legacy-max-seen-after-restart"} {
		t.Run(mode, func(t *testing.T) {
			store := testPG(t)
			remote := &remoteTelegram{messages: map[int64]int{}, globalDifference: &tg.UpdatesDifferenceEmpty{Date: 1700000000, Seq: 1}, peerTop: 159, history: []tg.MessageClass{&tg.Message{ID: 159, PeerID: &tg.PeerChannel{ChannelID: 321}, FromID: &tg.PeerUser{UserID: 90001}, Date: 1700000000, Message: "existing-inbound-159"}}}
			s, adapter, ts := updateTestService(t, store, store.DB, remote)
			pipeline := adapter.Updates("account-1", tg.NewClient(remote), func() {})
			if _, e := store.ExecFenced(context.Background(), "DELETE FROM gateway_channel_state WHERE account_id=$1 AND user_id=$2 AND channel_id=$3", "account-1", 10001, 321); e != nil {
				t.Fatal(e)
			}
			if e := pipeline.Storage.SetState(context.Background(), 10001, updates.State{Pts: 7, Date: 1700000000, Seq: 1}); e != nil {
				t.Fatal(e)
			}
			if e := pipeline.AccessHasher.SetChannelAccessHash(context.Background(), 10001, 321, 64); e != nil {
				t.Fatal(e)
			}
			if mode == "legacy-max-seen-after-restart" {
				remote.history = append(remote.history, &tg.Message{ID: 158, PeerID: &tg.PeerChannel{ChannelID: 321}, FromID: &tg.PeerUser{UserID: 90001}, Date: 1700000000, Message: "unseen-before-maximum"})
				if e := pipeline.Handler.Handle(context.Background(), &tg.Updates{Updates: updatesForMessages(remote.history[:1])}); e != nil {
					t.Fatal(e)
				}
			}
			if mode != "first-hash-only" {
				if e := pipeline.Storage.SetChannelPts(context.Background(), 10001, 321, 0); e != nil {
					t.Fatal(e)
				}
				ts.Close()
				s.Close()
				store = testPG(t)
				adapter = newTestRPCConnection(t, remote, store.DB)
				var e error
				s, e = New(context.Background(), Config{Mode: "fake", Store: store, Telegram: adapter})
				if e != nil {
					t.Fatal(e)
				}
				ts = httptest.NewServer(s)
			}
			defer func() { ts.Close(); s.Close() }()
			script := &subscriptionRemote{remote, make(chan int, 10)}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			pipeline = adapter.Updates("account-1", tg.NewClient(script), cancel)
			manager := updates.New(updates.Config{Handler: pipeline.Handler, Storage: pipeline.Storage, AccessHasher: pipeline.AccessHasher, OnChannelTooLong: pipeline.OnChannelTooLong})
			done := make(chan error, 1)
			go func() { done <- pipeline.Run(ctx, manager, 10001, updates.AuthOptions{}) }()
			select {
			case pts := <-script.requests:
				if pts <= 0 {
					cancel()
					<-done
					t.Fatal("real gotd startup subscribed with hash-only pts=0; inbound remains unprocessed")
				}
			case <-time.After(2 * time.Second):
				cancel()
				<-done
				t.Fatal("real SDK did not subscribe")
			}
			assertMessageReplay(t, ts, "existing-inbound-159")
			if mode == "legacy-max-seen-after-restart" {
				assertMessageReplay(t, ts, "unseen-before-maximum")
			}
			if e := manager.Handle(ctx, &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateNewChannelMessage{Message: &tg.Message{ID: 160, PeerID: &tg.PeerChannel{ChannelID: 321}, FromID: &tg.PeerUser{UserID: 90001}, Date: 1700000001, Message: "next-live-inbound"}, Pts: 81, PtsCount: 1}}}); e != nil {
				t.Fatal(e)
			}
			assertMessageReplay(t, ts, "next-live-inbound")
			cancel()
			<-done
		})
	}
}

func TestRealSDKManagerAcceptsFirstInboundForNewlyRegisteredGroup(t *testing.T) {
	store := testPG(t)
	remote := &remoteTelegram{messages: map[int64]int{}, globalDifference: &tg.UpdatesDifferenceEmpty{Date: 1700000000, Seq: 1}}
	s, adapter, ts := updateTestService(t, store, store.DB, remote)
	defer func() { ts.Close(); s.Close() }()
	s.mu.Lock()
	delete(s.state.Groups, "tg-321")
	if e := s.save(context.Background()); e != nil {
		t.Fatal(e)
	}
	s.mu.Unlock()
	if _, e := store.ExecFenced(context.Background(), "DELETE FROM gateway_channel_state WHERE account_id=$1 AND user_id=$2 AND channel_id=$3", "account-1", 10001, 321); e != nil {
		t.Fatal(e)
	}
	script := &subscriptionRemote{remote, make(chan int, 10)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pipeline := adapter.Updates("account-1", tg.NewClient(script), cancel)
	if e := pipeline.Storage.SetState(ctx, 10001, updates.State{Pts: 7, Date: 1700000000, Seq: 1}); e != nil {
		t.Fatal(e)
	}
	manager := updates.New(updates.Config{Handler: pipeline.Handler, Storage: pipeline.Storage, AccessHasher: pipeline.AccessHasher, OnChannelTooLong: pipeline.OnChannelTooLong})
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- pipeline.Run(ctx, manager, 10001, updates.AuthOptions{OnStart: func(context.Context) { close(started) }})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("manager did not start")
	}
	if status, _ := rpcCall(t, ts, "POST", "/groups", map[string]string{"creatorAccountId": "account-1"}); status != 200 {
		t.Fatal(status)
	}
	channel := &tg.Channel{ID: 321, Title: "Synthetic", Photo: &tg.ChatPhotoEmpty{}, Megagroup: true}
	channel.SetAccessHash(64)
	if e := manager.Handle(ctx, &tg.Updates{Date: 1700000000, Chats: []tg.ChatClass{channel}, Updates: []tg.UpdateClass{&tg.UpdateNewChannelMessage{Message: &tg.Message{ID: 159, PeerID: &tg.PeerChannel{ChannelID: 321}, FromID: &tg.PeerUser{UserID: 90001}, Date: 1700000000, Message: "first-new-group-inbound"}, Pts: 81, PtsCount: 1}}}); e != nil {
		t.Fatal(e)
	}
	assertMessageReplay(t, ts, "first-new-group-inbound")
	cancel()
	<-done
	select {
	case pts := <-script.requests:
		if pts <= 0 {
			t.Fatal("first new-group event initiated zero cursor difference")
		}
	default:
		t.Fatal("new group never subscribed")
	}
}

func TestRealSDKBootstrapFailureCannotReportReadyOrAdvanceCursor(t *testing.T) {
	store := testPG(t)
	remote := &remoteTelegram{messages: map[int64]int{}, onPeerDialogs: func(context.Context) error { return errors.New("metadata denied") }}
	s, adapter, ts := updateTestService(t, store, store.DB, remote)
	defer func() { ts.Close(); s.Close() }()
	if _, e := store.ExecFenced(context.Background(), "DELETE FROM gateway_channel_state WHERE account_id=$1 AND user_id=$2 AND channel_id=$3", "account-1", 10001, 321); e != nil {
		t.Fatal(e)
	}
	pipeline := adapter.Updates("account-1", tg.NewClient(remote), func() {})
	manager := updates.New(updates.Config{Handler: pipeline.Handler, Storage: pipeline.Storage, AccessHasher: pipeline.AccessHasher, OnChannelTooLong: pipeline.OnChannelTooLong})
	ready := false
	if e := pipeline.Run(context.Background(), manager, 10001, updates.AuthOptions{OnStart: func(context.Context) { ready = true }}); e == nil {
		t.Fatal("failed history bootstrap accepted")
	}
	if ready {
		t.Fatal("reported SDK ready before registered history recovered")
	}
	if _, found, e := pipeline.Storage.GetChannelPts(context.Background(), 10001, 321); e != nil || found {
		t.Fatalf("failed bootstrap established cursor: %v %v", found, e)
	}
	_, body := rpcCall(t, ts, "GET", "/groups/tg-321/recovery", nil)
	if rec, ok := body["recovery"].(map[string]any); !ok || rec["status"] != "blocked" {
		t.Fatal(body)
	}
}
