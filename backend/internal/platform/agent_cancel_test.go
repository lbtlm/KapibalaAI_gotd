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
	for _, change := range []string{"disabled", "unreachable"} {
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
				g := gatewayFixture(t)
				base := g.server.Config.Handler
				g.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/groups/gateway-group/send" {
						w.WriteHeader(403)
						fmt.Fprint(w, `{"code":"GROUP_WRITE_FORBIDDEN"}`)
						return
					}
					base.ServeHTTP(w, r)
				})
				a, s := fixture(t, platform.Config{GatewayURL: g.server.URL, AgentURL: agent.URL})
				a.Start(context.Background())
				token, _ := login(t, s, "admin")
				group := createGroup(t, s, token)
				call(t, s, "PATCH", "/api/groups/"+group, token, `{"agentEnabled":true}`)
				g.event("message", map[string]any{"groupId": "gateway-group", "msgId": "cancel-trigger", "senderPlatformUserId": "outside", "text": "trigger", "sentAt": time.Now().UTC().Format(time.RFC3339Nano)})
				select {
				case <-entered:
				case <-time.After(4 * time.Second):
					t.Fatal("turn not reached")
				}
				if change == "disabled" {
					call(t, s, "PATCH", "/api/groups/"+group, token, `{"agentEnabled":false}`)
				} else {
					call(t, s, "POST", "/api/groups/"+group+"/send", token, `{"accountId":"account-1","text":"detect forbidden"}`)
					eventually(t, 2*time.Second, func() bool {
						_, detail, _ := call(t, s, "GET", "/api/groups/"+group, token, "")
						return detail["status"] == "unreachable"
					})
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
			})
		}
	}
}
