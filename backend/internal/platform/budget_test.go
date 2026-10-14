package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/domain"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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

func TestHalfSecondHardDowntimeDoesNotSpendBudgetUnlessPeerRemainsOnline(t *testing.T) {
	for _, peerOnline := range []bool{false, true} {
		t.Run(fmt.Sprintf("peer=%v", peerOnline), func(t *testing.T) {
			g := gatewayFixture(t)
			entered := make(chan struct{}, 1)
			var turns atomic.Int32
			agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request domain.TurnRequest
				json.NewDecoder(r.Body).Decode(&request)
				if turns.Add(1) == 1 {
					entered <- struct{}{}
					<-r.Context().Done()
					return
				}
				delay := 20 * time.Millisecond
				if peerOnline {
					delay = time.Second
				}
				select {
				case <-r.Context().Done():
					return
				case <-time.After(delay):
				}
				fmt.Fprint(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"resumed within remaining online budget"}]}`)
			}))
			defer agent.Close()
			a, _, cfg := fixtureConfig(t, platform.Config{GatewayURL: g.server.URL, AgentURL: agent.URL})
			a.Close()
			cmd, server := childServer(t, cfg)
			alive := true
			defer func() {
				if alive {
					cmd.Process.Kill()
					cmd.Wait()
				}
			}()
			token, _ := login(t, server, "admin")
			group := createGroup(t, server, token)
			call(t, server, "PATCH", "/api/groups/"+group, token, `{"agentEnabled":true}`)
			g.event("message", map[string]any{"groupId": "gateway-group", "msgId": "near-budget", "senderPlatformUserId": "outside", "text": "trigger", "sentAt": time.Now().UTC().Format(time.RFC3339Nano)})
			select {
			case <-entered:
			case <-time.After(4 * time.Second):
				t.Fatal("turn not started")
			}
			run := runList(t, server, token, group)[0]["id"].(string)
			if peerOnline {
				peer, e := platform.New(context.Background(), cfg)
				if e != nil {
					t.Fatal(e)
				}
				defer peer.Close()
				peer.Start(context.Background())
			}
			pool, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			// Time-boundary fault setup, not a persistence assertion: move a real
			// running process to the last 400ms of its accumulated online budget.
			if _, e = pool.Exec(context.Background(), `UPDATE agent_runs SET elapsed_ms=59600,budget_checkpoint=now() WHERE id=$1`, run); e != nil {
				t.Fatal(e)
			}
			cmd.Process.Kill()
			cmd.Wait()
			alive = false
			time.Sleep(500 * time.Millisecond)
			restarted, restored := childServer(t, cfg)
			defer func() { restarted.Process.Kill(); restarted.Wait() }()
			eventually(t, 2*time.Second, func() bool {
				_, detail, _ := call(t, restored, "GET", "/api/agent-runs/"+run, token, "")
				if peerOnline {
					return detail["status"] == "failed" && detail["endReason"] == "wall_clock"
				}
				if detail["status"] == "failed" {
					t.Fatalf("offline half-second spent budget: %v", detail["endReason"])
				}
				return detail["status"] == "finished"
			})
		})
	}
}
