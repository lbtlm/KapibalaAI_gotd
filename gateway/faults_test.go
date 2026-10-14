package gateway_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAsyncSuspensionRemovesMembershipAndBlocksReconnect(t *testing.T) {
	ts := testServer(t)
	call(t, ts, "POST", "/accounts/account-1/connect", nil, 200)
	gid := call(t, ts, "POST", "/groups", map[string]string{"creatorAccountId": "account-1"}, 200)["groupId"].(string)
	call(t, ts, "POST", "/admin/faults", map[string]any{"operation": "send", "code": "ASYNC_FAILED", "effect": true}, 200)
	call(t, ts, "POST", "/groups/"+gid+"/send", map[string]string{"accountId": "account-1", "clientMsgId": "async-suspend", "text": "blocked"}, 202)
	time.Sleep(100 * time.Millisecond)
	call(t, ts, "POST", "/accounts/account-1/connect", nil, 403)
	res, e := http.Get(ts.URL + "/groups/" + gid + "/members")
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	var ms []map[string]string
	_ = json.NewDecoder(res.Body).Decode(&ms)
	if len(ms) != 0 {
		t.Fatalf("suspended account still member: %v", ms)
	}
}

func TestHistoryRepeatsEventsAndNeverDeduplicatesClientMsgID(t *testing.T) {
	ts := testServer(t)
	call(t, ts, "POST", "/accounts/account-1/connect", nil, 200)
	gid := call(t, ts, "POST", "/groups", map[string]string{"creatorAccountId": "account-1"}, 200)["groupId"].(string)
	for _, text := range []string{"first", "second"} {
		call(t, ts, "POST", "/groups/"+gid+"/send", map[string]string{"accountId": "account-1", "clientMsgId": "same", "text": text}, 202)
	}
	time.Sleep(120 * time.Millisecond)
	earliest := call(t, ts, "GET", "/groups/"+gid+"/messages/by-client-id/same", nil, 200)
	call(t, ts, "POST", "/admin/delivery", map[string]any{"duplicate": true, "reorder": true, "disconnectAfter": 8}, 200)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/events?since=0", nil)
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	scan := bufio.NewScanner(res.Body)
	seen := map[float64]int{}
	messages := map[string]bool{}
	sent := map[string]bool{}
	for scan.Scan() {
		line := scan.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var data map[string]any
		_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &data)
		seen[data["eventId"].(float64)]++
		if data["type"] == "message" {
			messages[data["msgId"].(string)] = true
		}
		if data["type"] == "message_sent" {
			sent[data["msgId"].(string)] = true
		}
	}
	if len(messages) != 2 || len(sent) != 2 || !sent[earliest["msgId"].(string)] {
		t.Fatalf("client-id deduplicated or lost sent pairing: %v %v %v", messages, sent, earliest)
	}
	if len(seen) != 4 {
		t.Fatalf("unexpected creator joined or missing frame: %v", seen)
	}
	for id, count := range seen {
		if count != 2 {
			t.Fatalf("event %v repeated %d times", id, count)
		}
	}
	call(t, ts, "POST", "/admin/delivery", map[string]any{"disconnectAfter": 2}, 200)
	req, _ = http.NewRequestWithContext(ctx, "GET", ts.URL+"/events?since=2", nil)
	res, e = http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	scan = bufio.NewScanner(res.Body)
	for scan.Scan() {
		line := scan.Text()
		if strings.HasPrefix(line, "data: ") {
			var data map[string]any
			_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &data)
			if data["eventId"].(float64) <= 2 {
				t.Fatalf("nonexclusive since: %v", data)
			}
		}
	}
}
