package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/domain"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestAgentBadJSONUnknownToolThenIdempotentAuditedSendFinishes(t *testing.T) {
	var mu sync.Mutex
	turns, audits := 0, 0
	m := http.NewServeMux()
	m.HandleFunc("POST /agent/turn", func(w http.ResponseWriter, r *http.Request) {
		var b domain.TurnRequest
		json.NewDecoder(r.Body).Decode(&b)
		if len(b.Tools) != 4 || b.RunID == "" {
			t.Error("invalid tools/runId")
		}
		mu.Lock()
		turns++
		n := turns
		mu.Unlock()
		switch n {
		case 1:
			fmt.Fprint(w, "```json\n{}\n```")
		case 2:
			fmt.Fprint(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"u1","name":"unknown","input":{}}]}`)
		case 3:
			fmt.Fprint(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"u2","name":"send_message","input":{"text":"hello","idempotency_key":"same"}}]}`)
		case 4:
			fmt.Fprint(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"u3","name":"send_message","input":{"text":"hello again","idempotency_key":"same"}}]}`)
		default:
			fmt.Fprint(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"u4","name":"finish","input":{"summary":"done"}}]}`)
		}
	})
	m.HandleFunc("POST /agent/audit", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		audits++
		mu.Unlock()
		fmt.Fprint(w, `{"verdict":"pass","reason":"test"}`)
	})
	agent := httptest.NewServer(m)
	defer agent.Close()
	g := gatewayFixture(t)
	g.timeoutSend = true
	a, s := fixture(t, platform.Config{GatewayURL: g.server.URL, AgentURL: agent.URL})
	a.Start(context.Background())
	token, _ := login(t, s, "admin")
	group := createGroup(t, s, token)
	call(t, s, "PATCH", "/api/groups/"+group, token, `{"agentEnabled":true}`)
	g.event("message", map[string]any{"groupId": "gateway-group", "msgId": "external-1", "senderPlatformUserId": "outside", "text": "hello", "sentAt": time.Now().UTC().Format(time.RFC3339Nano)})
	var runID string
	eventually(t, 12*time.Second, func() bool {
		req, _ := http.NewRequest("GET", s.URL+"/api/groups/"+group+"/agent-runs", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r, e := s.Client().Do(req)
		if e != nil {
			return false
		}
		defer r.Body.Close()
		var list []map[string]any
		if json.NewDecoder(r.Body).Decode(&list) != nil || len(list) == 0 {
			return false
		}
		if list[0]["status"] == "finished" {
			runID = list[0]["id"].(string)
			return true
		}
		return false
	})
	_, detail, _ := call(t, s, "GET", "/api/agent-runs/"+runID, token, "")
	if detail["summary"] != "done" || len(detail["steps"].([]any)) != 5 {
		t.Fatalf("run %v", detail)
	}
	mu.Lock()
	auditCount := audits
	mu.Unlock()
	g.mu.Lock()
	sendCount := g.sendCount
	g.mu.Unlock()
	if auditCount != 1 || sendCount != 1 {
		t.Fatalf("duplicate key repeated effect/audit: %d/%d", sendCount, auditCount)
	}
}
