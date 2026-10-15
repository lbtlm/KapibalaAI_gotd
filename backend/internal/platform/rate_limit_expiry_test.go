package platform_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
)

func TestRateLimitExpiresWithoutQueuedMessagesAcrossInstances(t *testing.T) {
	a, server, cfg := fixtureConfig(t)
	insertAccountRows(t, cfg, "expired", "future", "offline", "terminal")
	token, _ := login(t, server, "viewer")
	businessSQL(t, cfg, `UPDATE accounts SET status='rate_limited',rate_limited_until=now()-interval '1 second' WHERE id='expired'; UPDATE accounts SET status='rate_limited',rate_limited_until=now()+interval '1 hour' WHERE id='future'; UPDATE accounts SET status='disconnected',rate_limited_until=now()-interval '1 second' WHERE id='offline'; UPDATE accounts SET status='suspended',rate_limited_until=now()-interval '1 second' WHERE id='terminal'`)
	peer, err := platform.New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	if err = a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = peer.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, 2*time.Second, func() bool { return accountStatus(t, server, token, "expired") == "online" })
	for id, expected := range map[string]string{"future": "rate_limited", "offline": "disconnected", "terminal": "suspended"} {
		if got := accountStatus(t, server, token, id); got != expected {
			t.Fatalf("expiry overwrote %s: got %s want %s", id, got, expected)
		}
	}
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err = c.WriteJSON(map[string]any{"type": "auth", "accessToken": token, "sinceSeq": 0}); err != nil {
		t.Fatal(err)
	}
	var frame map[string]any
	if err = c.ReadJSON(&frame); err != nil || frame["success"] != true {
		t.Fatal("WS auth", err, frame)
	}
	if err = c.ReadJSON(&frame); err != nil {
		t.Fatal(err)
	}
	payload := frame["payload"].(map[string]any)
	if frame["type"] != "account_status_changed" || payload["accountId"] != "expired" || payload["from"] != "rate_limited" || payload["to"] != "online" {
		t.Fatalf("expiry event %v", frame)
	}
	// Both instances keep ticking, but only one committed transition is allowed.
	c.SetReadDeadline(time.Now().Add(350 * time.Millisecond))
	if err = c.ReadJSON(&frame); err == nil {
		t.Fatalf("duplicate expiry event: %v", frame)
	}
}
