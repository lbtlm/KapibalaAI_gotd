package platform_test

import (
	"context"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
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
