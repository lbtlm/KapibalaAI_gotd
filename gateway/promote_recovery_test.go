package gateway

import (
	"context"
	"errors"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

type promoteRemote struct {
	*remoteTelegram
	member            bool
	admin             bool
	promotes          int
	onPromote         func()
	participantsError error
}

func (r *promoteRemote) Invoke(ctx context.Context, in bin.Encoder, out bin.Decoder) error {
	r.mu.Lock()
	var response bin.Encoder
	switch in.(type) {
	case *tg.ChannelsGetParticipantsRequest:
		if r.participantsError != nil {
			err := r.participantsError
			r.mu.Unlock()
			return err
		}
		participants := []tg.ChannelParticipantClass{&tg.ChannelParticipantCreator{UserID: 10001, AdminRights: tg.ChatAdminRights{}}}
		if r.member {
			if r.admin {
				participants = append(participants, &tg.ChannelParticipantAdmin{UserID: 10002, AdminRights: tg.ChatAdminRights{}})
			} else {
				participants = append(participants, &tg.ChannelParticipant{UserID: 10002})
			}
		}
		response = &tg.ChannelsChannelParticipants{Count: len(participants), Participants: participants, Users: []tg.UserClass{&tg.User{ID: 10002, AccessHash: 55, FirstName: "Synthetic target"}}}
	case *tg.ChannelsEditAdminRequest:
		r.promotes++
		r.admin = true
		if r.onPromote != nil {
			r.onPromote()
		}
		response = &tg.Updates{}
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

type lostMemberIntentAck struct {
	*PostgresStore
	lose  atomic.Bool
	after atomic.Int64
}

func (s *lostMemberIntentAck) Save(ctx context.Context, data []byte) error {
	if err := s.PostgresStore.Save(ctx, data); err != nil {
		return err
	}
	if s.lose.CompareAndSwap(true, false) {
		return errors.New("lost prepared-commit acknowledgement")
	}
	if s.after.Load() > 0 && s.after.Add(-1) == 0 {
		return errors.New("lost unknown-commit acknowledgement before dispatch")
	}
	return nil
}

func TestPromoteKeySurvivesPreparedAndAcceptedResultWindows(t *testing.T) {
	for _, window := range []string{"prepared-before-dispatch", "accepted-before-result-save", "unknown-without-confirmed-role", "unknown-before-dispatch", "unknown-without-snapshot-access"} {
		t.Run(window, func(t *testing.T) {
			store := testPG(t)
			reset, err := New(context.Background(), Config{Mode: "fake", Store: store, AdminEnabled: true})
			if err != nil {
				t.Fatal(err)
			}
			ts := httptest.NewServer(reset)
			if status, _ := rpcCall(t, ts, "POST", "/admin/reset", nil); status != 200 {
				t.Fatal(status)
			}
			ts.Close()
			reset.Close()
			store = testPG(t)
			remote := &promoteRemote{remoteTelegram: &remoteTelegram{}, member: true}
			ack := &lostMemberIntentAck{PostgresStore: store}
			failure := &updateFailStore{PostgresStore: store}
			var persistence Store = failure
			if window == "prepared-before-dispatch" || window == "unknown-before-dispatch" {
				persistence = ack
			}
			adapter := newAcceptanceConnection(t, remote)
			s, err := New(context.Background(), Config{Mode: "fake", Store: persistence, Telegram: adapter})
			if err != nil {
				t.Fatal(err)
			}
			ts = httptest.NewServer(s)
			if status, _ := rpcCall(t, ts, "POST", "/accounts/account-1/connect", nil); status != 200 {
				t.Fatal(status)
			}
			if status, _ := rpcCall(t, ts, "POST", "/groups", map[string]string{"creatorAccountId": "account-1"}); status != 200 {
				t.Fatal(status)
			}
			input := map[string]string{"byAccountId": "account-1", "accountId": "account-2", "idempotencyKey": "job:promote:2"}
			if window == "prepared-before-dispatch" {
				ack.lose.Store(true)
			} else if window == "unknown-before-dispatch" {
				ack.after.Store(2)
			} else {
				remote.onPromote = func() { failure.fail.Store(true) }
			}
			status, _ := rpcCall(t, ts, "POST", "/groups/tg-321/promote", input)
			if status != 503 {
				t.Errorf("injected persistence failure status=%d want503", status)
			}
			remote.mu.Lock()
			calls := remote.promotes
			remote.onPromote = nil
			if window == "unknown-without-confirmed-role" {
				remote.admin = false
			}
			if window == "unknown-without-snapshot-access" {
				remote.participantsError = errors.New("CHAT_ADMIN_REQUIRED")
			}
			remote.mu.Unlock()
			if (window == "prepared-before-dispatch" || window == "unknown-before-dispatch") && calls != 0 {
				t.Errorf("prepared request dispatched %d platform effects", calls)
			}
			ts.Close()
			s.Close()
			store = testPG(t)
			adapter = newAcceptanceConnection(t, remote)
			s, err = New(context.Background(), Config{Mode: "fake", Store: store, Telegram: adapter})
			if err != nil {
				t.Fatal(err)
			}
			ts = httptest.NewServer(s)
			defer func() { ts.Close(); s.Close() }()
			status, body := rpcCall(t, ts, "POST", "/groups/tg-321/promote", input)
			if window == "unknown-without-confirmed-role" || window == "unknown-before-dispatch" || window == "unknown-without-snapshot-access" {
				if status != 504 || body["code"] != "NETWORK_TIMEOUT" {
					t.Errorf("unproven outcome status=%d body=%v", status, body)
				}
			} else if status != 200 {
				t.Errorf("same-key recovery status=%d body=%v", status, body)
			}
			if status2, _ := rpcCall(t, ts, "POST", "/groups/tg-321/promote", input); status2 != status {
				t.Errorf("replay changed result: %d ->%d", status, status2)
			}
			remote.mu.Lock()
			calls = remote.promotes
			remote.mu.Unlock()
			wantCalls := 1
			if window == "unknown-before-dispatch" {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Errorf("same key executed platform promotion %d times", calls)
			}
			for _, changed := range []map[string]string{
				{"byAccountId": "account-2", "accountId": "account-2", "idempotencyKey": "job:promote:2"},
				{"byAccountId": "account-1", "accountId": "account-1", "idempotencyKey": "job:promote:2"},
			} {
				if code, body := rpcCall(t, ts, "POST", "/groups/tg-321/promote", changed); code != 409 || body["code"] != "IDEMPOTENCY_CONFLICT" {
					t.Errorf("changed identity accepted: %d %v", code, body)
				}
			}
			if code, body := rpcCall(t, ts, "POST", "/groups/tg-999/promote", input); code != 409 || body["code"] != "IDEMPOTENCY_CONFLICT" {
				t.Errorf("changed group accepted: %d %v", code, body)
			}
		})
	}
}

func TestPromoteKeyReplaysDefinitiveFailureAndAllowsNextAttempt(t *testing.T) {
	remote := &promoteRemote{remoteTelegram: &remoteTelegram{}}
	adapter := newAcceptanceConnection(t, remote)
	s, err := New(context.Background(), Config{Mode: "telegram", Store: NewMemoryStore(), Telegram: adapter})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s)
	defer func() { ts.Close(); s.Close() }()
	if status, _ := rpcCall(t, ts, "POST", "/accounts/account-1/connect", nil); status != 200 {
		t.Fatal(status)
	}
	if status, _ := rpcCall(t, ts, "POST", "/groups", map[string]string{"creatorAccountId": "account-1"}); status != 200 {
		t.Fatal(status)
	}
	input := map[string]string{"byAccountId": "account-1", "accountId": "account-2", "idempotencyKey": "job:promote:1"}
	if status, body := rpcCall(t, ts, "POST", "/groups/tg-321/promote", input); status != 409 || body["code"] != "NOT_MEMBER_YET" {
		t.Fatal(status, body)
	}
	remote.mu.Lock()
	remote.member = true
	remote.mu.Unlock()
	if status, body := rpcCall(t, ts, "POST", "/groups/tg-321/promote", input); status != 409 || body["code"] != "NOT_MEMBER_YET" {
		t.Fatal("same key lost definitive failure", status, body)
	}
	input["idempotencyKey"] = "job:promote:2"
	if status, body := rpcCall(t, ts, "POST", "/groups/tg-321/promote", input); status != 200 {
		t.Fatal("new attempt blocked", status, body)
	}
	delete(input, "idempotencyKey")
	if status, body := rpcCall(t, ts, "POST", "/groups/tg-321/promote", input); status != 200 {
		t.Fatal("legacy call blocked", status, body)
	}
	remote.mu.Lock()
	defer remote.mu.Unlock()
	if remote.promotes != 2 {
		t.Fatalf("expected one keyed and one legacy RPC, got%d", remote.promotes)
	}
}
