package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/domain"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
)

// Only the documented Agent HTTP boundary supplies fault responses. Accounts and
// messages are isolated PostgreSQL inputs; no Telegram gateway is simulated.
func TestAgentAuditIndeterminateBlocksWithoutSending(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"http500", 500, `{"verdict":"pass"}`},
		{"invalidJSON", 200, `not json`},
		{"fencedJSON", 200, "```json\n{\"verdict\":\"pass\"}\n```"},
		{"trailingText", 200, `{"verdict":"pass"} trailing`},
		{"missingVerdict", 200, `{"reason":"no decision"}`},
		{"unknownVerdict", 200, `{"verdict":"allow"}`},
		{"nullVerdict", 200, `{"verdict":null}`},
		{"timeout", 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var turns, audits atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/agent/audit" {
					io.Copy(io.Discard, r.Body)
					audits.Add(1)
					if tc.status == 0 {
						<-r.Context().Done()
						return
					}
					w.WriteHeader(tc.status)
					fmt.Fprint(w, tc.body)
					return
				}
				turns.Add(1)
				fmt.Fprint(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"send-1","name":"send_message","input":{"text":"must not send","idempotency_key":"audit-key"}}]}`)
			}))
			defer provider.Close()
			a, server, cfg := fixtureConfig(t, platform.Config{AgentURL: provider.URL})
			group := insertAgentInput(t, cfg)
			token, _ := login(t, server, "viewer")
			if err := a.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			timeout := 5 * time.Second
			if tc.status == 0 {
				timeout = 35 * time.Second
			}
			run := awaitAgentEnd(t, server, token, group, timeout)
			steps := run["steps"].([]any)
			if run["status"] != "blocked" || run["endReason"] != "audit_blocked" || len(steps) != 1 || turns.Load() != 1 || audits.Load() != 3 {
				t.Fatalf("audit must block after exactly three attempts without another turn: run=%v turns=%d audits=%d", run, turns.Load(), audits.Load())
			}
			step := steps[0].(map[string]any)
			if step["isError"] != true || step["errorCode"] != "AUDIT_REJECTED" || step["auditVerdict"] != nil {
				t.Fatalf("blocked audit evidence: %v", step)
			}
			assertNoOutgoingMessages(t, server, token, group)
		})
	}
}

func TestAgentAuditAttemptBudgetSurvivesProcessKill(t *testing.T) {
	var turns, audits atomic.Int32
	blocked := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/agent/audit" {
			io.Copy(io.Discard, r.Body)
			if audits.Add(1) == 2 {
				close(blocked)
				<-r.Context().Done()
				return
			}
			w.WriteHeader(500)
			return
		}
		turns.Add(1)
		fmt.Fprint(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"send-before-kill","name":"send_message","input":{"text":"must not send","idempotency_key":"kill-key"}}]}`)
	}))
	defer provider.Close()
	a, _, cfg := fixtureConfig(t, platform.Config{AgentURL: provider.URL})
	group := insertAgentInput(t, cfg)
	a.Close()
	cmd, server := childServer(t, cfg)
	alive := true
	defer func() {
		if alive {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	token, _ := login(t, server, "viewer")
	select {
	case <-blocked:
	case <-time.After(4 * time.Second):
		t.Fatal("second audit did not start")
	}
	runs := runList(t, server, token, group)
	if len(runs) != 1 {
		t.Fatalf("expected one run: %v", runs)
	}
	runID := runs[0]["id"]
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	alive = false
	restored, restarted := childServer(t, cfg)
	defer func() { restored.Process.Kill(); restored.Wait() }()
	run := awaitAgentEnd(t, restarted, token, group, 5*time.Second)
	if run["id"] != runID || run["status"] != "blocked" || run["endReason"] != "audit_blocked" || len(run["steps"].([]any)) != 1 || audits.Load() != 3 || turns.Load() != 1 {
		t.Fatalf("restart reset audit budget or repeated turn: %v turns=%d audits=%d", run, turns.Load(), audits.Load())
	}
	assertNoOutgoingMessages(t, restarted, token, group)
}

func TestAgentAuditRejectionDoesNotConsumeSendKey(t *testing.T) {
	var turns, audits atomic.Int32
	history := make(chan domain.TurnRequest, 1)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/agent/audit" {
			audits.Add(1)
			fmt.Fprint(w, `{"verdict":"fail","reason":"denied"}`)
			return
		}
		var req domain.TurnRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		n := turns.Add(1)
		if n <= 2 {
			fmt.Fprintf(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"send-%d","name":"send_message","input":{"text":"must not send","idempotency_key":"same-key"}}]}`, n)
		} else {
			history <- req
			fmt.Fprint(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"audit rejected both attempts"}]}`)
		}
	}))
	defer provider.Close()
	a, server, cfg := fixtureConfig(t, platform.Config{AgentURL: provider.URL})
	group := insertAgentInput(t, cfg)
	token, _ := login(t, server, "viewer")
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := awaitAgentEnd(t, server, token, group, 5*time.Second)
	if run["status"] != "finished" || turns.Load() != 3 || audits.Load() != 2 {
		t.Fatalf("rejected key was consumed: %v audits=%d", run, audits.Load())
	}
	req := <-history
	for _, i := range []int{2, 4} {
		if len(req.Messages) != 5 {
			t.Fatalf("unexpected history length: %d", len(req.Messages))
		}
		block := req.Messages[i].Content[0]
		var result map[string]any
		if err := json.Unmarshal([]byte(block.Content), &result); err != nil {
			t.Fatal(err)
		}
		if req.Messages[i].Role != "user" || block.Type != "tool_result" || !block.IsError || result["code"] != "AUDIT_REJECTED" {
			t.Fatalf("rejection not returned to model: %v", req.Messages[i])
		}
	}
	assertNoOutgoingMessages(t, server, token, group)
}

func awaitAgentEnd(t *testing.T, server *httptest.Server, token, group string, timeout time.Duration) map[string]any {
	t.Helper()
	var result map[string]any
	eventually(t, timeout, func() bool {
		runs := runList(t, server, token, group)
		if len(runs) != 1 || runs[0]["status"] == "running" {
			return false
		}
		status, detail, _ := call(t, server, "GET", "/api/agent-runs/"+runs[0]["id"].(string), token, "")
		if status != 200 {
			t.Fatalf("run detail %d", status)
		}
		result = detail
		return true
	})
	return result
}

func TestAgentSendKeyReuseAfterUnknownDeliverySkipsAuditAndEnqueue(t *testing.T) {
	var turns, audits atomic.Int32
	history := make(chan domain.TurnRequest, 1)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/agent/audit" {
			audits.Add(1)
			fmt.Fprint(w, `{"verdict":"pass"}`)
			return
		}
		var req domain.TurnRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		n := turns.Add(1)
		if n <= 2 {
			fmt.Fprintf(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"retry-%d","name":"send_message","input":{"text":"unavailable gateway input","idempotency_key":"unchanged-key"}}]}`, n)
		} else {
			history <- req
			fmt.Fprint(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"delivery remains unknown"}]}`)
		}
	}))
	defer provider.Close()
	a, server, cfg := fixtureConfig(t, platform.Config{AgentURL: provider.URL})
	group := insertAgentInput(t, cfg)
	token, _ := login(t, server, "viewer")
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := awaitAgentEnd(t, server, token, group, 8*time.Second)
	if run["status"] != "finished" || audits.Load() != 1 || turns.Load() != 3 {
		t.Fatalf("same key re-audited or extra turn: %v audits=%d", run, audits.Load())
	}
	req := <-history
	if len(req.Messages) != 5 {
		t.Fatalf("unexpected history length %d", len(req.Messages))
	}
	var first, second map[string]any
	if err := json.Unmarshal([]byte(req.Messages[2].Content[0].Content), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(req.Messages[4].Content[0].Content), &second); err != nil {
		t.Fatal(err)
	}
	if !req.Messages[2].Content[0].IsError || first["code"] != "SEND_TIMEOUT" || req.Messages[4].Content[0].IsError || second["deliveryStatus"] != "unknown" || second["clientMsgId"] == nil {
		t.Fatalf("same-key result: first=%v second=%v", first, second)
	}
	_, timeline, _ := call(t, server, "GET", "/api/groups/"+group+"/messages", token, "")
	outgoing := 0
	for _, value := range timeline["items"].([]any) {
		item := value.(map[string]any)
		if item["clientMsgId"] != nil {
			outgoing++
			if item["clientMsgId"] != second["clientMsgId"] || item["deliveryStatus"] != "unknown" {
				t.Fatalf("retry created another message: %v", item)
			}
		}
	}
	if outgoing != 1 {
		t.Fatalf("same key created %d outgoing records", outgoing)
	}
}

func assertNoOutgoingMessages(t *testing.T, server *httptest.Server, token, group string) {
	t.Helper()
	status, timeline, _ := call(t, server, "GET", "/api/groups/"+group+"/messages", token, "")
	if status != 200 {
		t.Fatalf("timeline %d", status)
	}
	for _, item := range timeline["items"].([]any) {
		if item.(map[string]any)["clientMsgId"] != nil {
			t.Fatalf("unapproved outgoing message: %v", item)
		}
	}
}
