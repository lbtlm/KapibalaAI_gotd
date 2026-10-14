package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestAcceptedIsNotSentAndTimeoutReconcilesWithoutDuplicate(t *testing.T) {
	g := gatewayFixture(t)
	a, s := fixture(t, platform.Config{GatewayURL: g.server.URL})
	a.Start(context.Background())
	token, _ := login(t, s, "admin")
	group := createGroup(t, s, token)
	g.mu.Lock()
	g.timeoutSend = true
	g.mu.Unlock()
	st, b, _ := call(t, s, "POST", "/api/groups/"+group+"/send", token, `{"accountId":"account-2","text":"hello"}`)
	if st != 202 {
		t.Fatalf("send %d %v", st, b)
	}
	seenUnknown := false
	eventually(t, 6*time.Second, func() bool {
		_, v, _ := call(t, s, "GET", "/api/groups/"+group+"/messages", token, "")
		items, ok := v["items"].([]any)
		if !ok || len(items) == 0 {
			return false
		}
		m := items[0].(map[string]any)
		if m["deliveryStatus"] == "unknown" {
			seenUnknown = true
		}
		return m["deliveryStatus"] == "sent"
	})
	if !seenUnknown {
		t.Fatal("504 never exposed unknown")
	}
	g.mu.Lock()
	count := g.sendCount
	g.mu.Unlock()
	if count != 1 {
		t.Fatalf("ambiguous effect repeated: %d", count)
	}
	_, v, _ := call(t, s, "GET", "/api/groups/"+group+"/messages", token, "")
	items := v["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["isOwn"] != true {
		t.Fatalf("self-message duplicated %v", v)
	}
}

func TestSlowOtherAccountDoesNotConsumeAmbiguousSendFiveSecondDeadline(t *testing.T) {
	g := gatewayFixture(t)
	base := g.server.Config.Handler
	slowStarted := make(chan struct{}, 1)
	var attempts atomic.Int32
	g.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/groups/gateway-group/send" {
			var b map[string]string
			json.NewDecoder(r.Body).Decode(&b)
			if b["accountId"] == "account-2" {
				select {
				case slowStarted <- struct{}{}:
				default:
				}
				select {
				case <-r.Context().Done():
					return
				case <-time.After(6 * time.Second):
				}
				w.WriteHeader(202)
				fmt.Fprint(w, `{"accepted":true}`)
				return
			}
			if attempts.Add(1) == 1 {
				w.WriteHeader(504)
				fmt.Fprint(w, `{"code":"NETWORK_TIMEOUT"}`)
				return
			}
			sent := time.Now().UTC().Format(time.RFC3339Nano)
			g.event("message_sent", map[string]any{"clientMsgId": b["clientMsgId"], "msgId": "parallel-confirmed", "sentAt": sent})
			w.WriteHeader(202)
			fmt.Fprint(w, `{"accepted":true}`)
			return
		}
		base.ServeHTTP(w, r)
	})
	a, s := fixture(t, platform.Config{GatewayURL: g.server.URL})
	a.Start(context.Background())
	token, _ := login(t, s, "admin")
	group := createGroup(t, s, token)
	call(t, s, "POST", "/api/groups/"+group+"/send", token, `{"accountId":"account-2","text":"slow"}`)
	select {
	case <-slowStarted:
	case <-time.After(time.Second):
		t.Fatal("slow send did not start")
	}
	start := time.Now()
	st, b, _ := call(t, s, "POST", "/api/groups/"+group+"/send", token, `{"accountId":"account-1","text":"504 then confirmed retry"}`)
	if st != 202 {
		t.Fatal(st, b)
	}
	client := b["clientMsgId"]
	eventually(t, 4900*time.Millisecond, func() bool {
		_, messages, _ := call(t, s, "GET", "/api/groups/"+group+"/messages", token, "")
		for _, raw := range messages["items"].([]any) {
			m := raw.(map[string]any)
			if m["clientMsgId"] == client {
				return m["deliveryStatus"] == "accepted" || m["deliveryStatus"] == "sent"
			}
		}
		return false
	})
	if time.Since(start) >= 5*time.Second || attempts.Load() != 2 {
		t.Fatalf("deadline/retry violated: %v attempts=%d", time.Since(start), attempts.Load())
	}
}
