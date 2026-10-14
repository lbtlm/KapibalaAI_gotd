package gateway_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gateway "github.com/lbtlm/KapibalaAI_gotd/gateway"
)

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	s, err := gateway.New(context.Background(), gateway.Config{Mode: "fake", AdminEnabled: true, Store: gateway.NewMemoryStore()})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s)
	t.Cleanup(func() { ts.Close(); s.Close() })
	return ts
}

func TestSuspensionOnConnectIsPermanent(t *testing.T) {
	ts := testServer(t)
	call(t, ts, "POST", "/admin/faults", map[string]any{"operation": "connect", "accountId": "account-2", "code": "ACCOUNT_SUSPENDED"}, 200)
	call(t, ts, "POST", "/accounts/account-2/connect", nil, 403)
	again := call(t, ts, "POST", "/accounts/account-2/connect", nil, 403)
	if again["code"] != "ACCOUNT_SUSPENDED" {
		t.Fatal(again)
	}
}

func TestMemberListChangesBeforeDelayedEvent(t *testing.T) {
	ts := testServer(t)
	call(t, ts, "POST", "/accounts/account-1/connect", nil, 200)
	g := call(t, ts, "POST", "/groups", map[string]string{"creatorAccountId": "account-1"}, 200)["groupId"].(string)
	call(t, ts, "POST", "/admin/events", map[string]any{"type": "member_joined", "delayMs": 150, "data": map[string]string{"groupId": g, "platformUserId": "90001"}}, 200)
	res, err := http.Get(ts.URL + "/groups/" + g + "/members")
	if err != nil {
		t.Fatal(err)
	}
	var members []map[string]string
	_ = json.NewDecoder(res.Body).Decode(&members)
	res.Body.Close()
	if len(members) != 2 {
		t.Fatalf("member list did not change immediately: %v", members)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/events?since=0", nil)
	stream, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	scanner := bufio.NewScanner(stream.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			var data map[string]any
			_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &data)
			if data["groupId"] != g || data["platformUserId"] != "90001" {
				t.Fatalf("wrong delayed member event: %v", data)
			}
			return
		}
	}
	t.Fatalf("event not delivered: %v", scanner.Err())
}

func call(t *testing.T, ts *httptest.Server, method, path string, body any, status int) map[string]any {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, ts.URL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	if res.StatusCode != status {
		t.Fatalf("%s %s: got %d %v, want %d", method, path, res.StatusCode, out, status)
	}
	return out
}

func TestAccountIdentityAndCreatorMembership(t *testing.T) {
	ts := testServer(t)
	first := call(t, ts, "POST", "/accounts/account-1/connect", nil, 200)
	call(t, ts, "POST", "/accounts/account-1/disconnect", nil, 200)
	second := call(t, ts, "POST", "/accounts/account-1/connect", nil, 200)
	if first["platformUserId"] != "10001" || second["platformUserId"] != first["platformUserId"] {
		t.Fatalf("identity changed: %v %v", first, second)
	}
	g := call(t, ts, "POST", "/groups", map[string]string{"creatorAccountId": "account-1"}, 200)
	group := g["groupId"].(string)
	res, err := http.Get(ts.URL + "/groups/" + group + "/members")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var members []map[string]string
	_ = json.NewDecoder(res.Body).Decode(&members)
	if len(members) != 1 || members[0]["platformUserId"] != "10001" {
		t.Fatalf("creator absent: %v", members)
	}
	call(t, ts, "POST", "/accounts/account-1/disconnect", nil, 200)
	e := call(t, ts, "POST", "/groups/"+group+"/send", map[string]string{"accountId": "account-1", "clientMsgId": "x", "text": "hello"}, 409)
	if e["code"] != "ACCOUNT_OFFLINE" {
		t.Fatal(e)
	}
}
