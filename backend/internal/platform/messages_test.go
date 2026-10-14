package platform_test

import (
	"context"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
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
