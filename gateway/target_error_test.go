package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"net/http"
	"net/http/httptest"
	"testing"
)

type memberMutationFailure struct {
	*promoteRemote
	failure error
}

func (r *memberMutationFailure) Invoke(ctx context.Context, in bin.Encoder, out bin.Decoder) error {
	switch in.(type) {
	case *tg.ChannelsEditAdminRequest, *tg.ChannelsEditBannedRequest:
		return r.failure
	default:
		return r.promoteRemote.Invoke(ctx, in, out)
	}
}

func TestDeletedTargetNeverTerminatesActingAccountAcrossPostgresRestart(t *testing.T) {
	for _, operation := range []string{"promote", "kick"} {
		for _, tc := range []struct {
			name           string
			failure        error
			terminalStatus string
		}{
			{"typed-target", tgerr.New(400, "INPUT_USER_DEACTIVATED"), ""},
			{"wrapped-target", fmt.Errorf("remote mutation: %w", tgerr.New(400, "INPUT_USER_DEACTIVATED")), ""},
			{"typed-actor", tgerr.New(403, "USER_DEACTIVATED_BAN"), "suspended"},
			{"wrapped-actor", fmt.Errorf("remote mutation: %w", tgerr.New(403, "USER_DEACTIVATED_BAN")), "suspended"},
			{"typed-session", tgerr.New(401, "AUTH_KEY_UNREGISTERED"), "session_expired"},
			{"wrapped-session", fmt.Errorf("remote mutation: %w", tgerr.New(401, "AUTH_KEY_UNREGISTERED")), "session_expired"},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				store := testPG(t)
				remote := &memberMutationFailure{promoteRemote: &promoteRemote{remoteTelegram: &remoteTelegram{}, member: true}, failure: tc.failure}
				adapter := newAcceptanceConnection(t, remote)
				s, err := New(context.Background(), Config{Mode: "fake", Store: store, AdminEnabled: true, Telegram: adapter})
				if err != nil {
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
				if status, _ := rpcCall(t, ts, "POST", "/groups", map[string]string{"creatorAccountId": "account-1"}); status != 200 {
					t.Fatal(status)
				}
				input := map[string]string{"byAccountId": "account-1", "accountId": "account-2"}
				if operation == "kick" {
					input = map[string]string{"byAccountId": "account-1", "targetPlatformUserId": "10002"}
				}
				status, body := rpcCall(t, ts, "POST", "/groups/tg-321/"+operation, input)
				wantStatus, wantCode := 503, "GATEWAY_UNAVAILABLE"
				if tc.terminalStatus == "suspended" {
					wantStatus, wantCode = 403, "ACCOUNT_SUSPENDED"
				}
				if tc.terminalStatus == "session_expired" {
					wantStatus, wantCode = 401, "SESSION_EXPIRED"
				}
				if status != wantStatus || body["code"] != wantCode {
					t.Errorf("mutation status=%d body=%v want=%d/%s", status, body, wantStatus, wantCode)
				}
				for phase := 0; phase < 2; phase++ {
					status, body := rpcCall(t, ts, "POST", "/accounts/account-1/connect", nil)
					wantConnect := 200
					if tc.terminalStatus != "" {
						wantConnect = wantStatus
					}
					if status != wantConnect {
						t.Errorf("phase%d acting account connect=%d body=%v want%d", phase, status, body, wantConnect)
					}
					if tc.terminalStatus != "" && body["code"] != wantCode {
						t.Errorf("phase%d permanent code=%v want%s", phase, body, wantCode)
					}
					if tc.terminalStatus == "" {
						res, err := http.Get(ts.URL + "/groups/tg-321/members")
						if err != nil {
							t.Fatal(err)
						}
						var members []map[string]string
						_ = json.NewDecoder(res.Body).Decode(&members)
						res.Body.Close()
						creatorPresent := false
						for _, member := range members {
							if member["platformUserId"] == "10001" && member["role"] == "creator" {
								creatorPresent = true
							}
						}
						if res.StatusCode != 200 || !creatorPresent {
							t.Errorf("phase%d target error removed acting creator: status%d members=%v", phase, res.StatusCode, members)
						}
					}
					terminal, left := 0, 0
					for _, event := range gatewayEvents(t, ts) {
						if event["type"] == "account_status" && event["accountId"] == "account-1" {
							terminal++
							if event["status"] != tc.terminalStatus {
								t.Errorf("phase%d terminal event=%v want%s", phase, event, tc.terminalStatus)
							}
						}
						if event["type"] == "member_left" && event["groupId"] == "tg-321" && event["platformUserId"] == "10001" {
							left++
						}
					}
					wantEvents := 0
					if tc.terminalStatus != "" {
						wantEvents = 1
					}
					if terminal != wantEvents || left != wantEvents {
						t.Errorf("phase%d acting account terminal/left events=%d/%d want%d/%d", phase, terminal, left, wantEvents, wantEvents)
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
}
