package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/domain"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
)

func TestQueuedOriginalSendToolWaitsAfterHardKillWhileNewKeyReuseDoesNotRepeatEffect(t *testing.T) {
	g := gatewayFixture(t)
	var audits atomic.Int32
	results := make(chan map[string]any, 2)
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/agent/audit" {
			audits.Add(1)
			fmt.Fprint(w, `{"verdict":"pass"}`)
			return
		}
		var req domain.TurnRequest
		json.NewDecoder(r.Body).Decode(&req)
		turn := 0
		for _, m := range req.Messages {
			for _, b := range m.Content {
				if b.Type == "tool_result" {
					turn++
					if turn == len(req.Messages)/2 {
						var out map[string]any
						json.Unmarshal([]byte(b.Content), &out)
						results <- out
					}
				}
			}
		}
		if turn < 2 {
			fmt.Fprintf(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"send-%d","name":"send_message","input":{"text":"one effect","idempotency_key":"one-key"}}]}`, turn+1)
		} else {
			fmt.Fprint(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"restored"}]}`)
		}
	}))
	defer agent.Close()
	a, _, cfg := fixtureConfig(t, platform.Config{GatewayURL: g.server.URL, AgentURL: agent.URL})
	a.Close()
	cmd, s := childServer(t, cfg)
	alive := true
	defer func() {
		if alive {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	token, _ := login(t, s, "admin")
	group := createGroup(t, s, token)
	pool, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	barrier, e := pool.Acquire(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer barrier.Release()
	barrier.Exec(context.Background(), `SELECT pg_advisory_lock(9151010)`)
	defer barrier.Exec(context.Background(), `SELECT pg_advisory_unlock(9151010)`)
	// Hold dispatch at a PostgreSQL boundary, after the queued message/effect
	// commits. No gateway request has been sent when the process is killed.
	if _, e = pool.Exec(context.Background(), `CREATE FUNCTION hold_dispatch() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(9151010); RETURN NEW; END $$; CREATE TRIGGER hold_dispatch BEFORE UPDATE ON messages FOR EACH ROW WHEN (NEW.delivery_status='dispatching') EXECUTE FUNCTION hold_dispatch()`); e != nil {
		t.Fatal(e)
	}
	call(t, s, "PATCH", "/api/groups/"+group, token, `{"agentEnabled":true}`)
	g.event("message", map[string]any{"groupId": "gateway-group", "msgId": "queued-trigger", "senderPlatformUserId": "outside", "text": "trigger", "sentAt": time.Now().UTC().Format(time.RFC3339Nano)})
	var run string
	eventually(t, 4*time.Second, func() bool {
		runs := runList(t, s, token, group)
		if len(runs) != 1 {
			return false
		}
		run = runs[0]["id"].(string)
		_, messages, _ := call(t, s, "GET", "/api/groups/"+group+"/messages", token, "")
		for _, item := range messages["items"].([]any) {
			m := item.(map[string]any)
			if m["clientMsgId"] != nil && m["deliveryStatus"] == "queued" {
				return true
			}
		}
		return false
	})
	cmd.Process.Kill()
	cmd.Wait()
	alive = false
	restarted, server := childServer(t, cfg)
	defer func() { restarted.Process.Kill(); restarted.Wait() }()
	// The recovered original tool must still wait; it must not hand queued back
	// to the provider as if this were a new tool with the same key.
	var early map[string]any
	select {
	case early = <-results:
	case <-time.After(350 * time.Millisecond):
	}
	barrier.Exec(context.Background(), `SELECT pg_advisory_unlock(9151010)`)
	if early != nil {
		t.Fatalf("original tool returned before acceptance: %v", early)
	}
	for i := 0; i < 2; i++ {
		select {
		case result := <-results:
			if result["deliveryStatus"] != "accepted" && result["deliveryStatus"] != "sent" {
				t.Fatalf("invalid original/repeated tool result: %v", result)
			}
		case <-time.After(4 * time.Second):
			t.Fatal("send result missing")
		}
	}
	eventually(t, 2*time.Second, func() bool {
		_, detail, _ := call(t, server, "GET", "/api/agent-runs/"+run, token, "")
		return detail["status"] == "finished"
	})
	g.mu.Lock()
	sends := g.sendCount
	g.mu.Unlock()
	if sends != 1 || audits.Load() != 1 {
		t.Fatalf("recovery/key reuse repeated send or audit: %d/%d", sends, audits.Load())
	}
}

func TestNewToolIDReusingQueuedSendKeyReturnsCurrentStateWithoutWaitingOrAuditing(t *testing.T) {
	g := gatewayFixture(t)
	base := g.server.Config.Handler
	release := make(chan struct{})
	var sends, audits atomic.Int32
	g.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/groups/gateway-group/send" {
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			sends.Add(1)
			select {
			case <-release:
			case <-r.Context().Done():
			}
			w.WriteHeader(202)
			fmt.Fprint(w, `{"accepted":true}`)
			return
		}
		base.ServeHTTP(w, r)
	})
	results := make(chan map[string]any, 2)
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/agent/audit" {
			audits.Add(1)
			fmt.Fprint(w, `{"verdict":"pass"}`)
			return
		}
		var req domain.TurnRequest
		json.NewDecoder(r.Body).Decode(&req)
		turn := (len(req.Messages) - 1) / 2
		if turn > 0 {
			var out map[string]any
			json.Unmarshal([]byte(req.Messages[len(req.Messages)-1].Content[0].Content), &out)
			results <- out
		}
		if turn < 2 {
			fmt.Fprintf(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"send-%d","name":"send_message","input":{"text":"one effect","idempotency_key":"pending-key"}}]}`, turn+1)
		} else {
			fmt.Fprint(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}`)
		}
	}))
	defer agent.Close()
	defer close(release)
	a, s := fixture(t, platform.Config{GatewayURL: g.server.URL, AgentURL: agent.URL})
	a.Start(context.Background())
	token, _ := login(t, s, "admin")
	group := createGroup(t, s, token)
	call(t, s, "PATCH", "/api/groups/"+group, token, `{"agentEnabled":true}`)
	g.event("message", map[string]any{"groupId": "gateway-group", "msgId": "reuse-trigger", "senderPlatformUserId": "outside", "text": "trigger", "sentAt": time.Now().UTC().Format(time.RFC3339Nano)})
	select {
	case original := <-results:
		if original["code"] != "SEND_TIMEOUT" {
			t.Fatalf("original must time out while gateway is held: %v", original)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("original send did not finish its five-second wait")
	}
	select {
	case repeated := <-results:
		if repeated["deliveryStatus"] != "queued" || repeated["clientMsgId"] == nil {
			t.Fatalf("new tool did not return current queued status: %v", repeated)
		}
	case <-time.After(time.Second):
		t.Fatal("new tool reusing key waited for acceptance")
	}
	if sends.Load() != 1 || audits.Load() != 1 {
		t.Fatalf("new tool repeated effect/audit: %d/%d", sends.Load(), audits.Load())
	}
}
