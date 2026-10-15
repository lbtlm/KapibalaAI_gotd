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

	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/domain"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
)

func TestAgentBadJSONUnknownToolThenReadAndFinish(t *testing.T) {
	var turns atomic.Int32
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in domain.TurnRequest
		if json.NewDecoder(r.Body).Decode(&in) != nil || len(in.Tools) != 4 || in.RunID == "" {
			t.Error("invalid provider turn")
		}
		switch turns.Add(1) {
		case 1:
			fmt.Fprint(w, "```json\n{}\n```")
		case 2:
			fmt.Fprint(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"unknown","name":"unknown","input":{}}]}`)
		case 3:
			fmt.Fprint(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"read","name":"get_recent_messages","input":{"limit":10}}]}`)
		default:
			fmt.Fprint(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"finish","name":"finish","input":{"summary":"done"}}]}`)
		}
	}))
	defer agent.Close()
	a, s, cfg := fixtureConfig(t, platform.Config{AgentURL: agent.URL})
	group := insertAgentInput(t, cfg)
	a.Start(context.Background())
	token, _ := login(t, s, "admin")
	eventually(t, 5*time.Second, func() bool {
		runs := runList(t, s, token, group)
		if len(runs) != 1 || runs[0]["status"] != "finished" {
			return false
		}
		steps := runs[0]["steps"].([]any)
		if turns.Load() != 4 || len(steps) != 4 || runs[0]["summary"] != "done" || steps[0].(map[string]any)["kind"] != "protocol_error" || steps[1].(map[string]any)["errorCode"] != "UNKNOWN_TOOL" {
			t.Fatalf("provider protocol state: %v", runs[0])
		}
		return true
	})
}
