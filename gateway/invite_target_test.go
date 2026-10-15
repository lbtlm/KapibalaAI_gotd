package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type inviteRemote struct {
	*remoteTelegram
	checkTarget  int64
	resultTarget int64
	imports      int
	joined       bool
}

func (r *inviteRemote) Invoke(ctx context.Context, in bin.Encoder, out bin.Decoder) error {
	r.mu.Lock()
	var response bin.Encoder
	switch in.(type) {
	case *tg.MessagesCheckChatInviteRequest:
		if r.checkTarget == 0 {
			response = &tg.ChatInvite{Title: "Same title is not identity", Photo: &tg.PhotoEmpty{ID: 1}}
		} else {
			response = &tg.ChatInviteAlready{Chat: &tg.Channel{ID: r.checkTarget, AccessHash: 64, Title: "Synthetic", Photo: &tg.ChatPhotoEmpty{}}}
		}
	case *tg.MessagesImportChatInviteRequest:
		r.imports++
		r.joined = r.resultTarget == 321
		response = &tg.Updates{Chats: []tg.ChatClass{&tg.Channel{ID: r.resultTarget, AccessHash: 64, Title: "Synthetic", Photo: &tg.ChatPhotoEmpty{}}}}
	case *tg.ChannelsGetParticipantsRequest:
		participants := []tg.ChannelParticipantClass{&tg.ChannelParticipantCreator{UserID: 10001, AdminRights: tg.ChatAdminRights{}}}
		if r.joined {
			participants = append(participants, &tg.ChannelParticipant{UserID: 10002})
		}
		response = &tg.ChannelsChannelParticipants{Count: len(participants), Participants: participants}
	default:
		r.mu.Unlock()
		return r.remoteTelegram.Invoke(ctx, in, out)
	}
	r.mu.Unlock()
	var buffer bin.Buffer
	if err := response.Encode(&buffer); err != nil {
		return err
	}
	return out.Decode(&buffer)
}

// Only authentication is synthetic; operations use the production adapter and TL transport.
type acceptanceConnection struct {
	*TelegramAdapter
	remote tg.Invoker
}

func (a *acceptanceConnection) Connect(ctx context.Context, id string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	pid := int64(10001)
	if id == "account-2" {
		pid = 10002
	}
	if a.sessions[id] == nil {
		done := make(chan struct{})
		var once sync.Once
		a.sessions[id] = &onlineSession{api: a.fencedClient(tg.NewClient(a.remote)), self: &tg.User{ID: pid}, done: done, cancel: func() { once.Do(func() { close(done) }) }}
	}
	return stringID(pid), nil
}
func (a *acceptanceConnection) Resume(s *Service) {
	for id, account := range s.state.Accounts {
		if account.Status == "online" {
			_, _ = a.Connect(s.ctx, id)
		}
	}
}
func newAcceptanceConnection(t *testing.T, remote tg.Invoker) *acceptanceConnection {
	t.Helper()
	a, err := NewTelegramAdapter(context.Background(), nil, []AccountConfig{
		{ID: "account-1", APIID: 1, APIHash: "synthetic", SessionPath: "not-opened"},
		{ID: "account-2", APIID: 1, APIHash: "synthetic", SessionPath: "not-opened"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &acceptanceConnection{a, remote}
}

func gatewayEvents(t *testing.T, ts *httptest.Server) []map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/events?since=0", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var events []map[string]any
	scan := bufio.NewScanner(res.Body)
	for scan.Scan() {
		line := scan.Text()
		if len(line) > 6 && line[:6] == "data: " {
			var event map[string]any
			if err := json.Unmarshal([]byte(line[6:]), &event); err != nil {
				t.Fatal(err)
			}
			events = append(events, event)
		}
	}
	return events
}

func TestTelegramJoinVerifiesInviteTargetBeforeImport(t *testing.T) {
	for _, tc := range []struct {
		name            string
		check, result   int64
		status, imports int
		ownerOffline    bool
	}{
		{"other-group", 999, 999, 400, 0, false},
		{"unprovable-target", 0, 321, 503, 0, false},
		{"no-online-member", 321, 321, 503, 0, true},
		{"wrong-import-result", 321, 999, 503, 1, false},
		{"matching-group", 321, 321, 202, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			remote := &inviteRemote{remoteTelegram: &remoteTelegram{}, checkTarget: tc.check, resultTarget: tc.result}
			adapter := newAcceptanceConnection(t, remote)
			s, err := New(context.Background(), Config{Mode: "telegram", Store: NewMemoryStore(), Telegram: adapter})
			if err != nil {
				t.Fatal(err)
			}
			ts := httptest.NewServer(s)
			defer func() { ts.Close(); s.Close() }()
			for _, id := range []string{"account-1", "account-2"} {
				if status, _ := rpcCall(t, ts, "POST", "/accounts/"+id+"/connect", nil); status != 200 {
					t.Fatal(status)
				}
			}
			if status, _ := rpcCall(t, ts, "POST", "/groups", map[string]string{"creatorAccountId": "account-1"}); status != 200 {
				t.Fatal(status)
			}
			if tc.ownerOffline {
				if status, _ := rpcCall(t, ts, "POST", "/accounts/account-1/disconnect", nil); status != 200 {
					t.Fatal(status)
				}
			}
			input := map[string]string{"accountId": "account-2", "inviteLink": "https://t.me/+synthetic"}
			status, body := rpcCall(t, ts, "POST", "/groups/tg-321/join", input)
			if status != tc.status {
				t.Fatalf("join status=%d body=%v want=%d", status, body, tc.status)
			}
			if tc.name == "other-group" && body["code"] != "INVITE_GROUP_MISMATCH" {
				t.Fatal(body)
			}
			if tc.name == "matching-group" {
				status, body = rpcCall(t, ts, "POST", "/groups/tg-321/join", input)
				if status != 409 || body["code"] != "ALREADY_MEMBER" {
					t.Fatal("duplicate join", status, body)
				}
			}
			remote.mu.Lock()
			imports := remote.imports
			remote.mu.Unlock()
			if imports != tc.imports {
				t.Fatalf("external imports=%d want=%d", imports, tc.imports)
			}
			joined := 0
			for _, event := range gatewayEvents(t, ts) {
				if event["type"] == "member_joined" && event["groupId"] == "tg-321" && event["platformUserId"] == "10002" {
					joined++
				}
			}
			want := 0
			if tc.name == "matching-group" {
				want = 1
			}
			if joined != want {
				t.Fatalf("joined events=%d want=%d", joined, want)
			}
		})
	}
}
