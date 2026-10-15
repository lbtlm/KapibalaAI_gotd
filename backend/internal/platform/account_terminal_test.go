package platform_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/migration"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
)

func TestTerminalAccountOperationsReturnCauseAndPreserveEvidence(t *testing.T) {
	for _, tc := range []struct {
		status string
		code   string
		http   int
	}{{"session_expired", "SESSION_EXPIRED", 401}, {"suspended", "ACCOUNT_SUSPENDED", 403}} {
		t.Run(tc.status, func(t *testing.T) {
			_, s, cfg := fixtureConfig(t)
			businessSQL(t, cfg, `INSERT INTO accounts(id,status,phone,last_checked_at,last_check_result,last_check_error) VALUES('terminal-input',$1,'retained input','2026-01-01T00:00:00Z','failed',$2)`, tc.status, tc.code)
			businessSQL(t, cfg, `INSERT INTO account_group_snapshots(account_id,items,last_synced_at) VALUES('terminal-input','[]','2026-01-01T00:00:00Z')`)
			token, _ := login(t, s, "admin")
			for _, request := range []struct{ path, body string }{
				{"/check", `{"connectIfNeeded":false}`},
				{"/check", `{"connectIfNeeded":true}`},
				{"/connect", `{}`},
				{"/joined-groups/sync", `{}`},
			} {
				st, b, _ := call(t, s, "POST", "/api/accounts/terminal-input"+request.path, token, request.body)
				if st != tc.http || b["error"].(map[string]any)["code"] != tc.code {
					t.Errorf("terminal cause hidden for %s %s: %d %v", request.path, request.body, st, b)
				}
			}
			st, b, _ := call(t, s, "GET", "/api/accounts/terminal-input", token, "")
			if st != 200 || b["status"] != tc.status || b["lastCheckResult"] != "failed" || b["lastCheckError"] != tc.code || b["phone"] != "retained input" {
				t.Errorf("terminal state or diagnosis overwritten: %d %v", st, b)
			}
			st, b, _ = call(t, s, "POST", "/api/accounts/terminal-input/transition", token, `{"expectedFrom":"`+tc.status+`","to":"online"}`)
			if st != 409 || b["error"].(map[string]any)["code"] != "ILLEGAL_TRANSITION" {
				t.Errorf("manual transition changed its state-machine contract: %d %v", st, b)
			}
			st, b, _ = call(t, s, "GET", "/api/accounts/terminal-input/joined-groups", token, "")
			if st != 200 || b["lastSyncedAt"] != "2026-01-01T00:00:00Z" || len(b["items"].([]any)) != 0 {
				t.Errorf("rejected operation changed the complete cache: %d %v", st, b)
			}
		})
	}
}

func TestPersistedTerminalSignalsKeepFirstCauseAndDoNotBlockFollowingEvents(t *testing.T) {
	for _, first := range []string{"session_expired", "suspended"} {
		t.Run(first, func(t *testing.T) {
			a, s, cfg := fixtureConfig(t)
			insertAccountRows(t, cfg, "first-terminal-input", "following-input")
			token, _ := login(t, s, "admin")
			st, b, _ := call(t, s, "POST", "/api/accounts/first-terminal-input/transition", token, `{"expectedFrom":"idle","to":"`+first+`"}`)
			if st != 200 {
				t.Fatal(st, b)
			}
			other := "suspended"
			if first == "suspended" {
				other = "session_expired"
			}
			// These are already-persisted protocol inputs, not a gateway/SSE service
			// or a claim that Telegram emitted an event or accepted an RPC.
			businessSQL(t, cfg, `INSERT INTO gateway_inbox(event_id,type,data,received_at) VALUES(1,'account_status',jsonb_build_object('accountId','first-terminal-input','status',$1::text),now()-interval '2 seconds'),(2,'account_status',jsonb_build_object('accountId','first-terminal-input','status',$2::text),now()-interval '2 seconds'),(3,'account_status','{"accountId":"following-input","status":"session_expired"}',now()-interval '2 seconds')`, first, other)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if e := a.Start(ctx); e != nil {
				t.Fatal(e)
			}
			deadline := time.Now().Add(3 * time.Second)
			for accountStatus(t, s, token, "following-input") != "session_expired" && time.Now().Before(deadline) {
				time.Sleep(30 * time.Millisecond)
			}
			if got := accountStatus(t, s, token, "following-input"); got != "session_expired" {
				t.Fatalf("conflicting terminal input blocked the following event: status=%s", got)
			}
			if got := accountStatus(t, s, token, "first-terminal-input"); got != first {
				t.Fatalf("later signal replaced the first terminal cause: %s", got)
			}
			c, _, e := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(s.URL, "http")+"/ws", nil)
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			c.SetReadDeadline(time.Now().Add(3 * time.Second))
			c.WriteJSON(map[string]any{"type": "auth", "accessToken": token, "sinceSeq": 0})
			var frame map[string]any
			if e = c.ReadJSON(&frame); e != nil || frame["success"] != true {
				t.Fatal("WS authentication", e, frame)
			}
			for i := 1; i <= 4; i++ {
				if e = c.ReadJSON(&frame); e != nil {
					t.Fatal(e)
				}
				payload := frame["payload"].(map[string]any)
				id, expected := "first-terminal-input", first
				if i > 2 {
					id, expected = "following-input", "session_expired"
				}
				if frame["seq"] != float64(i) || payload["accountId"] != id || accountStatus(t, s, token, id) != expected || (frame["type"] != "account_terminal" && frame["type"] != "account_status_changed") {
					t.Fatalf("WS event does not match committed terminal state: %v", frame)
				}
			}
		})
	}
}

func TestLateMessageFailurePreservesTerminalAndInboxProgress(t *testing.T) {
	for _, first := range []string{"session_expired", "suspended"} {
		t.Run(first, func(t *testing.T) {
			a, s, cfg := fixtureConfig(t)
			group := insertBusinessGroup(t, cfg)
			// Persisted business inputs exercise backend message reduction only;
			// they do not provide a Session or a successful Telegram RPC.
			businessSQL(t, cfg, `INSERT INTO messages(group_id,client_msg_id,account_id,sender_platform_user_id,is_own,text,delivery_status) VALUES($1,'late-failure-input','account-2','database-input-b',true,'accepted business input','accepted'),($1,'terminal-queued-input','account-2','database-input-b',true,'queued business input','queued')`, group)
			token, _ := login(t, s, "admin")
			st, b, _ := call(t, s, "POST", "/api/accounts/account-2/transition", token, `{"expectedFrom":"online","to":"`+first+`"}`)
			if st != 200 {
				t.Fatal(st, b)
			}
			businessSQL(t, cfg, `INSERT INTO gateway_inbox(event_id,type,data,received_at) VALUES(1,'message_failed','{"clientMsgId":"late-failure-input","code":"ACCOUNT_SUSPENDED"}',now()-interval '2 seconds'),(2,'account_status','{"accountId":"account-3","status":"session_expired"}',now()-interval '2 seconds')`)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if e := a.Start(ctx); e != nil {
				t.Fatal(e)
			}
			deadline := time.Now().Add(3 * time.Second)
			for accountStatus(t, s, token, "account-3") != "session_expired" && time.Now().Before(deadline) {
				time.Sleep(30 * time.Millisecond)
			}
			if got := accountStatus(t, s, token, "account-3"); got != "session_expired" {
				t.Fatalf("late message failure blocked the following event: status=%s", got)
			}
			if got := accountStatus(t, s, token, "account-2"); got != first {
				t.Fatalf("late message failure replaced committed terminal state: %s", got)
			}
			st, b, _ = call(t, s, "GET", "/api/groups/"+group, token, "")
			if st != 200 || len(b["members"].([]any)) != 1 || b["members"].([]any)[0].(map[string]any)["accountId"] != "account-1" {
				t.Fatalf("terminal membership consequence changed: %d %v", st, b)
			}
			st, b, _ = call(t, s, "GET", "/api/groups/"+group+"/messages", token, "")
			if st != 200 || len(b["items"].([]any)) != 2 {
				t.Fatal(st, b)
			}
			for _, raw := range b["items"].([]any) {
				item := raw.(map[string]any)
				status, code := "failed", "ACCOUNT_SUSPENDED"
				if item["clientMsgId"] == "terminal-queued-input" {
					status, code = "cancelled", "ACCOUNT_TERMINAL"
				}
				if item["deliveryStatus"] != status || item["failCode"] != code {
					t.Fatalf("message result or committed cancellation changed: %v", item)
				}
			}
			c, _, e := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(s.URL, "http")+"/ws", nil)
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			c.SetReadDeadline(time.Now().Add(3 * time.Second))
			c.WriteJSON(map[string]any{"type": "auth", "accessToken": token, "sinceSeq": 0})
			var frame map[string]any
			if e = c.ReadJSON(&frame); e != nil || frame["success"] != true {
				t.Fatal("WS authentication", e, frame)
			}
			for i := 1; i <= 4; i++ {
				if e = c.ReadJSON(&frame); e != nil {
					t.Fatal(e)
				}
				id, expected := "account-2", first
				if i > 2 {
					id, expected = "account-3", "session_expired"
				}
				payload := frame["payload"].(map[string]any)
				if frame["seq"] != float64(i) || payload["accountId"] != id || (payload["status"] != expected && payload["to"] != expected) {
					t.Fatalf("WS replay diverged from committed terminal state: %v", frame)
				}
			}
		})
	}
}

func TestTerminalDiagnosisMigrationRepairsOnlyOverwrittenFailures(t *testing.T) {
	a, _, cfg := fixtureConfig(t)
	a.Close()
	// Version 8 changes data only; these rows are explicit version-7 inputs.
	rewindBusinessSchema(t, cfg, 7)
	cases := []struct{ id, status, result, previous, expected string }{
		{"expired-illegal", "session_expired", "failed", "ILLEGAL_TRANSITION", "SESSION_EXPIRED"},
		{"expired-offline", "session_expired", "failed", "ACCOUNT_OFFLINE", "SESSION_EXPIRED"},
		{"suspended-illegal", "suspended", "failed", "ILLEGAL_TRANSITION", "ACCOUNT_SUSPENDED"},
		{"suspended-offline", "suspended", "failed", "ACCOUNT_OFFLINE", "ACCOUNT_SUSPENDED"},
		{"correct-cause", "session_expired", "failed", "SESSION_EXPIRED", "SESSION_EXPIRED"},
		{"other-cause", "session_expired", "failed", "NETWORK_TIMEOUT", "NETWORK_TIMEOUT"},
		{"not-terminal", "idle", "failed", "ILLEGAL_TRANSITION", "ILLEGAL_TRANSITION"},
		{"not-failed", "suspended", "success", "ILLEGAL_TRANSITION", "ILLEGAL_TRANSITION"},
	}
	for _, tc := range cases {
		businessSQL(t, cfg, `INSERT INTO accounts(id,status,phone,last_checked_at,last_check_result,last_check_error) VALUES($1,$2,'retained profile','2026-01-01T00:00:00Z',$3,$4)`, tc.id, tc.status, tc.result, tc.previous)
		businessSQL(t, cfg, `INSERT INTO account_logs(account_id,actor,action,result,error_code,created_at) VALUES($1,'historical input','check','failed',$2,'2026-01-01T00:00:00Z')`, tc.id, tc.previous)
	}
	if e := migration.Apply(cfg.DatabaseURL); e != nil {
		t.Fatal(e)
	}
	reopened, e := platform.New(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	s := httptest.NewServer(reopened.Handler())
	defer s.Close()
	token, _ := login(t, s, "admin")
	for _, tc := range cases {
		st, b, _ := call(t, s, "GET", "/api/accounts/"+tc.id, token, "")
		if st != 200 || b["status"] != tc.status || b["lastCheckError"] != tc.expected || b["lastCheckResult"] != tc.result || b["lastCheckedAt"] != "2026-01-01T00:00:00Z" || b["phone"] != "retained profile" {
			t.Errorf("diagnosis migration changed the wrong fields for %s: %d %v", tc.id, st, b)
		}
		st, b, _ = call(t, s, "GET", "/api/accounts/"+tc.id+"/logs", token, "")
		if st != 200 || len(b["items"].([]any)) != 1 || b["items"].([]any)[0].(map[string]any)["errorCode"] != tc.previous {
			t.Errorf("historical operation log rewritten for %s: %d %v", tc.id, st, b)
		}
	}
}

func TestLateOfflineCheckCannotReplaceCommittedTerminalCause(t *testing.T) {
	_, s, cfg := fixtureConfig(t)
	insertAccountRows(t, cfg, "late-check-input")
	token, _ := login(t, s, "admin")
	pool, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	tx, e := pool.Begin(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	// The request can read the previous idle row, but its failure checkpoint must
	// wait for this real PostgreSQL transaction to publish the terminal input.
	if _, e = tx.Exec(context.Background(), `UPDATE accounts SET status='session_expired',last_checked_at='2026-01-01T00:00:00Z',last_check_result='failed',last_check_error='SESSION_EXPIRED' WHERE id='late-check-input'`); e != nil {
		t.Fatal(e)
	}
	type outcome struct {
		status int
		body   map[string]any
	}
	result := make(chan outcome, 1)
	go func() {
		st, b, _ := call(t, s, "POST", "/api/accounts/late-check-input/check", token, `{"connectIfNeeded":false}`)
		result <- outcome{st, b}
	}()
	eventually(t, 2*time.Second, func() bool {
		var blocked bool
		err := pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, tx.Conn().PgConn().PID()).Scan(&blocked)
		return err == nil && blocked
	})
	if e = tx.Commit(context.Background()); e != nil {
		t.Fatal(e)
	}
	select {
	case got := <-result:
		if got.status != 409 || got.body["error"].(map[string]any)["code"] != "ACCOUNT_OFFLINE" {
			t.Fatalf("request did not observe its original idle input: %d %v", got.status, got.body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("failure checkpoint did not complete after the terminal input committed")
	}
	st, b, _ := call(t, s, "GET", "/api/accounts/late-check-input", token, "")
	if st != 200 || b["status"] != "session_expired" || b["lastCheckError"] != "SESSION_EXPIRED" || b["lastCheckedAt"] != "2026-01-01T00:00:00Z" {
		t.Fatalf("late local failure replaced committed terminal diagnosis: %d %v", st, b)
	}
}
