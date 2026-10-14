package gateway

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Scripted Invoker models the remote Telegram random-ID contract, not collaborators.
type remoteTelegram struct {
	mu               sync.Mutex
	messages         map[int64]int
	calls            []int64
	onAccept         func()
	history          []tg.MessageClass
	difference       tg.UpdatesChannelDifferenceClass
	historyRequests  [][]int
	omitHistoryID    int
	globalDifference tg.UpdatesDifferenceClass
	peerTop          int
	targetPresent    bool
	kickCalls        int
	onKickAccept     func()
	onPeerDialogs    func(context.Context) error
	onParticipants   func(context.Context) error
	sendErrors       []error
}

func (r *remoteTelegram) Invoke(ctx context.Context, in bin.Encoder, out bin.Decoder) error {
	var response bin.Encoder
	switch req := in.(type) {
	case *tg.UpdatesGetChannelDifferenceRequest:
		response = r.difference
	case *tg.UpdatesGetDifferenceRequest:
		response = r.globalDifference
	case *tg.MessagesGetPeerDialogsRequest:
		r.mu.Lock()
		hook := r.onPeerDialogs
		r.mu.Unlock()
		if hook != nil {
			if e := hook(ctx); e != nil {
				return e
			}
		}
		response = &tg.MessagesPeerDialogs{Dialogs: []tg.DialogClass{&tg.Dialog{Peer: &tg.PeerChannel{ChannelID: 321}, TopMessage: r.peerTop, Pts: 80, NotifySettings: tg.PeerNotifySettings{}}}}
	case *tg.ChannelsGetMessagesRequest:
		ids := make([]int, 0, len(req.ID))
		messages := make([]tg.MessageClass, 0, len(req.ID))
		for _, item := range req.ID {
			mid := item.(*tg.InputMessageID).ID
			ids = append(ids, mid)
			if mid == r.omitHistoryID {
				continue
			}
			var found tg.MessageClass
			for _, message := range r.history {
				if message.GetID() == mid {
					found = message
				}
			}
			if found == nil {
				found = &tg.MessageEmpty{ID: mid}
			}
			messages = append(messages, found)
		}
		r.mu.Lock()
		r.historyRequests = append(r.historyRequests, ids)
		r.mu.Unlock()
		response = &tg.MessagesMessages{Messages: messages}
	case *tg.ChannelsGetParticipantsRequest:
		r.mu.Lock()
		hook := r.onParticipants
		r.mu.Unlock()
		if hook != nil {
			if e := hook(ctx); e != nil {
				return e
			}
		}
		participants := []tg.ChannelParticipantClass{&tg.ChannelParticipantCreator{UserID: 10001, AdminRights: tg.ChatAdminRights{BanUsers: true}}}
		users := []tg.UserClass{&tg.User{ID: 10001, FirstName: "Synthetic"}}
		r.mu.Lock()
		present := r.targetPresent
		r.mu.Unlock()
		if present {
			participants = append(participants, &tg.ChannelParticipant{UserID: 90001})
			users = append(users, &tg.User{ID: 90001, AccessHash: 55, FirstName: "Target"})
		}
		response = &tg.ChannelsChannelParticipants{Count: len(participants), Participants: participants, Users: users}
	case *tg.ChannelsEditBannedRequest:
		r.mu.Lock()
		r.kickCalls++
		r.targetPresent = false
		hook := r.onKickAccept
		r.onKickAccept = nil
		r.mu.Unlock()
		if hook != nil {
			hook()
		}
		response = &tg.Updates{}
	case *tg.ChannelsCreateChannelRequest:
		response = &tg.Updates{Chats: []tg.ChatClass{&tg.Channel{ID: 321, AccessHash: 64, Title: req.Title, Photo: &tg.ChatPhotoEmpty{}, Megagroup: true, Creator: true}}}
	case *tg.MessagesSendMessageRequest:
		r.mu.Lock()
		r.calls = append(r.calls, req.RandomID)
		if len(r.sendErrors) > 0 {
			e := r.sendErrors[0]
			r.sendErrors = r.sendErrors[1:]
			r.mu.Unlock()
			return e
		}
		mid, ok := r.messages[req.RandomID]
		if !ok {
			mid = len(r.messages) + 1
			r.messages[req.RandomID] = mid
		}
		hook := r.onAccept
		r.onAccept = nil
		r.mu.Unlock()
		if hook != nil {
			hook()
		}
		response = &tg.UpdateShortSentMessage{Out: true, ID: mid, Date: 1700000000, Pts: mid, PtsCount: 1}
	default:
		return errors.New("unscripted external Telegram method")
	}
	var buffer bin.Buffer
	if e := response.Encode(&buffer); e != nil {
		return e
	}
	return out.Decode(&buffer)
}

type testRPCConnection struct {
	*TelegramAdapter
	remote *remoteTelegram
}

func (c *testRPCConnection) Connect(ctx context.Context, id string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessions[id] == nil {
		done := make(chan struct{})
		var once sync.Once
		c.sessions[id] = &onlineSession{api: tg.NewClient(c.remote), self: &tg.User{ID: 10001}, done: done, cancel: func() { once.Do(func() { close(done) }) }}
	}
	return "10001", nil
}
func (c *testRPCConnection) Resume(s *Service) {
	for id, acct := range s.state.Accounts {
		if acct.Status == "online" {
			s.wg.Add(1)
			go func(id string) {
				defer s.wg.Done()
				_, _ = c.Connect(s.ctx, id)
				if recoverer, ok := any(c.TelegramAdapter).(interface {
					RecoverEffects(context.Context, string) error
				}); ok {
					_ = recoverer.RecoverEffects(s.ctx, id)
				}
			}(id)
		}
	}
}
func newTestRPCConnection(t *testing.T, r *remoteTelegram, databases ...*sql.DB) *testRPCConnection {
	t.Helper()
	var db *sql.DB
	if len(databases) > 0 {
		db = databases[0]
	}
	a, e := NewTelegramAdapter(context.Background(), db, []AccountConfig{{ID: "account-1", APIID: 1, APIHash: "synthetic", SessionPath: "not-opened", SessionFormat: "gotdJSON"}})
	if e != nil {
		t.Fatal(e)
	}
	return &testRPCConnection{a, r}
}

type interruptedStore struct {
	*MemoryStore
	failBeforeRPC atomic.Bool
	failResult    atomic.Bool
}

func (d *interruptedStore) Save(ctx context.Context, b []byte) error {
	if d.failBeforeRPC.CompareAndSwap(true, false) {
		if e := d.MemoryStore.Save(ctx, b); e != nil {
			return e
		}
		return errors.New("lost intent-commit acknowledgement")
	}
	if d.failResult.CompareAndSwap(true, false) {
		return errors.New("lost result commit before persistence")
	}
	return d.MemoryStore.Save(ctx, b)
}
func rpcCall(t *testing.T, ts *httptest.Server, method, path string, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, ts.URL+path, bytes.NewReader(b))
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	var data map[string]any
	_ = json.NewDecoder(res.Body).Decode(&data)
	return res.StatusCode, data
}
func TestTelegramSendRecoveryRetainsOneRemoteRandomID(t *testing.T) {
	for _, point := range []string{"before-rpc", "accepted-before-result-persist"} {
		t.Run(point, func(t *testing.T) {
			remote := &remoteTelegram{messages: map[int64]int{}}
			store := &interruptedStore{MemoryStore: NewMemoryStore()}
			adapter := newTestRPCConnection(t, remote)
			s, e := New(context.Background(), Config{Mode: "telegram", Store: store, Telegram: adapter})
			if e != nil {
				t.Fatal(e)
			}
			ts := httptest.NewServer(s)
			status, _ := rpcCall(t, ts, "POST", "/accounts/account-1/connect", nil)
			if status != 200 {
				t.Fatal(status)
			}
			status, g := rpcCall(t, ts, "POST", "/groups", map[string]string{"creatorAccountId": "account-1"})
			if status != 200 {
				t.Fatal(status, g)
			}
			gid := g["groupId"].(string)
			if point == "before-rpc" {
				store.failBeforeRPC.Store(true)
			} else {
				remote.mu.Lock()
				remote.onAccept = func() { store.failResult.Store(true) }
				remote.mu.Unlock()
			}
			status, _ = rpcCall(t, ts, "POST", "/groups/"+gid+"/send", map[string]string{"accountId": "account-1", "clientMsgId": "durable-real", "text": "one logical send"})
			if status != 503 {
				t.Fatalf("crash exposure got %d", status)
			}
			remote.mu.Lock()
			before := len(remote.messages)
			remote.mu.Unlock()
			if point == "before-rpc" && before != 0 {
				t.Fatal("platform RPC issued before durable-intent acknowledgement")
			}
			ts.Close()
			s.Close()
			adapter = newTestRPCConnection(t, remote)
			s, e = New(context.Background(), Config{Mode: "telegram", Store: store, Telegram: adapter})
			if e != nil {
				t.Fatal(e)
			}
			ts = httptest.NewServer(s)
			defer func() { ts.Close(); s.Close() }()
			deadline := time.Now().Add(2 * time.Second)
			var result map[string]any
			for {
				status, result = rpcCall(t, ts, "GET", "/groups/"+gid+"/messages/by-client-id/durable-real", nil)
				if status == 200 || time.Now().After(deadline) {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if status != 200 || result["msgId"] != "1" {
				t.Fatalf("real intent did not resolve: %d %v", status, result)
			}
			remote.mu.Lock()
			defer remote.mu.Unlock()
			if len(remote.messages) != 1 {
				t.Fatalf("remote messages=%d", len(remote.messages))
			}
			for _, rid := range remote.calls {
				if rid != remote.calls[0] {
					t.Fatal("recovery issued a new random ID")
				}
			}
		})
	}
}
func TestSendRecoveryWaitsForFloodDeadlineWithoutResettingIt(t *testing.T) {
	remote := &remoteTelegram{messages: map[int64]int{}, sendErrors: []error{&APIError{429, "RATE_LIMITED", 30}}}
	store := &interruptedStore{MemoryStore: NewMemoryStore()}
	adapter := newTestRPCConnection(t, remote)
	s, e := New(context.Background(), Config{Mode: "telegram", Store: store, Telegram: adapter})
	if e != nil {
		t.Fatal(e)
	}
	ts := httptest.NewServer(s)
	defer func() { ts.Close(); s.Close() }()
	rpcCall(t, ts, "POST", "/accounts/account-1/connect", nil)
	_, g := rpcCall(t, ts, "POST", "/groups", map[string]string{"creatorAccountId": "account-1"})
	store.failBeforeRPC.Store(true)
	rpcCall(t, ts, "POST", "/groups/"+g["groupId"].(string)+"/send", map[string]string{"accountId": "account-1", "clientMsgId": "flood-recovery", "text": "retained intent"})
	if e = adapter.RecoverEffects(context.Background(), "account-1"); platformError(e).Code != "RATE_LIMITED" {
		t.Fatal(e)
	}
	s.mu.Lock()
	deadline := s.state.Accounts["account-1"].RateUntil
	s.mu.Unlock()
	if e = adapter.RecoverEffects(context.Background(), "account-1"); e == nil {
		t.Fatal("recovery retried during flood wait")
	}
	s.mu.Lock()
	unchanged := s.state.Accounts["account-1"].RateUntil.Equal(deadline)
	s.mu.Unlock()
	remote.mu.Lock()
	calls := len(remote.calls)
	remote.mu.Unlock()
	if calls != 1 || !unchanged {
		t.Fatalf("background recovery reset flood wait or invoked remote again: calls=%d unchanged=%v", calls, unchanged)
	}
}
