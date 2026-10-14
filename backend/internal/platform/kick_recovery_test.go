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

func TestHardKillCompletedKickThenRejoinReplaysDurableGatewayResult(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("policyDisabled=%v", disabled), func(t *testing.T) { hardKillKickThenRejoin(t, disabled) })
	}
}

func hardKillKickThenRejoin(t *testing.T, disabled bool) {
	g := gatewayFixture(t)
	base := g.server.Config.Handler
	entered := make(chan struct{}, 1)
	var audits atomic.Int32
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/agent/audit" {
			audits.Add(1)
			fmt.Fprint(w, `{"verdict":"pass"}`)
			return
		}
		var in domain.TurnRequest
		json.NewDecoder(r.Body).Decode(&in)
		if len(in.Messages) == 1 {
			fmt.Fprint(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"kick-once","name":"kick_user","input":{"platform_user_id":"outside","reason":"test"}}]}`)
		} else if disabled && len(in.Messages) == 3 {
			fmt.Fprint(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"new-kick","name":"kick_user","input":{"platform_user_id":"outside","reason":"new step must follow current policy"}}]}`)
		} else {
			fmt.Fprint(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}`)
		}
	}))
	defer agent.Close()
	a, _, cfg := fixtureConfig(t, platform.Config{GatewayURL: g.server.URL, AgentURL: agent.URL})
	a.Close()
	pool, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(context.Background(), `CREATE TABLE external_kick_receipts(key text PRIMARY KEY,actor text NOT NULL,target text NOT NULL)`); e != nil {
		t.Fatal(e)
	}
	// This HTTP opponent stores completed keys in PG. Reconstructing both its
	// server and pool must retain the result, independent of present membership.
	install := func(pool *pgxpool.Pool, block bool) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "POST" && r.URL.Path == "/groups/gateway-group/kick":
				var b map[string]string
				json.NewDecoder(r.Body).Decode(&b)
				key := b["idempotencyKey"]
				if key == "" {
					key = platform.ID()
				}
				tag, err := pool.Exec(r.Context(), `INSERT INTO external_kick_receipts VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, key, b["byAccountId"], b["targetPlatformUserId"])
				if err != nil {
					w.WriteHeader(503)
					return
				}
				if tag.RowsAffected() == 1 {
					g.mu.Lock()
					delete(g.members, b["targetPlatformUserId"])
					g.mu.Unlock()
					if block {
						entered <- struct{}{}
						<-r.Context().Done()
						return
					}
				}
				fmt.Fprint(w, `{"kicked":true}`)
			case r.Method == "POST" && r.URL.Path == "/test/rejoin":
				g.mu.Lock()
				g.members["outside"] = true
				g.mu.Unlock()
				fmt.Fprint(w, `{}`)
			case r.Method == "GET" && r.URL.Path == "/test/status":
				var effects int
				pool.QueryRow(r.Context(), `SELECT count(*) FROM external_kick_receipts`).Scan(&effects)
				g.mu.Lock()
				present := g.members["outside"]
				g.mu.Unlock()
				json.NewEncoder(w).Encode(map[string]any{"effects": effects, "present": present})
			default:
				base.ServeHTTP(w, r)
			}
		})
	}
	g.server.Config.Handler = install(pool, true)
	cmd, s := childServer(t, cfg)
	alive := true
	defer func() {
		if alive {
			cmd.Process.Kill()
			cmd.Wait()
		}
		pool.Close()
	}()
	token, _ := login(t, s, "admin")
	group := createGroup(t, s, token)
	call(t, g.server, "POST", "/test/rejoin", "", "{}")
	call(t, s, "PATCH", "/api/groups/"+group, token, `{"agentEnabled":true,"autoKickEnabled":true}`)
	g.event("message", map[string]any{"groupId": "gateway-group", "msgId": "kick-trigger", "senderPlatformUserId": "outside", "text": "trigger", "sentAt": time.Now().UTC().Format(time.RFC3339Nano)})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("kick effect not reached")
	}
	run := runList(t, s, token, group)[0]["id"].(string)
	cmd.Process.Kill()
	cmd.Wait()
	alive = false
	g.server.Close()
	pool.Close()
	pool, e = pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	opponent := httptest.NewServer(install(pool, false))
	defer opponent.Close()
	cfg.GatewayURL = opponent.URL
	call(t, opponent, "POST", "/test/rejoin", "", "{}")
	if disabled {
		// Expose the same persisted HTTP API while workers are parked, so an
		// operator can change policy during the hard-interruption recovery window.
		idle, e := platform.New(context.Background(), cfg)
		if e != nil {
			t.Fatal(e)
		}
		control := httptest.NewServer(idle.Handler())
		st, _, _ := call(t, control, "PATCH", "/api/groups/"+group, token, `{"autoKickEnabled":false}`)
		control.Close()
		idle.Close()
		if st != 200 {
			t.Fatal("policy change", st)
		}
		time.Sleep(500 * time.Millisecond)
	} else {
		time.Sleep(14200 * time.Millisecond)
	}
	restarted, restored := childServer(t, cfg)
	defer func() { restarted.Process.Kill(); restarted.Wait() }()
	eventually(t, 4*time.Second, func() bool {
		_, detail, _ := call(t, restored, "GET", "/api/agent-runs/"+run, token, "")
		return detail["status"] == "finished"
	})
	_, status, _ := call(t, opponent, "GET", "/test/status", "", "")
	if status["effects"] != float64(1) || status["present"] != true || audits.Load() != 1 {
		t.Fatalf("rejoined target was kicked again or reaudited: status=%v audits=%d", status, audits.Load())
	}
	_, detail, _ := call(t, restored, "GET", "/api/agent-runs/"+run, token, "")
	steps := detail["steps"].([]any)
	original := steps[0].(map[string]any)
	var result struct {
		Kicked bool `json:"kicked"`
	}
	json.Unmarshal([]byte(original["resultSummary"].(string)), &result)
	if original["isError"] != false || original["auditVerdict"] != "pass" || !result.Kicked {
		t.Fatalf("completed authorized step was misreported: %v", original)
	}
	if disabled {
		fresh := steps[1].(map[string]any)
		if fresh["isError"] != true || fresh["errorCode"] != "POLICY_DENIED" {
			t.Fatalf("new step ignored disabled policy: %v", fresh)
		}
	}
}
