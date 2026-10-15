package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/domain"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
)

func TestGroupCreationPersistsChosenAdministrator(t *testing.T) {
	_, server, cfg := fixtureConfig(t)
	insertBusinessGroup(t, cfg)
	token, _ := login(t, server, "admin")
	status, body, _ := call(t, server, "POST", "/api/groups", token, `{"title":"Workspace","creatorAccountId":"account-1","memberAccountIds":["account-2","account-3"],"adminAccountId":"account-3"}`)
	if status != 202 {
		t.Fatalf("creation: %d %v", status, body)
	}
	p, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	var admin, title string
	if e = p.QueryRow(context.Background(), `SELECT state->>'admin',state->>'title' FROM jobs WHERE id=$1`, body["jobId"]).Scan(&admin, &title); e != nil || admin != "account-3" || title != "Workspace" {
		t.Fatalf("lost selected administrator/title: %q %q %v", admin, title, e)
	}
	status, _, _ = call(t, server, "POST", "/api/groups", token, `{"creatorAccountId":"account-1","memberAccountIds":["account-2"],"adminAccountId":"account-3"}`)
	if status != 422 {
		t.Fatalf("administrator outside selected members admitted: %d", status)
	}
}

func TestAgentPinnedOfflineAdminNeverFallsBackToOwner(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/agent/audit" {
			fmt.Fprint(w, `{"verdict":"pass"}`)
			return
		}
		var turn domain.TurnRequest
		json.NewDecoder(r.Body).Decode(&turn)
		if len(turn.Messages) == 1 {
			fmt.Fprint(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"send","name":"send_message","input":{"text":"hello","idempotency_key":"one"}}]}`)
		} else {
			fmt.Fprint(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}`)
		}
	}))
	defer provider.Close()
	a, server, cfg := fixtureConfig(t, platform.Config{AgentURL: provider.URL})
	group := insertAgentInput(t, cfg)
	businessSQL(t, cfg, `UPDATE accounts SET status='disconnected' WHERE id='account-2'`)
	if e := a.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	token, _ := login(t, server, "admin")
	eventually(t, 5*time.Second, func() bool {
		runs := runList(t, server, token, group)
		if len(runs) != 1 || runs[0]["status"] == "running" {
			return false
		}
		if runs[0]["adminAccountId"] != "account-2" {
			t.Fatalf("run identity not pinned: %v", runs[0])
		}
		steps := runs[0]["steps"].([]any)
		if len(steps) < 1 || steps[0].(map[string]any)["errorCode"] != "ADMIN_UNAVAILABLE" {
			t.Fatalf("administrator silently replaced: %v", steps)
		}
		return true
	})
	p, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	var queued int
	if e = p.QueryRow(context.Background(), `SELECT count(*) FROM messages WHERE client_msg_id IS NOT NULL`).Scan(&queued); e != nil || queued != 0 {
		t.Fatalf("unexpected send intent: %d %v", queued, e)
	}
}

func TestOwnedMessagesCannotBecomeAgentTriggers(t *testing.T) {
	entered := make(chan domain.TurnRequest, 1)
	release := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var turn domain.TurnRequest
		json.NewDecoder(r.Body).Decode(&turn)
		entered <- turn
		<-release
		fmt.Fprint(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}`)
	}))
	defer provider.Close()
	defer close(release)
	a, server, cfg := fixtureConfig(t, platform.Config{AgentURL: provider.URL})
	group := insertAgentInput(t, cfg)
	// Persisted business inputs only, no account connection or Gateway substitute.
	businessSQL(t, cfg, `INSERT INTO messages(group_id,msg_id,sender_platform_user_id,is_own,text) VALUES($1,'script','database-input-c',true,'script'),($1,'agent','database-input-b',true,'agent'),($1,'manual','database-input-a',true,'manual')`, group)
	businessSQL(t, cfg, `INSERT INTO agent_pending(group_id,msg_id) VALUES($1,'script'),($1,'agent'),($1,'manual')`, group)
	if e := a.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	token, _ := login(t, server, "admin")
	select {
	case turn := <-entered:
		var payload struct {
			TriggerMessages []map[string]any `json:"triggerMessages"`
		}
		if e := json.Unmarshal([]byte(turn.Messages[0].Content[0].Text), &payload); e != nil || len(payload.TriggerMessages) != 1 || payload.TriggerMessages[0]["msgId"] != "database-input" {
			t.Fatalf("own message triggered run: %+v %v", payload, e)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("external message did not trigger")
	}
	if runs := runList(t, server, token, group); len(runs) != 1 {
		t.Fatalf("multiple active runs: %v", runs)
	}
}

func TestGroupAdministratorCannotChangeDuringRun(t *testing.T) {
	_, server, cfg := fixtureConfig(t)
	group := insertBusinessGroup(t, cfg)
	token, _ := login(t, server, "admin")
	status, body, _ := call(t, server, "PATCH", "/api/groups/"+group, token, `{"adminAccountId":"account-2"}`)
	if status != 200 || body["adminAccountId"] != "account-2" {
		t.Fatalf("pin admin: %d %v", status, body)
	}
	businessSQL(t, cfg, `INSERT INTO agent_runs(id,group_id,history) VALUES('active-group-run',$1,'[]')`, group)
	status, _, _ = call(t, server, "PATCH", "/api/groups/"+group, token, `{"adminAccountId":"account-1"}`)
	if status != 409 {
		t.Fatalf("changed actor during run: %d", status)
	}
	businessSQL(t, cfg, `UPDATE agent_runs SET status='finished' WHERE id='active-group-run'`)
	status, _, _ = call(t, server, "PATCH", "/api/groups/"+group, token, `{"adminAccountId":"account-3"}`)
	if status != 422 {
		t.Fatalf("ordinary member bound without promotion: %d", status)
	}
}

func TestEndMarketingStopsAdmissionAndKeepsInflightHistory(t *testing.T) {
	_, server, cfg := fixtureConfig(t)
	group := insertAgentInput(t, cfg)
	businessSQL(t, cfg, `INSERT INTO sequences(id,name,steps) VALUES('script','Script','[{"index":1,"accountRole":"member","text":"hello","delaySeconds":0}]'); INSERT INTO sequence_runs(id,group_id,sequence_id) VALUES('script-run','database-group','script'); INSERT INTO messages(group_id,client_msg_id,account_id,sender_platform_user_id,is_own,text,delivery_status) VALUES('database-group','queued-message','account-3','database-input-c',true,'queued','queued'),('database-group','accepted-message','account-3','database-input-c',true,'accepted','accepted')`)
	token, _ := login(t, server, "admin")
	status, first, _ := call(t, server, "POST", "/api/groups/"+group+"/leave-all", token, "")
	if status != 202 {
		t.Fatalf("end marketing: %d %v", status, first)
	}
	_, g, _ := call(t, server, "GET", "/api/groups/"+group, token, "")
	if g["status"] != "ending" || g["agentEnabled"] != false {
		t.Fatalf("new work still admitted: %v", g)
	}
	status, again, _ := call(t, server, "POST", "/api/groups/"+group+"/leave-all", token, "")
	if status != 202 || first["jobId"] != again["jobId"] {
		t.Fatalf("duplicate leave tasks: %d %v", status, again)
	}
	p, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	var sequence, queued, accepted string
	var pending int
	e = p.QueryRow(context.Background(), `SELECT (SELECT status FROM sequence_runs WHERE id='script-run'),(SELECT delivery_status FROM messages WHERE client_msg_id='queued-message'),(SELECT delivery_status FROM messages WHERE client_msg_id='accepted-message'),(SELECT count(*) FROM agent_pending)`).Scan(&sequence, &queued, &accepted, &pending)
	if e != nil || sequence != "cancelled" || queued != "cancelled" || accepted != "accepted" || pending != 0 {
		t.Fatalf("end state: %s %s %s %d %v", sequence, queued, accepted, pending, e)
	}
	status, _, _ = call(t, server, "POST", "/api/groups/"+group+"/sequence-runs", token, `{"sequenceId":"script"}`)
	if status != 409 {
		t.Fatalf("new script during end: %d", status)
	}
}

func TestScriptPreviewPinsRolesAndRunKeepsSnapshot(t *testing.T) {
	_, server, cfg := fixtureConfig(t)
	group := insertBusinessGroup(t, cfg)
	token, _ := login(t, server, "admin")
	_, definition, _ := call(t, server, "POST", "/api/sequences", token, `{"name":"Script","steps":[{"index":1,"accountRole":"admin","text":"Welcome","delaySeconds":0},{"index":2,"accountRole":"member","text":"Hello","delaySeconds":2}]}`)
	input := `{"sequenceId":"` + definition["id"].(string) + `","memberAccountIds":["account-3"]}`
	status, preview, _ := call(t, server, "POST", "/api/groups/"+group+"/sequence-runs/preview", token, input)
	if status != 200 {
		t.Fatalf("preview: %d %v", status, preview)
	}
	steps := preview["steps"].([]any)
	if steps[0].(map[string]any)["accountId"] != "account-2" || steps[1].(map[string]any)["accountId"] != "account-3" {
		t.Fatalf("roles not pinned: %v", steps)
	}
	status, run, _ := call(t, server, "POST", "/api/groups/"+group+"/sequence-runs", token, input)
	if status != 201 {
		t.Fatalf("start: %d %v", status, run)
	}
	status, _, _ = call(t, server, "PUT", "/api/sequences/"+definition["id"].(string), token, `{"name":"Edited","steps":[{"index":1,"accountRole":"member","text":"Changed","delaySeconds":0}]}`)
	if status != 200 {
		t.Fatalf("visual edit persistence: %d", status)
	}
	_, progress, _ := call(t, server, "GET", "/api/sequence-runs/"+run["runId"].(string), token, "")
	stored := progress["steps"].([]any)
	if len(stored) != 2 || stored[0].(map[string]any)["text"] != "Welcome" || stored[0].(map[string]any)["accountId"] != "account-2" {
		t.Fatalf("run snapshot changed: %v", stored)
	}
}

func TestEndMarketingDoesNotDeadlockWithGroupWorker(t *testing.T) {
	_, server, cfg := fixtureConfig(t)
	group := insertBusinessGroup(t, cfg)
	token, _ := login(t, server, "admin")
	p, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	tx, e := p.Begin(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(context.Background(), `SELECT id FROM groups WHERE id=$1 FOR UPDATE`, group); e != nil {
		t.Fatal(e)
	}
	result := make(chan int, 1)
	go func() {
		status, _, _ := call(t, server, "POST", "/api/groups/"+group+"/leave-all", token, "")
		result <- status
	}()
	select {
	case status := <-result:
		if status != 409 {
			t.Fatalf("busy group should reject bounded admission: %d", status)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("end holds account locks while waiting for group worker")
	}
	var count int
	if e = p.QueryRow(context.Background(), `SELECT count(*) FROM jobs`).Scan(&count); e != nil || count != 0 {
		t.Fatalf("failed admission left work: %d %v", count, e)
	}
}

func TestSlowGroupJobDoesNotBlockAnotherGroup(t *testing.T) {
	a, _, cfg := fixtureConfig(t)
	businessSQL(t, cfg, `INSERT INTO groups(id) VALUES('slow-group'),('fast-group');INSERT INTO jobs(id,kind,group_id,state,created_at) VALUES('slow-job','leave','slow-group','{"phase":"leave","members":[]}','2020-01-01'),('fast-job','leave','fast-group','{"phase":"leave","members":[]}','2021-01-01')`)
	p, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	tx, e := p.Begin(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(context.Background(), `SELECT id FROM jobs WHERE id='slow-job' FOR UPDATE`); e != nil {
		t.Fatal(e)
	}
	if e = a.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	eventually(t, time.Second, func() bool {
		var status string
		e := p.QueryRow(context.Background(), `SELECT status FROM jobs WHERE id='fast-job'`).Scan(&status)
		return e == nil && status == "finished"
	})
}
