package platform_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
)

func TestSlowFinalTurnCancelsAfterPolicyClosesOrGroupBecomesUnreachable(t *testing.T) {
	for _, change := range []string{"disabled", "unreachable", "paused"} {
		for _, final := range []string{"end_turn", "finish"} {
			t.Run(change+"/"+final, func(t *testing.T) {
				entered, release := make(chan struct{}), make(chan struct{})
				var turns atomic.Int32
				agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					turns.Add(1)
					close(entered)
					<-release
					if final == "finish" {
						fmt.Fprint(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"finish","name":"finish","input":{"summary":"done"}}]}`)
					} else {
						fmt.Fprint(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}`)
					}
				}))
				defer agent.Close()
				a, s, cfg := fixtureConfig(t, platform.Config{AgentURL: agent.URL})
				group := insertAgentInput(t, cfg)
				a.Start(context.Background())
				token, _ := login(t, s, "admin")
				select {
				case <-entered:
				case <-time.After(4 * time.Second):
					t.Fatal("turn not reached")
				}
				if change == "disabled" {
					call(t, s, "PATCH", "/api/groups/"+group, token, `{"agentEnabled":false}`)
				} else if change == "paused" {
					if status, _, _ := call(t, s, "POST", "/api/groups/"+group+"/marketing/pause", token, ""); status != 200 {
						t.Fatalf("pause %d", status)
					}
					if status, _, _ := call(t, s, "POST", "/api/groups/"+group+"/marketing/resume", token, ""); status != 409 {
						t.Fatalf("resumed during in-flight turn %d", status)
					}
				} else {
					// Direct persisted group input tests final-turn cancellation, not a platform error.
					businessSQL(t, cfg, `UPDATE groups SET status='unreachable' WHERE id=$1`, group)
				}
				close(release)
				eventually(t, 2*time.Second, func() bool {
					runs := runList(t, s, token, group)
					if len(runs) != 1 || runs[0]["status"] == "running" {
						return false
					}
					if runs[0]["status"] != "cancelled" || runs[0]["endReason"] != "cancelled" || len(runs[0]["steps"].([]any)) != 1 || turns.Load() != 1 {
						t.Fatalf("closed group recorded final or extra turn: %v turns=%d", runs[0], turns.Load())
					}
					return true
				})
				if change == "paused" {
					if status, _, _ := call(t, s, "POST", "/api/groups/"+group+"/marketing/resume", token, ""); status != 200 {
						t.Fatalf("resume after cancellation %d", status)
					}
				}
			})
		}
	}
}
