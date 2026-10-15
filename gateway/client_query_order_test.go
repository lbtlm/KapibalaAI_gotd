package gateway

import (
	"context"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

type landedOrderRemote struct {
	*remoteTelegram
	firstID      int64
	secondDate   int
	recoverFirst atomic.Bool
}

func (r *landedOrderRemote) Invoke(ctx context.Context, in bin.Encoder, out bin.Decoder) error {
	request, ok := in.(*tg.MessagesSendMessageRequest)
	if !ok {
		return r.remoteTelegram.Invoke(ctx, in, out)
	}
	r.mu.Lock()
	if r.firstID == 0 {
		r.firstID = request.RandomID
	}
	first := r.firstID == request.RandomID
	r.mu.Unlock()
	if first && !r.recoverFirst.Load() {
		return context.DeadlineExceeded
	}
	id, date := 10, r.secondDate
	if first {
		id, date = 9, 1700000000
	}
	var buffer bin.Buffer
	if err := (&tg.UpdateShortSentMessage{Out: true, ID: id, Date: date, Pts: id, PtsCount: 1}).Encode(&buffer); err != nil {
		return err
	}
	return out.Decode(&buffer)
}

func TestByClientIDChoosesEarliestLandedAfterLateRecoveryAndPostgresRestart(t *testing.T) {
	for _, tc := range []struct {
		name string
		date int
	}{
		{"earlier-platform-date", 1700000001}, {"same-second-numeric-message-order", 1700000000},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			remote := &landedOrderRemote{remoteTelegram: &remoteTelegram{}, secondDate: tc.date}
			adapter := newAcceptanceConnection(t, remote)
			s, err := New(context.Background(), Config{Mode: "fake", Store: store, Telegram: adapter})
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
			input := map[string]string{"accountId": "account-1", "clientMsgId": "shared-client-id", "text": "first landed"}
			if status, _ := rpcCall(t, ts, "POST", "/groups/tg-321/send", input); status != 504 {
				t.Fatal(status)
			}
			input["text"] = "second landed"
			if status, _ := rpcCall(t, ts, "POST", "/groups/tg-321/send", input); status != 202 {
				t.Fatal(status)
			}
			path := "/groups/tg-321/messages/by-client-id/shared-client-id"
			if status, body := rpcCall(t, ts, "GET", path, nil); status != 200 || body["msgId"] != "10" {
				t.Fatal("later message not persisted first", status, body)
			}
			remote.recoverFirst.Store(true)
			if err := adapter.RecoverEffects(context.Background(), "account-1"); err != nil {
				t.Fatal(err)
			}
			for phase := 0; phase < 2; phase++ {
				status, body := rpcCall(t, ts, "GET", path, nil)
				if status != 200 || body["msgId"] != "9" || body["sentAt"] != "2023-11-14T22:13:20Z" {
					t.Errorf("phase=%d earliest landed query status=%d body=%v", phase, status, body)
				}
				messages := map[any]bool{}
				for _, event := range gatewayEvents(t, ts) {
					if event["type"] == "message_sent" && event["clientMsgId"] == "shared-client-id" {
						messages[event["msgId"]] = true
					}
				}
				if len(messages) != 2 {
					t.Errorf("client ID deduplicated distinct platform messages: %v", messages)
				}
				ts.Close()
				s.Close()
				if phase == 0 {
					store = testPG(t)
					adapter = newAcceptanceConnection(t, remote)
					s, err = New(context.Background(), Config{Mode: "fake", Store: store, Telegram: adapter})
					if err != nil {
						t.Fatal(err)
					}
					ts = httptest.NewServer(s)
				}
			}
		})
	}
}
