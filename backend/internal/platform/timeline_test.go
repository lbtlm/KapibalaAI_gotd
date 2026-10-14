package platform_test

import (
	"context"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReorderedDuplicateEventsOfflineIdentityAndStablePagination(t *testing.T) {
	g := gatewayFixture(t)
	a, s := fixture(t, platform.Config{GatewayURL: g.server.URL})
	a.Start(context.Background())
	token, _ := login(t, s, "admin")
	group := createGroup(t, s, token)
	sent := time.Now().UTC().Truncate(time.Millisecond).Format(time.RFC3339Nano)
	g.mu.Lock()
	next := int64(len(g.events) + 1)
	one := map[string]any{"eventId": next + 1, "type": "message", "groupId": "gateway-group", "msgId": "one", "senderPlatformUserId": "outside", "text": "one", "sentAt": sent}
	two := map[string]any{"eventId": next, "type": "message", "groupId": "gateway-group", "msgId": "two", "senderPlatformUserId": "outside", "text": "two", "sentAt": sent}
	g.events = append(g.events, gatewayEvent{next + 1, "message", one}, gatewayEvent{next, "message", two}, gatewayEvent{next + 1, "message", one})
	g.mu.Unlock()
	call(t, s, "POST", "/api/accounts/account-2/transition", token, `{"expectedFrom":"online","to":"idle"}`)
	g.event("message", map[string]any{"groupId": "gateway-group", "msgId": "offline-old", "senderPlatformUserId": "p-account-2", "text": "old own", "sentAt": time.Now().Add(-365 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)})
	eventually(t, 3*time.Second, func() bool {
		_, v, _ := call(t, s, "GET", "/api/groups/"+group+"/messages", token, "")
		return len(v["items"].([]any)) == 3
	})
	_, page, _ := call(t, s, "GET", "/api/groups/"+group+"/messages?limit=2", token, "")
	if len(page["items"].([]any)) != 2 || page["nextCursor"] == nil {
		t.Fatalf("first page %v", page)
	}
	cursor := page["nextCursor"].(string)
	g.event("message", map[string]any{"groupId": "gateway-group", "msgId": "new-but-older", "senderPlatformUserId": "outside", "text": "late insert", "sentAt": time.Now().Add(-400 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)})
	eventually(t, 3*time.Second, func() bool {
		_, v, _ := call(t, s, "GET", "/api/groups/"+group+"/messages", token, "")
		return len(v["items"].([]any)) == 4
	})
	_, older, _ := call(t, s, "GET", "/api/groups/"+group+"/messages?limit=2&before="+cursor, token, "")
	items := older["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["msgId"] != "offline-old" || items[0].(map[string]any)["isOwn"] != true {
		t.Fatalf("stable snapshot/offline identity %v", older)
	}
}

func TestYoungerLowerEventBlocksEligibleHigherEventAcrossRestart(t *testing.T) {
	g := gatewayFixture(t)
	a, s, cfg := fixtureConfig(t, platform.Config{GatewayURL: g.server.URL})
	a.Start(context.Background())
	token, _ := login(t, s, "admin")
	group := createGroup(t, s, token)
	g.mu.Lock()
	next := int64(len(g.events) + 1)
	left := map[string]any{"eventId": next + 1, "type": "member_left", "groupId": "gateway-group", "platformUserId": "reordered-visitor"}
	joined := map[string]any{"eventId": next, "type": "member_joined", "groupId": "gateway-group", "platformUserId": "reordered-visitor"}
	g.events = append(g.events, gatewayEvent{next + 1, "member_left", left})
	g.mu.Unlock()
	time.Sleep(900 * time.Millisecond)
	g.mu.Lock()
	g.events = append(g.events, gatewayEvent{next, "member_joined", joined}, gatewayEvent{next + 1, "member_left", left}, gatewayEvent{next, "member_joined", joined})
	g.mu.Unlock()
	// The higher ID is eligible while the known lower ID is still young.
	time.Sleep(400 * time.Millisecond)
	a.Close()
	restored, e := platform.New(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	server := httptest.NewServer(restored.Handler())
	defer server.Close()
	restored.Start(context.Background())
	g.event("message", map[string]any{"groupId": "gateway-group", "msgId": "ordered-prefix-barrier", "senderPlatformUserId": "outside", "text": "barrier", "sentAt": time.Now().UTC().Format(time.RFC3339Nano)})
	eventually(t, 4*time.Second, func() bool {
		_, messages, _ := call(t, server, "GET", "/api/groups/"+group+"/messages", token, "")
		return len(messages["items"].([]any)) == 1
	})
	_, detail, _ := call(t, server, "GET", "/api/groups/"+group, token, "")
	for _, raw := range detail["members"].([]any) {
		if raw.(map[string]any)["platformUserId"] == "reordered-visitor" {
			t.Fatal("lower join overtook higher left despite known ordered inbox prefix")
		}
	}
}
