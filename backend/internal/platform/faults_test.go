package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func accountStatus(t *testing.T, s *httptest.Server, token, id string) string {
	t.Helper()
	req, _ := http.NewRequest("GET", s.URL+"/api/accounts", nil)
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
	var values []map[string]any
	json.NewDecoder(r.Body).Decode(&values)
	for _, v := range values {
		if v["id"] == id {
			return v["status"].(string)
		}
	}
	return ""
}
func TestRateLimitSurvivesRepositoryReconstructionAndPreservesFIFO(t *testing.T) {
	g := gatewayFixture(t)
	a, s, cfg := fixtureConfig(t, platform.Config{GatewayURL: g.server.URL})
	a.Start(context.Background())
	token, _ := login(t, s, "admin")
	group := createGroup(t, s, token)
	g.mu.Lock()
	g.rateOnce = true
	g.mu.Unlock()
	call(t, s, "POST", "/api/groups/"+group+"/send", token, `{"accountId":"account-2","text":"first"}`)
	eventually(t, 2*time.Second, func() bool { return accountStatus(t, s, token, "account-2") == "rate_limited" })
	call(t, s, "POST", "/api/groups/"+group+"/send", token, `{"accountId":"account-2","text":"second"}`)
	a.Close()
	b, e := platform.New(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	server := httptest.NewServer(b.Handler())
	defer server.Close()
	b.Start(context.Background())
	time.Sleep(300 * time.Millisecond)
	g.mu.Lock()
	count := g.sendCount
	g.mu.Unlock()
	if count != 1 {
		t.Fatalf("sent during persisted rate limit: %d", count)
	}
	eventually(t, 5*time.Second, func() bool {
		_, v, _ := call(t, server, "GET", "/api/groups/"+group+"/messages", token, "")
		items := v["items"].([]any)
		return len(items) == 2 && items[0].(map[string]any)["deliveryStatus"] == "sent" && items[1].(map[string]any)["deliveryStatus"] == "sent"
	})
	_, v, _ := call(t, server, "GET", "/api/groups/"+group+"/messages", token, "")
	items := v["items"].([]any)
	if items[0].(map[string]any)["text"] != "second" || items[1].(map[string]any)["text"] != "first" {
		t.Fatalf("FIFO violated: %v", v)
	}
}
func TestTerminalTransitionRollsBackEveryConsequenceThenCancelsQueuedMessages(t *testing.T) {
	g := gatewayFixture(t)
	a, s, cfg := fixtureConfig(t, platform.Config{GatewayURL: g.server.URL})
	a.Start(context.Background())
	token, _ := login(t, s, "admin")
	group := createGroup(t, s, token)
	g.mu.Lock()
	g.rateOnce = true
	g.mu.Unlock()
	call(t, s, "POST", "/api/groups/"+group+"/send", token, `{"accountId":"account-2","text":"queued"}`)
	eventually(t, 2*time.Second, func() bool { return accountStatus(t, s, token, "account-2") == "rate_limited" })
	pool, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	_, e = pool.Exec(context.Background(), `CREATE FUNCTION reject_member_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected database failure'; END $$;CREATE TRIGGER reject_delete BEFORE DELETE ON members FOR EACH ROW EXECUTE FUNCTION reject_member_delete()`)
	if e != nil {
		t.Fatal(e)
	}
	st, _, _ := call(t, s, "POST", "/api/accounts/account-2/transition", token, `{"expectedFrom":"rate_limited","to":"suspended"}`)
	if st != 500 {
		t.Fatalf("injected transaction did not fail %d", st)
	}
	if accountStatus(t, s, token, "account-2") != "rate_limited" {
		t.Fatal("partial account state escaped rollback")
	}
	_, v, _ := call(t, s, "GET", "/api/groups/"+group, token, "")
	if len(v["members"].([]any)) != 3 {
		t.Fatal("partial member removal escaped rollback")
	}
	if _, e = pool.Exec(context.Background(), `DROP TRIGGER reject_delete ON members;DROP FUNCTION reject_member_delete()`); e != nil {
		t.Fatal(e)
	}
	st, _, _ = call(t, s, "POST", "/api/accounts/account-2/transition", token, `{"expectedFrom":"rate_limited","to":"suspended"}`)
	if st != 200 {
		t.Fatalf("terminal %d", st)
	}
	_, v, _ = call(t, s, "GET", "/api/groups/"+group+"/messages", token, "")
	m := v["items"].([]any)[0].(map[string]any)
	if m["deliveryStatus"] != "cancelled" || m["failCode"] != "ACCOUNT_TERMINAL" {
		t.Fatalf("queue consequence %v", m)
	}
	_, v, _ = call(t, s, "GET", "/api/groups/"+group, token, "")
	if len(v["members"].([]any)) != 2 {
		t.Fatal("terminal member not removed")
	}
}
func TestLeaveFailureContinuesOthersAndKeepsCreatorAndFailedMember(t *testing.T) {
	g := gatewayFixture(t)
	a, s := fixture(t, platform.Config{GatewayURL: g.server.URL})
	a.Start(context.Background())
	token, _ := login(t, s, "admin")
	group := createGroup(t, s, token)
	g.mu.Lock()
	g.failLeave = true
	g.mu.Unlock()
	st, b, _ := call(t, s, "POST", "/api/groups/"+group+"/leave-all", token, "{}")
	if st != 202 {
		t.Fatal(st)
	}
	job := b["jobId"].(string)
	eventually(t, 3*time.Second, func() bool { _, v, _ := call(t, s, "GET", "/api/jobs/"+job, token, ""); return v["status"] == "failed" })
	_, b, _ = call(t, s, "GET", "/api/groups/"+group, token, "")
	if len(b["members"].([]any)) != 2 {
		t.Fatalf("DB members inconsistent %v", b)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.members["p-account-1"] || !g.members["p-account-2"] || g.members["p-account-3"] {
		t.Fatalf("gateway members inconsistent %v", g.members)
	}
}
func TestInviteReadinessExpiryAlreadyMemberAndPromoteRetry(t *testing.T) {
	g := gatewayFixture(t)
	base := g.server.Config.Handler
	var joins, promotes atomic.Int32
	g.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/groups/gateway-group/join":
			n := joins.Add(1)
			if n == 1 {
				w.WriteHeader(409)
				fmt.Fprint(w, `{"code":"INVITE_NOT_READY"}`)
				return
			}
			if n == 2 {
				w.WriteHeader(410)
				fmt.Fprint(w, `{"code":"INVITE_EXPIRED"}`)
				return
			}
			if n == 3 {
				var b map[string]string
				json.NewDecoder(r.Body).Decode(&b)
				g.mu.Lock()
				g.members["p-"+b["accountId"]] = true
				g.mu.Unlock()
				w.WriteHeader(409)
				fmt.Fprint(w, `{"code":"ALREADY_MEMBER"}`)
				return
			}
		case "/groups/gateway-group/promote":
			if promotes.Add(1) == 1 {
				w.WriteHeader(409)
				fmt.Fprint(w, `{"code":"NOT_MEMBER_YET"}`)
				return
			}
		}
		base.ServeHTTP(w, r)
	})
	a, s := fixture(t, platform.Config{GatewayURL: g.server.URL})
	a.Start(context.Background())
	token, _ := login(t, s, "admin")
	group := createGroup(t, s, token)
	_, b, _ := call(t, s, "GET", "/api/groups/"+group, token, "")
	if len(b["members"].([]any)) != 3 || promotes.Load() != 2 {
		t.Fatalf("lifecycle did not converge with bounded promote: %v calls=%d", b, promotes.Load())
	}
}
func TestAuditFailsClosedAfterThreeMalformedResponses(t *testing.T) {
	var audits atomic.Int32
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/agent/audit" {
			audits.Add(1)
			fmt.Fprint(w, `{"verdict":"maybe"}`)
			return
		}
		fmt.Fprint(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"send","name":"send_message","input":{"text":"blocked","idempotency_key":"blocked-key"}}]}`)
	}))
	defer agent.Close()
	g := gatewayFixture(t)
	a, s := fixture(t, platform.Config{GatewayURL: g.server.URL, AgentURL: agent.URL})
	a.Start(context.Background())
	token, _ := login(t, s, "admin")
	group := createGroup(t, s, token)
	call(t, s, "PATCH", "/api/groups/"+group, token, `{"agentEnabled":true}`)
	g.event("message", map[string]any{"groupId": "gateway-group", "msgId": "audit-trigger", "senderPlatformUserId": "outside", "text": "trigger", "sentAt": time.Now().UTC().Format(time.RFC3339Nano)})
	eventually(t, 4*time.Second, func() bool {
		runs := runList(t, s, token, group)
		return len(runs) == 1 && runs[0]["status"] == "blocked" && runs[0]["endReason"] == "audit_blocked"
	})
	g.mu.Lock()
	sent := g.sendCount
	g.mu.Unlock()
	if audits.Load() != 3 || sent != 0 {
		t.Fatalf("audit fail-open attempts=%d sends=%d", audits.Load(), sent)
	}
}
