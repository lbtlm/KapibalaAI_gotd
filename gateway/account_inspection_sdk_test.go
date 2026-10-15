package gateway

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
)

type inspectionRemote struct {
	mu          sync.Mutex
	dialogCalls int
	failSecond  bool
	onPage      func(*tg.MessagesGetDialogsRequest) error
	onSelf      func(context.Context) error
}

func (r *inspectionRemote) Invoke(ctx context.Context, in bin.Encoder, out bin.Decoder) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var reply bin.Encoder
	switch req := in.(type) {
	case *tg.UsersGetUsersRequest:
		if r.onSelf != nil {
			if err := r.onSelf(ctx); err != nil {
				return err
			}
		}
		reply = &tg.UserClassVector{Elems: []tg.UserClass{&tg.User{ID: 10001, Phone: "15551234567", FirstName: "Alice", LastName: "Example", Username: "alice", Premium: true}}}
	case *tg.MessagesGetDialogsRequest:
		r.dialogCalls++
		if r.onPage != nil {
			if err := r.onPage(req); err != nil {
				return err
			}
		}
		if req.OffsetID == 0 {
			reply = &tg.MessagesDialogsSlice{Count: 4, Dialogs: []tg.DialogClass{&tg.Dialog{Peer: &tg.PeerChannel{ChannelID: 11}, TopMessage: 101}, &tg.Dialog{Peer: &tg.PeerChat{ChatID: 12}, TopMessage: 102}}, Messages: []tg.MessageClass{&tg.Message{ID: 101, PeerID: &tg.PeerChannel{ChannelID: 11}, Date: 1700000000}, &tg.Message{ID: 102, PeerID: &tg.PeerChat{ChatID: 12}, Date: 1700000001}}, Chats: []tg.ChatClass{&tg.Channel{ID: 11, AccessHash: 111, Title: "Owned", Photo: &tg.ChatPhotoEmpty{}, Megagroup: true, Creator: true}, &tg.Chat{ID: 12, Title: "Admin", Photo: &tg.ChatPhotoEmpty{}, AdminRights: tg.ChatAdminRights{BanUsers: true}}}}
		} else {
			if req.OffsetID != 102 || req.OffsetDate != 1700000001 {
				return errors.New("incorrect pagination offset")
			}
			if r.failSecond {
				return errors.New("network page failed")
			}
			reply = &tg.MessagesDialogs{Dialogs: []tg.DialogClass{&tg.Dialog{Peer: &tg.PeerChannel{ChannelID: 13}, TopMessage: 103}, &tg.Dialog{Peer: &tg.PeerChannel{ChannelID: 14}, TopMessage: 104}, &tg.Dialog{Peer: &tg.PeerChannel{ChannelID: 15}, TopMessage: 105}}, Chats: []tg.ChatClass{&tg.Channel{ID: 13, AccessHash: 113, Title: "Member", Photo: &tg.ChatPhotoEmpty{}, Megagroup: true}, &tg.Channel{ID: 14, AccessHash: 114, Title: "Broadcast", Photo: &tg.ChatPhotoEmpty{}, Broadcast: true}, &tg.Channel{ID: 15, AccessHash: 115, Title: "Limited", Photo: &tg.ChatPhotoEmpty{}, Megagroup: true, Min: true}}}
		}
	default:
		return errors.New("unexpected Telegram request")
	}
	var b bin.Buffer
	if err := reply.Encode(&b); err != nil {
		return err
	}
	return out.Decode(&b)
}

type inspectionConnection struct {
	*TelegramAdapter
	remote tg.Invoker
}

func (a *inspectionConnection) Connect(ctx context.Context, id string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sessions[id] == nil {
		done := make(chan struct{})
		var once sync.Once
		cid, _ := ctx.Value(connectionIDKey{}).(string)
		a.sessions[id] = &onlineSession{connectionID: cid, self: &tg.User{ID: 10001}, api: tg.NewClient(a.remote), done: done, cancel: func() { once.Do(func() { close(done) }) }}
	}
	return "10001", nil
}
func TestAccountInspectionUsesManagedClientAndAllDialogPages(t *testing.T) {
	remote := &inspectionRemote{}
	base, err := NewTelegramAdapter(context.Background(), nil, []AccountConfig{{ID: "account-1", APIID: 1, APIHash: "synthetic", SessionPath: "not-opened"}})
	if err != nil {
		t.Fatal(err)
	}
	adapter := &inspectionConnection{base, remote}
	s, err := New(context.Background(), Config{Mode: "fake", Store: NewMemoryStore(), Telegram: adapter})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s)
	defer func() { ts.Close(); s.Close() }()
	if status, _ := rpcCall(t, ts, "POST", "/accounts/account-1/check", nil); status != 409 {
		t.Fatal("offline check implicitly connected", status)
	}
	if status, _ := rpcCall(t, ts, "POST", "/accounts/account-1/connect", nil); status != 200 {
		t.Fatal(status)
	}
	status, profile := rpcCall(t, ts, "POST", "/accounts/account-1/check", nil)
	if status != 200 || profile["source"] != "telegram" || profile["phone"] != "15551234567" || profile["premium"] != true || profile["username"] != "alice" {
		t.Fatalf("profile: %d %v", status, profile)
	}
	status, body := rpcCall(t, ts, "GET", "/accounts/account-1/joined-groups", nil)
	if status != 200 {
		t.Fatalf("groups: %d %v", status, body)
	}
	items := body["items"].([]any)
	if len(items) != 4 {
		t.Fatalf("did not finish all pages or included broadcast: %v", items)
	}
	roles := map[string]string{}
	for _, item := range items {
		g := item.(map[string]any)
		roles[g["title"].(string)] = g["role"].(string)
	}
	if roles["Owned"] != "creator" || roles["Admin"] != "admin" || roles["Member"] != "member" || roles["Limited"] != "unknown" {
		t.Fatal(roles)
	}
	remote.mu.Lock()
	remote.failSecond = true
	remote.mu.Unlock()
	if status, body := rpcCall(t, ts, "GET", "/accounts/account-1/joined-groups", nil); status != 503 || body["items"] != nil {
		t.Fatalf("partial success returned: %d %v", status, body)
	}
}
