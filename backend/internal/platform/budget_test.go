package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/domain"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestActualSixtySecondBudgetStopsSlowLegalTurnsAndRecordsInterruptedTurn(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in domain.TurnRequest
		json.NewDecoder(r.Body).Decode(&in)
		select {
		case <-r.Context().Done():
			return
		case <-time.After(8 * time.Second):
		}
		fmt.Fprintf(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"read-%d","name":"get_recent_messages","input":{"limit":10}}]}`, len(in.Messages))
	}))
	defer agent.Close()
	g := gatewayFixture(t)
	a, s := fixture(t, platform.Config{GatewayURL: g.server.URL, AgentURL: agent.URL})
	a.Start(context.Background())
	token, _ := login(t, s, "admin")
	group := createGroup(t, s, token)
	call(t, s, "PATCH", "/api/groups/"+group, token, `{"agentEnabled":true}`)
	g.event("message", map[string]any{"groupId": "gateway-group", "msgId": "budget-trigger", "senderPlatformUserId": "outside", "text": "trigger", "sentAt": time.Now().UTC().Format(time.RFC3339Nano)})
	eventually(t, 3*time.Second, func() bool { return len(runList(t, s, token, group)) == 1 })
	created := time.Now()
	var id string
	eventually(t, 63*time.Second, func() bool {
		runs := runList(t, s, token, group)
		if len(runs) == 1 && runs[0]["status"] == "failed" && runs[0]["endReason"] == "wall_clock" {
			id = runs[0]["id"].(string)
			return true
		}
		return false
	})
	elapsed := time.Since(created)
	if elapsed < 58*time.Second || elapsed > 62*time.Second {
		t.Fatalf("wall budget elapsed=%v", elapsed)
	}
	_, detail, _ := call(t, s, "GET", "/api/agent-runs/"+id, token, "")
	steps := detail["steps"].([]any)
	last := steps[len(steps)-1].(map[string]any)
	if last["kind"] != "protocol_error" || last["errorCode"] != "TURN_TIMEOUT" || len(steps) > 12 {
		t.Fatalf("interrupted turn not visible: %v", detail)
	}
}
