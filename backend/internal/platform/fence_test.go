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

func runList(t *testing.T, s *httptest.Server, token, group string) []map[string]any {
	t.Helper()
	req, _ := http.NewRequest("GET", s.URL+"/api/groups/"+group+"/agent-runs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	client := s.Client()
	if client == nil {
		client = http.DefaultClient
	}
	r, e := client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	var list []map[string]any
	json.NewDecoder(r.Body).Decode(&list)
	return list
}
func TestLostAdvisoryConnectionCancelsOldWorkerAndReconcilesKickOnce(t *testing.T) {
	g := gatewayFixture(t)
	entered, cancelled, release := make(chan struct{}, 1), make(chan struct{}, 1), make(chan struct{})
	defer close(release)
	var kicks, audits atomic.Int32
	kickedKeys := map[string]bool{}
	base := g.server.Config.Handler
	g.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/groups/gateway-group/kick" {
			var request map[string]any
			json.NewDecoder(r.Body).Decode(&request)
			g.mu.Lock()
			key, _ := request["idempotencyKey"].(string)
			if key != "" && kickedKeys[key] {
				g.mu.Unlock()
				fmt.Fprint(w, `{"kicked":true}`)
				return
			}
			kickedKeys[key] = true
			kicks.Add(1)
			delete(g.members, "outside")
			g.mu.Unlock()
			select {
			case entered <- struct{}{}:
			default:
			}
			select {
			case <-r.Context().Done():
				select {
				case cancelled <- struct{}{}:
				default:
				}
				return
			case <-release:
				fmt.Fprint(w, `{"kicked":true}`)
				return
			}
		}
		base.ServeHTTP(w, r)
	})
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/agent/audit" {
			audits.Add(1)
			fmt.Fprint(w, `{"verdict":"pass"}`)
			return
		}
		var in domain.TurnRequest
		json.NewDecoder(r.Body).Decode(&in)
		if len(in.Messages) == 1 {
			fmt.Fprint(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"kick-1","name":"kick_user","input":{"platform_user_id":"outside","reason":"test"}}]}`)
		} else {
			fmt.Fprint(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}`)
		}
	}))
	defer agent.Close()
	a, s, cfg := fixtureConfig(t, platform.Config{GatewayURL: g.server.URL, AgentURL: agent.URL})
	a.Start(context.Background())
	token, _ := login(t, s, "admin")
	group := createGroup(t, s, token)
	g.mu.Lock()
	g.members["outside"] = true
	g.mu.Unlock()
	call(t, s, "PATCH", "/api/groups/"+group, token, `{"agentEnabled":true,"autoKickEnabled":true}`)
	g.event("message", map[string]any{"groupId": "gateway-group", "msgId": "trigger", "senderPlatformUserId": "outside", "text": "test", "sentAt": time.Now().UTC().Format(time.RFC3339Nano)})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("kick not started")
	}
	runs := runList(t, s, token, group)
	id := runs[0]["id"].(string)
	b, e := platform.New(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	b.Start(context.Background())
	pool, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	_, e = pool.Exec(context.Background(), `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=current_database() AND application_name=$1`, "kapibala-lock:agent:"+id)
	if e != nil {
		t.Fatal(e)
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("old worker HTTP context survived its database lock connection")
	}
	eventually(t, 5*time.Second, func() bool {
		runs := runList(t, s, token, group)
		return len(runs) == 1 && runs[0]["status"] == "finished"
	})
	if kicks.Load() != 1 || audits.Load() != 1 {
		t.Fatalf("effect or audit repeated %d/%d", kicks.Load(), audits.Load())
	}
}
