package gateway_test

import (
	"context"
	"encoding/json"
	gateway "github.com/lbtlm/KapibalaAI_gotd/gateway"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFakeAdminIsExplicitlyEnabled(t *testing.T) {
	s, e := gateway.New(context.Background(), gateway.Config{Mode: "fake", Store: gateway.NewMemoryStore()})
	if e != nil {
		t.Fatal(e)
	}
	ts := httptest.NewServer(s)
	defer func() { ts.Close(); s.Close() }()
	call(t, ts, "POST", "/admin/reset", nil, 404)
}
func TestUnavailableIncludesQueriesAndEvents(t *testing.T) {
	ts := testServer(t)
	call(t, ts, "POST", "/accounts/account-1/connect", nil, 200)
	gid := call(t, ts, "POST", "/groups", map[string]string{"creatorAccountId": "account-1"}, 200)["groupId"].(string)
	call(t, ts, "POST", "/admin/faults", map[string]any{"operation": "any", "code": "GATEWAY_UNAVAILABLE", "count": -1}, 200)
	for _, p := range []string{"/groups/" + gid + "/members", "/groups/" + gid + "/messages/by-client-id/no-message", "/events?since=0"} {
		body := call(t, ts, "GET", p, nil, 503)
		if body["code"] != "GATEWAY_UNAVAILABLE" {
			t.Fatal(body)
		}
	}
	call(t, ts, "POST", "/accounts/account-1/disconnect", nil, 503)
	call(t, ts, "POST", "/groups/"+gid+"/send", map[string]string{"accountId": "account-1", "clientMsgId": "x", "text": "x"}, 503)
}
func TestRateLimitEveryAttemptResetsCooldown(t *testing.T) {
	ts := testServer(t)
	call(t, ts, "POST", "/accounts/account-1/connect", nil, 200)
	gid := call(t, ts, "POST", "/groups", map[string]string{"creatorAccountId": "account-1"}, 200)["groupId"].(string)
	p := "/groups/" + gid + "/send"
	in := map[string]string{"accountId": "account-1", "clientMsgId": "rate", "text": "hello"}
	call(t, ts, "POST", "/admin/faults", map[string]any{"operation": "send", "accountId": "account-1", "code": "RATE_LIMITED", "retryAfterSeconds": 1}, 200)
	first := call(t, ts, "POST", p, in, 429)
	if first["retryAfterSeconds"] != float64(1) {
		t.Fatal(first)
	}
	time.Sleep(600 * time.Millisecond)
	call(t, ts, "POST", p, in, 429)
	time.Sleep(600 * time.Millisecond)
	call(t, ts, "POST", p, in, 429)
	time.Sleep(1100 * time.Millisecond)
	call(t, ts, "POST", p, in, 202)
}
func TestUnknownSendAndKickConvergeWithinTwoSeconds(t *testing.T) {
	ts := testServer(t)
	call(t, ts, "POST", "/accounts/account-1/connect", nil, 200)
	gid := call(t, ts, "POST", "/groups", map[string]string{"creatorAccountId": "account-1"}, 200)["groupId"].(string)
	call(t, ts, "POST", "/admin/events", map[string]any{"type": "member_joined", "data": map[string]string{"groupId": gid, "platformUserId": "90001"}}, 200)
	for _, operation := range []string{"send", "kick"} {
		call(t, ts, "POST", "/admin/faults", map[string]any{"operation": operation, "code": "NETWORK_TIMEOUT", "effect": true, "settleAfterMs": 300}, 200)
	}
	call(t, ts, "POST", "/groups/"+gid+"/send", map[string]string{"accountId": "account-1", "clientMsgId": "timeout-accepted", "text": "accepted"}, 504)
	call(t, ts, "POST", "/groups/"+gid+"/kick", map[string]string{"byAccountId": "account-1", "targetPlatformUserId": "90001"}, 504)
	call(t, ts, "GET", "/groups/"+gid+"/messages/by-client-id/timeout-accepted", nil, 404)
	time.Sleep(450 * time.Millisecond)
	call(t, ts, "GET", "/groups/"+gid+"/messages/by-client-id/timeout-accepted", nil, 200)
	res, e := http.Get(ts.URL + "/groups/" + gid + "/members")
	if e != nil {
		t.Fatal(e)
	}
	var ms []map[string]string
	_ = json.NewDecoder(res.Body).Decode(&ms)
	res.Body.Close()
	if len(ms) != 1 {
		t.Fatalf("kick failed to converge: %v", ms)
	}
	call(t, ts, "POST", "/admin/faults", map[string]any{"operation": "send", "code": "NETWORK_TIMEOUT", "effect": false}, 200)
	call(t, ts, "POST", "/groups/"+gid+"/send", map[string]string{"accountId": "account-1", "clientMsgId": "timeout-rejected", "text": "reject"}, 504)
	time.Sleep(2100 * time.Millisecond)
	call(t, ts, "GET", "/groups/"+gid+"/messages/by-client-id/timeout-rejected", nil, 404)
}
func TestInviteJoinPromotionAndOwnerLeaveContracts(t *testing.T) {
	ts := testServer(t)
	for _, id := range []string{"account-1", "account-2", "account-3"} {
		call(t, ts, "POST", "/accounts/"+id+"/connect", nil, 200)
	}
	gid := call(t, ts, "POST", "/groups", map[string]string{"creatorAccountId": "account-1"}, 200)["groupId"].(string)
	p := "/groups/" + gid
	call(t, ts, "POST", "/admin/faults", map[string]any{"operation": "invite", "code": "INVITE_DELAY", "settleAfterMs": 100}, 200)
	link := call(t, ts, "POST", p+"/invite", map[string]any{}, 200)["inviteLink"].(string)
	join := map[string]string{"accountId": "account-2", "inviteLink": link}
	call(t, ts, "POST", p+"/join", join, 409)
	time.Sleep(120 * time.Millisecond)
	call(t, ts, "POST", p+"/join", join, 202)
	call(t, ts, "POST", p+"/promote", map[string]string{"byAccountId": "account-1", "accountId": "account-2"}, 409)
	time.Sleep(120 * time.Millisecond)
	call(t, ts, "POST", p+"/join", join, 409)
	call(t, ts, "POST", p+"/promote", map[string]string{"byAccountId": "account-1", "accountId": "account-2"}, 200)
	call(t, ts, "POST", p+"/kick", map[string]string{"byAccountId": "account-3", "targetPlatformUserId": "10002"}, 403)
	call(t, ts, "POST", "/admin/faults", map[string]any{"operation": "leave", "accountId": "account-2", "code": "LEAVE_FAILED"}, 200)
	call(t, ts, "POST", p+"/leave", map[string]string{"accountId": "account-2"}, 500)
	call(t, ts, "POST", p+"/leave", map[string]string{"accountId": "account-1"}, 200)
	e := call(t, ts, "POST", p+"/kick", map[string]string{"byAccountId": "account-2", "targetPlatformUserId": "10002"}, 409)
	if e["code"] != "OWNER_LEFT" {
		t.Fatal(e)
	}
}
