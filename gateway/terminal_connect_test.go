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

type terminalConnectRemote struct {
	*remoteTelegram
	code  string
	calls atomic.Int64
}

func (r *terminalConnectRemote) Invoke(ctx context.Context, in bin.Encoder, out bin.Decoder) error {
	r.calls.Add(1)
	if _, ok := in.(*tg.UsersGetUsersRequest); ok {
		return errors.New(r.code)
	}
	return r.remoteTelegram.Invoke(ctx, in, out)
}

type terminalConnectAdapter struct{ *acceptanceConnection }

func (a *terminalConnectAdapter) Connect(ctx context.Context, id string) (string, error) {
	if id != "account-2" {
		return a.acceptanceConnection.Connect(ctx, id)
	}
	_, err := tg.NewClient(a.remote).UsersGetUsers(ctx, []tg.InputUserClass{&tg.InputUserSelf{}})
	return "", platformError(err)
}

func TestFirstTelegramConnectPermanentFailureSurvivesReconstruction(t *testing.T) {
	for _, tc := range []struct {
		remote, code string
		status       int
	}{
		{"AUTH_KEY_UNREGISTERED", "SESSION_EXPIRED", 401},
		{"USER_DEACTIVATED_BAN", "ACCOUNT_SUSPENDED", 403},
	} {
		t.Run(tc.code, func(t *testing.T) {
			store := testPG(t)
			// Reset through the public simulator before using the isolated PG snapshot.
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
			remote := &terminalConnectRemote{remoteTelegram: &remoteTelegram{}, code: tc.remote}
			adapter := &terminalConnectAdapter{newAcceptanceConnection(t, remote)}
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
			status, body := rpcCall(t, ts, "POST", "/accounts/account-2/connect", nil)
			if status != tc.status || body["code"] != tc.code {
				t.Fatal(status, body)
			}
			before := remote.calls.Load()
			requests := []struct {
				path string
				body any
			}{
				{"/accounts/account-2/connect", nil}, {"/accounts/account-2/disconnect", nil},
				{"/groups", map[string]string{"creatorAccountId": "account-2"}},
				{"/groups/tg-321/send", map[string]string{"accountId": "account-2", "clientMsgId": "blocked", "text": "synthetic"}},
				{"/groups/tg-321/join", map[string]string{"accountId": "account-2", "inviteLink": "https://t.me/+synthetic"}},
				{"/groups/tg-321/promote", map[string]string{"byAccountId": "account-2", "accountId": "account-1"}},
				{"/groups/tg-321/kick", map[string]string{"byAccountId": "account-2", "targetPlatformUserId": "10001"}},
				{"/groups/tg-321/leave", map[string]string{"accountId": "account-2"}},
				{"/groups/import", map[string]any{"username": "synthetic", "accountIds": []string{"account-2"}}},
			}
			for phase := 0; phase < 2; phase++ {
				for _, request := range requests {
					status, body := rpcCall(t, ts, "POST", request.path, request.body)
					if status != tc.status || body["code"] != tc.code {
						t.Errorf("phase=%d %s: status=%d body=%v", phase, request.path, status, body)
					}
				}
				if remote.calls.Load() != before {
					t.Errorf("terminal requests reached Telegram: before=%d after=%d", before, remote.calls.Load())
				}
				ts.Close()
				s.Close()
				if phase == 0 {
					store = testPG(t)
					adapter = &terminalConnectAdapter{newAcceptanceConnection(t, remote)}
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
