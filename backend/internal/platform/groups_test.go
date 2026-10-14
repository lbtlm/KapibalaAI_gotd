package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type gatewayEvent struct {
	ID   int64
	Type string
	Data map[string]any
}
type fakeGateway struct {
	server      *httptest.Server
	mu          sync.Mutex
	events      []gatewayEvent
	members     map[string]bool
	sendCount   int
	timeoutSend bool
	settled     map[string]map[string]any
	failLeave   bool
	rateOnce    bool
}

func gatewayFixture(t *testing.T) *fakeGateway {
	g := &fakeGateway{members: map[string]bool{}, settled: map[string]map[string]any{}}
	m := http.NewServeMux()
	m.HandleFunc("POST /accounts/{id}/connect", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"platformUserId": "p-" + r.PathValue("id")})
	})
	m.HandleFunc("POST /accounts/{id}/disconnect", func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(map[string]any{}) })
	m.HandleFunc("POST /groups", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.members["p-account-1"] = true
		g.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"groupId": "gateway-group"})
	})
	m.HandleFunc("POST /groups/{id}/invite", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"inviteLink": "invite", "readyAfterMs": 0})
	})
	m.HandleFunc("POST /groups/{id}/join", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		json.NewDecoder(r.Body).Decode(&b)
		p := "p-" + b["accountId"]
		g.mu.Lock()
		g.members[p] = true
		g.mu.Unlock()
		g.event("member_joined", map[string]any{"groupId": "gateway-group", "platformUserId": p})
		w.WriteHeader(202)
		fmt.Fprint(w, `{"accepted":true}`)
	})
	m.HandleFunc("POST /groups/{id}/promote", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{}`) })
	m.HandleFunc("GET /groups/{id}/members", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		out := []any{}
		for p := range g.members {
			out = append(out, map[string]any{"platformUserId": p})
		}
		json.NewEncoder(w).Encode(out)
	})
	m.HandleFunc("POST /groups/{id}/leave", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		json.NewDecoder(r.Body).Decode(&b)
		g.mu.Lock()
		if g.failLeave && b["accountId"] == "account-2" {
			g.mu.Unlock()
			w.WriteHeader(500)
			fmt.Fprint(w, `{"code":"LEAVE_FAILED"}`)
			return
		}
		delete(g.members, "p-"+b["accountId"])
		g.mu.Unlock()
		g.event("member_left", map[string]any{"groupId": "gateway-group", "platformUserId": "p-" + b["accountId"]})
		fmt.Fprint(w, `{}`)
	})
	m.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		var since int64
		fmt.Sscan(r.URL.Query().Get("since"), &since)
		readIndex := -1
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			g.mu.Lock()
			events := append([]gatewayEvent{}, g.events...)
			g.mu.Unlock()
			for index, e := range events {
				if index > readIndex && e.ID > since {
					b, _ := json.Marshal(e.Data)
					fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.ID, e.Type, b)
					w.(http.Flusher).Flush()
					readIndex = index
				}
			}
			select {
			case <-r.Context().Done():
				return
			case <-tick.C:
			}
		}
	})
	m.HandleFunc("POST /groups/{id}/send", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		json.NewDecoder(r.Body).Decode(&b)
		g.mu.Lock()
		g.sendCount++
		timed := g.timeoutSend
		if g.rateOnce {
			g.rateOnce = false
			g.mu.Unlock()
			w.WriteHeader(429)
			fmt.Fprint(w, `{"code":"RATE_LIMITED","retryAfterSeconds":1}`)
			return
		}
		g.mu.Unlock()
		delay := 100 * time.Millisecond
		if timed {
			delay = 1500 * time.Millisecond
		}
		time.AfterFunc(delay, func() {
			msg := "m-" + b["clientMsgId"]
			sent := time.Now().UTC().Format(time.RFC3339Nano)
			g.mu.Lock()
			g.settled[b["clientMsgId"]] = map[string]any{"msgId": msg, "sentAt": sent}
			g.mu.Unlock()
			g.event("message_sent", map[string]any{"clientMsgId": b["clientMsgId"], "msgId": msg, "sentAt": sent})
			g.event("message", map[string]any{"groupId": "gateway-group", "msgId": msg, "senderPlatformUserId": "p-" + b["accountId"], "text": b["text"], "sentAt": sent})
		})
		if timed {
			w.WriteHeader(504)
			fmt.Fprint(w, `{"code":"NETWORK_TIMEOUT"}`)
			return
		}
		w.WriteHeader(202)
		fmt.Fprint(w, `{"accepted":true}`)
	})
	m.HandleFunc("GET /groups/{id}/messages/by-client-id/{client}", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		v, ok := g.settled[r.PathValue("client")]
		g.mu.Unlock()
		if !ok {
			w.WriteHeader(404)
			fmt.Fprint(w, `{"code":"NOT_FOUND"}`)
			return
		}
		json.NewEncoder(w).Encode(v)
	})
	g.server = httptest.NewServer(m)
	t.Cleanup(g.server.Close)
	return g
}
func (g *fakeGateway) event(typ string, data map[string]any) {
	g.mu.Lock()
	defer g.mu.Unlock()
	id := int64(len(g.events) + 1)
	data["type"] = typ
	data["eventId"] = id
	g.events = append(g.events, gatewayEvent{id, typ, data})
}
func eventually(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	end := time.Now().Add(timeout)
	for time.Now().Before(end) {
		if fn() {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("behavior did not converge before timeout")
}
func createGroup(t *testing.T, s *httptest.Server, token string) string {
	for _, id := range []string{"account-1", "account-2", "account-3"} {
		st, b, _ := call(t, s, "POST", "/api/accounts/"+id+"/connect", token, "{}")
		if st != 200 {
			t.Fatalf("connect %d %v", st, b)
		}
	}
	st, b, _ := call(t, s, "POST", "/api/groups", token, `{"creatorAccountId":"account-1","memberAccountIds":["account-2","account-3"]}`)
	if st != 202 {
		t.Fatalf("create %d %v", st, b)
	}
	job := b["jobId"].(string)
	var group string
	eventually(t, 12*time.Second, func() bool {
		_, v, _ := call(t, s, "GET", "/api/jobs/"+job, token, "")
		if v["status"] == "failed" {
			t.Fatalf("job failed %v", v)
		}
		if v["status"] == "finished" {
			group = v["groupId"].(string)
			return true
		}
		return false
	})
	return group
}
func TestGroupJobCreatesMembersThenPromotesFirstMember(t *testing.T) {
	g := gatewayFixture(t)
	a, s := fixture(t, platform.Config{GatewayURL: g.server.URL})
	a.Start(context.Background())
	token, _ := login(t, s, "admin")
	group := createGroup(t, s, token)
	st, b, _ := call(t, s, "GET", "/api/groups/"+group, token, "")
	if st != 200 || len(b["members"].([]any)) != 3 {
		t.Fatalf("members %d %v", st, b)
	}
	for _, m := range b["members"].([]any) {
		v := m.(map[string]any)
		if v["accountId"] == "account-2" && v["role"] != "admin" {
			t.Fatalf("not promoted %v", v)
		}
	}
}
