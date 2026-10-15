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
	"testing"
	"time"
)

func TestPauseMarketingKeepsGroupAndCancelsOnlyAutomation(t *testing.T) {
	_, server, cfg := fixtureConfig(t)
	group := insertAgentInput(t, cfg)
	businessSQL(t, cfg, `INSERT INTO sequences(id,name,steps) VALUES('pause-script','Script','[{"index":1,"accountRole":"member","text":"hello","delaySeconds":0}]'); INSERT INTO sequence_runs(id,group_id,sequence_id) VALUES('pause-run','database-group','pause-script'); INSERT INTO messages(group_id,client_msg_id,account_id,sender_platform_user_id,is_own,text,delivery_status) VALUES('database-group','auto-q','account-3','database-input-c',true,'auto','queued'),('database-group','manual-q','account-3','database-input-c',true,'manual','queued'); INSERT INTO sequence_steps(run_id,index,account_role,text,delay_seconds,client_msg_id,resolved_vars,var_sources) VALUES('pause-run',1,'member','hello',0,'auto-q','{}','{}')`)
	token, _ := login(t, server, "admin")
	viewer, _ := login(t, server, "viewer")
	if status, _, _ := call(t, server, "POST", "/api/groups/"+group+"/marketing/pause", viewer, ""); status != 403 {
		t.Fatalf("viewer pause %d", status)
	}
	status, g, _ := call(t, server, "POST", "/api/groups/"+group+"/marketing/pause", token, "")
	if status != 200 || g["marketingPaused"] != true || g["status"] != "active" || g["agentEnabled"] != true || len(g["members"].([]any)) != 3 {
		t.Fatalf("pause lost group: %d %v", status, g)
	}
	pool, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	var run, auto, manual string
	var pending, jobs int
	e = pool.QueryRow(context.Background(), `SELECT (SELECT status FROM sequence_runs WHERE id='pause-run'),(SELECT delivery_status FROM messages WHERE client_msg_id='auto-q'),(SELECT delivery_status FROM messages WHERE client_msg_id='manual-q'),(SELECT count(*) FROM agent_pending),(SELECT count(*) FROM jobs WHERE kind='leave')`).Scan(&run, &auto, &manual, &pending, &jobs)
	if e != nil || run != "cancelled" || auto != "cancelled" || manual != "queued" || pending != 0 || jobs != 0 {
		t.Fatalf("pause effects %q %q %q %d %d %v", run, auto, manual, pending, jobs, e)
	}
	if status, _, _ := call(t, server, "POST", "/api/groups/"+group+"/sequence-runs", token, `{"sequenceId":"pause-script","vars":{},"stepVars":{},"memberAccountIds":["account-3"]}`); status != 409 {
		t.Fatalf("script started while paused: %d", status)
	}
	status, g, _ = call(t, server, "POST", "/api/groups/"+group+"/marketing/resume", token, "")
	if status != 200 || g["marketingPaused"] != false || g["agentEnabled"] != true {
		t.Fatalf("resume %d %v", status, g)
	}
	status, _, _ = call(t, server, "POST", "/api/groups/"+group+"/sequence-runs", token, `{"sequenceId":"pause-script","vars":{},"stepVars":{},"memberAccountIds":["account-3"]}`)
	if status != 201 {
		t.Fatalf("same script cannot restart: %d", status)
	}
}

func TestGroupAgentSessionReusesPersistedContextAcrossRuns(t *testing.T) {
	turns := make(chan domain.TurnRequest, 3)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var turn domain.TurnRequest
		json.NewDecoder(r.Body).Decode(&turn)
		turns <- turn
		fmt.Fprint(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"活动定在周六"}]}`)
	}))
	defer model.Close()
	app, server, cfg := fixtureConfig(t, platform.Config{AgentURL: model.URL})
	group := insertAgentInput(t, cfg)
	if e := app.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	token, _ := login(t, server, "admin")
	var first domain.TurnRequest
	select {
	case first = <-turns:
	case <-time.After(3 * time.Second):
		t.Fatal("first turn did not start")
	}
	eventually(t, 3*time.Second, func() bool {
		runs := runList(t, server, token, group)
		return len(runs) == 1 && runs[0]["status"] == "finished"
	})
	app.Close()
	restarted, e := platform.New(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer restarted.Close()
	if e = restarted.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	businessSQL(t, cfg, `INSERT INTO messages(group_id,msg_id,sender_platform_user_id,is_own,text) VALUES($1,'second','outside',false,'上一条说的什么时候？')`, group)
	businessSQL(t, cfg, `INSERT INTO agent_pending(group_id,msg_id) VALUES($1,'second')`, group)
	select {
	case next := <-turns:
		var one, two map[string]any
		json.Unmarshal([]byte(first.Messages[0].Content[0].Text), &one)
		json.Unmarshal([]byte(next.Messages[0].Content[0].Text), &two)
		if first.RunID == next.RunID || one["agentSessionId"] == nil || one["agentSessionId"] != two["agentSessionId"] || two["previousSummary"] != "活动定在周六" || two["conversation"] == nil {
			t.Fatalf("session context not reused: first=%v next=%v", one, two)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("second message did not run")
	}
}

func TestCancelledCampaignQueueCannotRestartAfterResume(t *testing.T) {
	app, _, cfg := fixtureConfig(t)
	insertBusinessGroup(t, cfg)
	businessSQL(t, cfg, `INSERT INTO sequences(id,name,steps) VALUES('old-script','Script','[]'); INSERT INTO sequence_runs(id,group_id,sequence_id,status) VALUES('old-run','database-group','old-script','cancelled'); INSERT INTO messages(group_id,client_msg_id,account_id,sender_platform_user_id,is_own,text,delivery_status) VALUES('database-group','late-queued','account-3','database-input-c',true,'old automation retry','queued'); INSERT INTO sequence_steps(run_id,index,account_role,text,delay_seconds,client_msg_id,resolved_vars,var_sources) VALUES('old-run',1,'member','old automation retry',0,'late-queued','{}','{}')`)
	pool, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	if e = app.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	eventually(t, 3*time.Second, func() bool {
		var status, code string
		e = pool.QueryRow(context.Background(), `SELECT delivery_status,COALESCE(fail_code,'') FROM messages WHERE client_msg_id='late-queued'`).Scan(&status, &code)
		return e == nil && status == "cancelled" && code == "MARKETING_STOPPED"
	})
}
