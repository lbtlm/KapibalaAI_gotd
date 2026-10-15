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

	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/domain"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
)

func TestAgentAllowsExactlyTwelveTurnsIncludingFinalWithoutThirteenthRequest(t *testing.T) {
	for _, last := range []string{"read", "end_turn", "finish"} {
		t.Run(last, func(t *testing.T) {
			var turns atomic.Int32
			agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := turns.Add(1)
				if n == 12 && last == "end_turn" {
					fmt.Fprint(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"twelfth final"}]}`)
				} else if n == 12 && last == "finish" {
					fmt.Fprint(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"finish-12","name":"finish","input":{"summary":"twelfth final"}}]}`)
				} else {
					fmt.Fprintf(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"read-%d","name":"get_recent_messages","input":{"limit":1}}]}`, n)
				}
			}))
			defer agent.Close()
			a, s, cfg := fixtureConfig(t, platform.Config{AgentURL: agent.URL})
			group := insertAgentInput(t, cfg)
			a.Start(context.Background())
			token, _ := login(t, s, "admin")

			eventually(t, 5*time.Second, func() bool {
				runs := runList(t, s, token, group)
				if len(runs) != 1 || runs[0]["status"] == "running" {
					return false
				}
				_, detail, _ := call(t, s, "GET", "/api/agent-runs/"+runs[0]["id"].(string), token, "")
				if turns.Load() != 12 || len(detail["steps"].([]any)) != 12 {
					t.Fatalf("incorrect turn bound: turns=%d detail=%v", turns.Load(), detail)
				}
				if last == "read" {
					if detail["status"] != "failed" || detail["endReason"] != "budget_exhausted" {
						t.Fatalf("twelfth tool did not exhaust budget: %v", detail)
					}
				} else if detail["status"] != "finished" || detail["endReason"] != "final" || detail["summary"] != "twelfth final" {
					t.Fatalf("twelfth final was rejected: %v", detail)
				}
				return true
			})
		})
	}
}

func TestDuplicateToolIDRecordsProtocolErrorWithoutRepeatingRead(t *testing.T) {
	var turns, audits atomic.Int32
	history := make(chan domain.TurnRequest, 1)
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/agent/audit" {
			audits.Add(1)
			fmt.Fprint(w, `{"verdict":"pass"}`)
			return
		}
		var req domain.TurnRequest
		json.NewDecoder(r.Body).Decode(&req)
		n := turns.Add(1)
		if n <= 2 {
			fmt.Fprintf(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"duplicate-id","name":"get_recent_messages","input":{"limit":%d}}]}`, n)
		} else {
			history <- req
			fmt.Fprint(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}`)
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
		if len(steps) != 3 {
			t.Fatalf("duplicate step not recorded: %v", steps)
		}
		step := steps[1].(map[string]any)
		if step["kind"] != "protocol_error" || step["errorCode"] != "DUPLICATE_TOOL_USE_ID" || !strings.Contains(step["rawResponse"].(string), `"limit":2`) {
			t.Fatalf("duplicate id did not become visible protocol error: %v", step)
		}
		return true
	})
	req := <-history
	if len(req.Messages) != 4 || req.Messages[3].Role != "user" || !strings.HasPrefix(req.Messages[3].Content[0].Text, "PROTOCOL_ERROR DUPLICATE_TOOL_USE_ID:") {
		t.Fatalf("duplicate assistant was appended or error not returned as user text: %v", req.Messages)
	}
	if audits.Load() != 0 || turns.Load() != 3 {
		t.Fatalf("duplicate read changed protocol/audit count: audits=%d turns=%d", audits.Load(), turns.Load())
	}
}

func TestProtocolErrorPreservesOriginalResponseBeyondTwoKilobytes(t *testing.T) {
	raw := strings.Repeat("无效协议", 600) + "END-EVIDENCE"
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, raw) }))
	defer agent.Close()
	a, s, cfg := fixtureConfig(t, platform.Config{AgentURL: agent.URL})
	group := insertAgentInput(t, cfg)
	if e := a.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	token, _ := login(t, s, "viewer")
	eventually(t, 5*time.Second, func() bool {
		runs := runList(t, s, token, group)
		if len(runs) != 1 || runs[0]["status"] == "running" {
			return false
		}
		steps := runs[0]["steps"].([]any)
		if len(steps) != 3 {
			t.Fatalf("expected three protocol failures")
		}
		for _, v := range steps {
			if v.(map[string]any)["rawResponse"] != raw {
				t.Fatal("original protocol response truncated")
			}
		}
		return true
	})
}
