package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
)

func TestInboxWriteFailureRecoversAfterRestartWithoutDuplicateOrOwnTrigger(t *testing.T) {
	var turns atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		turns.Add(1)
		fmt.Fprint(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"handled once"}]}`)
	}))
	defer provider.Close()
	a, server, cfg := fixtureConfig(t, platform.Config{AgentURL: provider.URL})
	group := insertBusinessGroup(t, cfg)
	businessSQL(t, cfg, `UPDATE groups SET agent_enabled=true; UPDATE accounts SET status='disconnected' WHERE id='account-3'; CREATE FUNCTION reject_incoming() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.msg_id='outside' THEN RAISE EXCEPTION 'injected message write failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_incoming BEFORE INSERT ON messages FOR EACH ROW EXECUTE FUNCTION reject_incoming()`)
	token, _ := login(t, server, "viewer")
	// Persisted inbox inputs exercise the transaction/recovery boundary, not an
	// SSE transport or a claim of Telegram delivery. Event 2 arrives before 1.
	putInbox(t, cfg, 2, "message", map[string]any{"groupId": "unroutable-database-group", "msgId": "own-old", "senderPlatformUserId": "database-input-c", "text": "old own message", "sentAt": "2020-01-01T00:00:00Z"})
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	putInbox(t, cfg, 1, "message", map[string]any{"groupId": "unroutable-database-group", "msgId": "outside", "senderPlatformUserId": "outside-user", "text": "external input", "sentAt": "2026-01-01T00:00:00Z"})
	putInbox(t, cfg, 3, "message", map[string]any{"groupId": "unroutable-database-group", "msgId": "outside", "senderPlatformUserId": "outside-user", "text": "external input", "sentAt": "2026-01-01T00:00:00Z"})
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(4 * time.Second))
	if err = c.WriteJSON(map[string]any{"type": "auth", "accessToken": token, "sinceSeq": 0}); err != nil {
		t.Fatal(err)
	}
	var frame map[string]any
	for {
		if err = c.ReadJSON(&frame); err != nil {
			t.Fatal(err)
		}
		if frame["type"] == "inconsistency" {
			break
		}
	}
	if frame["payload"].(map[string]any)["ref"] != "1" {
		t.Fatalf("wrong failed input: %v", frame)
	}
	_, timeline, _ := call(t, server, "GET", "/api/groups/"+group+"/messages", token, "")
	if len(timeline["items"].([]any)) != 0 || len(runList(t, server, token, group)) != 0 {
		t.Fatal("failed event partially committed or later event overtook it")
	}
	a.Close()
	businessSQL(t, cfg, `DROP TRIGGER reject_incoming ON messages; DROP FUNCTION reject_incoming()`)
	restarted, err := platform.New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if err = restarted.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	restored := httptest.NewServer(restarted.Handler())
	defer restored.Close()
	run := awaitAgentEnd(t, restored, token, group, 4*time.Second)
	if run["status"] != "finished" || turns.Load() != 1 {
		t.Fatalf("duplicate/own message retriggered Agent: %v turns=%d", run, turns.Load())
	}
	_, timeline, _ = call(t, restored, "GET", "/api/groups/"+group+"/messages", token, "")
	items := timeline["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["msgId"] != "outside" || items[1].(map[string]any)["msgId"] != "own-old" || items[1].(map[string]any)["isOwn"] != true {
		t.Fatalf("dedup/order/offline ownership: %v", items)
	}
}

func TestUnknownSendSurvivesOutageRestartAndLateReceipt(t *testing.T) {
	a, server, cfg := fixtureConfig(t)
	group := insertBusinessGroup(t, cfg)
	token, _ := login(t, server, "viewer")
	businessSQL(t, cfg, `INSERT INTO messages(group_id,client_msg_id,account_id,sender_platform_user_id,is_own,text,delivery_status,dispatch_started_at,reconcile_after) VALUES($1,'unknown-send','account-2','database-input-b',true,'already dispatched input','unknown',now()-interval '10 seconds',now())`, group)
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A real closed endpoint is unavailable. Waiting beyond the 504 convergence
	// deadline must not turn an unqueryable result into failure or a new send.
	deadline := time.Now().Add(5200 * time.Millisecond)
	for time.Now().Before(deadline) {
		_, timeline, _ := call(t, server, "GET", "/api/groups/"+group+"/messages", token, "")
		items := timeline["items"].([]any)
		if len(items) != 1 || items[0].(map[string]any)["deliveryStatus"] != "unknown" {
			t.Fatalf("outage changed unknown delivery: %v", items)
		}
		time.Sleep(100 * time.Millisecond)
	}
	a.Close()
	putInbox(t, cfg, 1, "message", map[string]any{"groupId": "unroutable-database-group", "msgId": "confirmed", "senderPlatformUserId": "database-input-b", "text": "already dispatched input", "sentAt": "2026-01-01T00:00:00Z"})
	putInbox(t, cfg, 2, "message_sent", map[string]any{"clientMsgId": "unknown-send", "msgId": "confirmed", "sentAt": "2026-01-01T00:00:00Z"})
	putInbox(t, cfg, 3, "message_sent", map[string]any{"clientMsgId": "unknown-send", "msgId": "confirmed", "sentAt": "2026-01-01T00:00:00Z"})
	restarted, err := platform.New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if err = restarted.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	restored := httptest.NewServer(restarted.Handler())
	defer restored.Close()
	eventually(t, 3*time.Second, func() bool {
		_, timeline, _ := call(t, restored, "GET", "/api/groups/"+group+"/messages", token, "")
		items := timeline["items"].([]any)
		return len(items) == 1 && items[0].(map[string]any)["deliveryStatus"] == "sent" && items[0].(map[string]any)["clientMsgId"] == "unknown-send" && items[0].(map[string]any)["msgId"] == "confirmed"
	})
}

func putInbox(t *testing.T, cfg platform.Config, id int, kind string, data map[string]any) {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	businessSQL(t, cfg, `INSERT INTO gateway_inbox(event_id,type,data) VALUES($1,$2,$3)`, id, kind, raw)
}
