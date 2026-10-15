package platform_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
)

func TestEndedConnectionSignalReleasesIdleDeleteIntent(t *testing.T) {
	gatewayURL := os.Getenv("TEST_ACCOUNT_DELETE_GATEWAY_URL")
	if gatewayURL == "" {
		t.Skip("actual isolated empty Telegram-mode TEST_ACCOUNT_DELETE_GATEWAY_URL required")
	}
	app, server, cfg := fixtureConfig(t, platform.Config{GatewayURL: gatewayURL})
	token, _ := login(t, server, "admin")
	ids := []string{}
	for _, status := range []string{"idle", "disconnected"} {
		for _, signal := range []string{"matched", "stale"} {
			id := "delete_test_" + platform.ID()
			ids = append(ids, id)
			businessSQL(t, cfg, `INSERT INTO accounts(id,status,connection_id) VALUES($1,$2,'current-intent')`, id, status)
			if st, body, _ := call(t, server, "DELETE", "/api/accounts/"+id, token, ""); st != 409 || body["error"].(map[string]any)["reason"] != "active_connection" {
				t.Fatal("unknown connection intent did not block deletion", st, body)
			}
			connectionID := "current-intent"
			if signal == "stale" {
				connectionID = "older-intent"
			}
			businessSQL(t, cfg, `INSERT INTO gateway_inbox(event_id,type,data,received_at) VALUES($1,'account_status',$2,now()-interval '2 seconds')`, len(ids), map[string]any{"accountId": id, "status": "offline", "connectionId": connectionID})
		}
	}
	pool, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if e = app.Start(ctx); e != nil {
		t.Fatal(e)
	}
	eventually(t, 3*time.Second, func() bool {
		var processed bool
		return pool.QueryRow(context.Background(), `SELECT count(*)=4 FROM gateway_inbox WHERE processed`).Scan(&processed) == nil && processed
	})
	for index, id := range ids {
		status := "idle"
		if index >= 2 {
			status = "disconnected"
		}
		if actual := accountStatus(t, server, token, id); actual != status {
			t.Fatalf("offline intent cleanup changed account state: %s != %s", actual, status)
		}
		st, body, _ := call(t, server, "DELETE", "/api/accounts/"+id, token, "")
		if index%2 == 0 {
			if st != 204 {
				t.Fatalf("matched ended signal left the idle intent occupied: %d %v", st, body)
			}
		} else if st != 409 || body["error"].(map[string]any)["reason"] != "active_connection" {
			t.Fatalf("stale signal cleared the current intent: %d %v", st, body)
		}
	}
}

func TestAccountDeleteAndLeaveAdmissionSerializeAgainstRealEmptyGateway(t *testing.T) {
	gatewayURL := os.Getenv("TEST_ACCOUNT_DELETE_GATEWAY_URL")
	if gatewayURL == "" {
		t.Skip("actual isolated empty Telegram-mode TEST_ACCOUNT_DELETE_GATEWAY_URL required")
	}
	for _, scenario := range []string{"delete_wins_creator", "delete_wins_member", "leave_wins"} {
		t.Run(scenario, func(t *testing.T) {
			_, server, cfg := fixtureConfig(t, platform.Config{GatewayURL: gatewayURL})
			id, group := "delete_test_"+platform.ID(), "history_"+platform.ID()
			businessSQL(t, cfg, `INSERT INTO accounts(id,status) VALUES($1,'idle')`, id)
			businessSQL(t, cfg, `INSERT INTO groups(id,status,creator_account_id) VALUES($1,'left',$2)`, group, id)
			if scenario == "delete_wins_member" {
				businessSQL(t, cfg, `UPDATE groups SET creator_account_id=NULL WHERE id=$1`, group)
				businessSQL(t, cfg, `INSERT INTO members(group_id,platform_user_id,account_id,role) VALUES($1,'historical-member',$2,'member')`, group, id)
			}
			pool, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			gate, e := pool.Acquire(context.Background())
			if e != nil {
				t.Fatal(e)
			}
			key := int64(gate.Conn().PgConn().PID())
			defer func() {
				gate.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, key)
				gate.Release()
			}()
			if _, e = gate.Exec(context.Background(), `SELECT pg_advisory_lock($1)`, key); e != nil {
				t.Fatal(e)
			}
			triggerTarget := "BEFORE DELETE ON accounts"
			if scenario == "leave_wins" {
				triggerTarget = "BEFORE INSERT ON jobs"
			}
			businessSQL(t, cfg, fmt.Sprintf(`CREATE FUNCTION hold_admission() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(%d);IF TG_OP='DELETE' THEN RETURN OLD;END IF;RETURN NEW;END $$;CREATE TRIGGER hold_admission %s FOR EACH ROW EXECUTE FUNCTION hold_admission()`, key, triggerTarget))
			token, _ := login(t, server, "admin")
			type outcome struct {
				status int
				body   map[string]any
			}
			deleteResult, leaveResult := make(chan outcome, 1), make(chan outcome, 1)
			startDelete := func() {
				go func() {
					status, body, _ := call(t, server, "DELETE", "/api/accounts/"+id, token, "")
					deleteResult <- outcome{status, body}
				}()
			}
			startLeave := func() {
				go func() {
					status, body, _ := call(t, server, "POST", "/api/groups/"+group+"/leave-all", token, "")
					leaveResult <- outcome{status, body}
				}()
			}
			first, second := deleteResult, leaveResult
			if scenario == "leave_wins" {
				first, second = leaveResult, deleteResult
				startLeave()
			} else {
				startDelete()
			}
			var firstPID int
			eventually(t, 3*time.Second, func() bool {
				return pool.QueryRow(context.Background(), `SELECT pid FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid))`, key).Scan(&firstPID) == nil
			})
			if scenario == "leave_wins" {
				startDelete()
			} else {
				startLeave()
			}
			// Wait until the competing HTTP request either returns (the original
			// race) or is actually blocked by the first transaction's row lock.
			var secondOutcome *outcome
			eventually(t, 3*time.Second, func() bool {
				select {
				case got := <-second:
					secondOutcome = &got
					return true
				default:
					var blocked bool
					e := pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, firstPID).Scan(&blocked)
					return e == nil && blocked
				}
			})
			if _, e = gate.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, key); e != nil {
				t.Fatal(e)
			}
			await := func(result <-chan outcome) outcome {
				select {
				case got := <-result:
					return got
				case <-time.After(3 * time.Second):
					t.Fatal("HTTP admission did not complete after the PostgreSQL gate released")
					return outcome{}
				}
			}
			firstOutcome := await(first)
			if secondOutcome == nil {
				got := await(second)
				secondOutcome = &got
			}
			deleted, admitted := firstOutcome, *secondOutcome
			if scenario == "leave_wins" {
				deleted, admitted = *secondOutcome, firstOutcome
			}
			if admitted.status != 202 {
				t.Fatalf("leave-all changed its accepted-job contract: %d %v", admitted.status, admitted.body)
			}
			jobID := admitted.body["jobId"].(string)
			if status, body, _ := call(t, server, "GET", "/api/jobs/"+jobID, token, ""); status != 200 || body["status"] != "running" {
				t.Fatalf("admitted job unavailable: %d %v", status, body)
			}
			if scenario == "leave_wins" {
				// Leave admission marks the group ending in the same transaction;
				// managed-group ownership is checked before active jobs on delete.
				var groupStatus string
				if e = pool.QueryRow(context.Background(), `SELECT status FROM groups WHERE id=$1`, group).Scan(&groupStatus); e != nil || groupStatus != "ending" {
					t.Fatalf("admitted leave did not retain an ending group: status=%s error=%v", groupStatus, e)
				}
				if deleted.status != 409 || deleted.body["error"].(map[string]any)["code"] != "ACCOUNT_IN_USE" || deleted.body["error"].(map[string]any)["reason"] != "managed_group" {
					t.Fatalf("DELETE passed an admitted leave task: %d %v", deleted.status, deleted.body)
				}
			} else if deleted.status != 204 {
				t.Fatalf("winning DELETE failed: %d %v", deleted.status, deleted.body)
			}
			var dangling bool
			if e = pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM jobs WHERE status='running' AND (state->>'creator'=$1 OR (state->'members') ? $1)) AND NOT EXISTS(SELECT 1 FROM accounts WHERE id=$1)`, id).Scan(&dangling); e != nil || dangling {
				t.Fatalf("leave admission retained a deleted account: dangling=%v error=%v", dangling, e)
			}
		})
	}
}

func TestAccountDeleteAgainstRealEmptyGatewayWithRecoveryAndConcurrency(t *testing.T) {
	gatewayURL := os.Getenv("TEST_ACCOUNT_DELETE_GATEWAY_URL")
	if gatewayURL == "" {
		t.Skip("actual isolated empty Telegram-mode TEST_ACCOUNT_DELETE_GATEWAY_URL required")
	}
	for _, scenario := range []string{"retained_history", "post_gateway_failure_retry", "concurrent_deletes"} {
		t.Run(scenario, func(t *testing.T) {
			_, s, cfg := fixtureConfig(t, platform.Config{GatewayURL: gatewayURL})
			id := "delete_test_" + platform.ID()
			group := "history_" + platform.ID()
			// These are historical business inputs. The actual empty Gateway has
			// no account or credentials under this unique ID and performs no RPC.
			businessSQL(t, cfg, `INSERT INTO accounts(id,status,phone) VALUES($1,'idle','retained input')`, id)
			businessSQL(t, cfg, `INSERT INTO groups(id,status,creator_account_id) VALUES($1,'left',$2)`, group, id)
			businessSQL(t, cfg, `INSERT INTO members(group_id,platform_user_id,account_id,role) VALUES($1,'historical-sender',$2,'creator')`, group, id)
			businessSQL(t, cfg, `INSERT INTO messages(group_id,client_msg_id,account_id,sender_platform_user_id,is_own,text,delivery_status,fail_code) VALUES($1,$2,$3,'historical-sender',true,'historical database input','failed','NETWORK_TIMEOUT')`, group, "message_"+id, id)
			businessSQL(t, cfg, `INSERT INTO account_imports(session_fingerprint,config_fingerprint,account_id,completed) VALUES($1,'historical-config-input',$1,true)`, id)
			businessSQL(t, cfg, `INSERT INTO gateway_actions(id,account_id,action,status) VALUES($1,$1,'disconnect','done')`, id)
			businessSQL(t, cfg, `INSERT INTO account_group_snapshots(account_id,items) VALUES($1,'[]')`, id)
			businessSQL(t, cfg, `INSERT INTO account_logs(account_id,actor,action,result,created_at) VALUES($1,'historical actor','historical_input','success','2026-01-01T00:00:00Z')`, id)
			pool, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			token, _ := login(t, s, "admin")
			if scenario == "post_gateway_failure_retry" {
				businessSQL(t, cfg, `CREATE FUNCTION reject_account_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'deletion transaction input'; END $$;CREATE TRIGGER reject_delete BEFORE DELETE ON accounts FOR EACH ROW EXECUTE FUNCTION reject_account_delete()`)
				st, b, _ := call(t, s, "DELETE", "/api/accounts/"+id, token, "")
				if st != 503 || b["error"].(map[string]any)["code"] != "ACCOUNT_DELETE_INCOMPLETE" {
					t.Fatalf("confirmed Gateway cleanup failure was concealed: %d %v", st, b)
				}
				st, b, _ = call(t, s, "GET", "/api/accounts/"+id, token, "")
				if st != 200 || b["phone"] != "retained input" {
					t.Fatal("failed commit removed local account", st, b)
				}
				var intact bool
				if e = pool.QueryRow(context.Background(), `SELECT (SELECT account_id=$1 FROM messages WHERE client_msg_id=$2) AND (SELECT creator_account_id=$1 FROM groups WHERE id=$3) AND EXISTS(SELECT 1 FROM account_group_snapshots WHERE account_id=$1) AND EXISTS(SELECT 1 FROM account_imports WHERE account_id=$1) AND NOT EXISTS(SELECT 1 FROM account_logs WHERE account_id=$1 AND action='delete' AND result='success')`, id, "message_"+id, group).Scan(&intact); e != nil || !intact {
					t.Fatal("cleanup transaction partially committed", e, intact)
				}
				businessSQL(t, cfg, `DROP TRIGGER reject_delete ON accounts;DROP FUNCTION reject_account_delete()`)
			}
			if scenario == "concurrent_deletes" {
				results := make(chan int, 2)
				for i := 0; i < 2; i++ {
					go func() {
						st, _, _ := call(t, s, "DELETE", "/api/accounts/"+id, token, "")
						results <- st
					}()
				}
				for i := 0; i < 2; i++ {
					if st := <-results; st != 204 {
						t.Fatalf("concurrent deletion did not converge: %d", st)
					}
				}
			} else if st, b, _ := call(t, s, "DELETE", "/api/accounts/"+id, token, ""); st != 204 {
				t.Fatalf("actual empty Gateway deletion failed: %d %v", st, b)
			}
			if st, b, _ := call(t, s, "DELETE", "/api/accounts/"+id, token, ""); st != 204 {
				t.Fatalf("completed deletion is not idempotent: %d %v", st, b)
			}
			if st, b, _ := call(t, s, "GET", "/api/accounts/"+id, token, ""); st != 404 {
				t.Fatalf("deleted account remains visible: %d %v", st, b)
			}
			var preserved bool
			if e = pool.QueryRow(context.Background(), `SELECT
                NOT EXISTS(SELECT 1 FROM accounts WHERE id=$1)
                AND NOT EXISTS(SELECT 1 FROM account_imports WHERE account_id=$1)
                AND NOT EXISTS(SELECT 1 FROM account_group_snapshots WHERE account_id=$1)
                AND NOT EXISTS(SELECT 1 FROM gateway_actions WHERE account_id=$1)
                AND (SELECT creator_account_id IS NULL FROM groups WHERE id=$3)
                AND (SELECT account_id IS NULL FROM members WHERE group_id=$3 AND platform_user_id='historical-sender')
                AND (SELECT account_id IS NULL AND sender_platform_user_id='historical-sender' AND is_own AND delivery_status='failed' AND fail_code='NETWORK_TIMEOUT' FROM messages WHERE client_msg_id=$2)
                AND (SELECT count(*)=1 FROM account_logs WHERE account_id=$1 AND action='historical_input' AND actor='historical actor' AND created_at='2026-01-01T00:00:00Z')
                AND (SELECT count(*)=1 FROM account_logs WHERE account_id=$1 AND action='delete' AND actor='admin' AND result='success')`, id, "message_"+id, group).Scan(&preserved); e != nil || !preserved {
				t.Fatal("deletion lost history, audit or left account dependencies", e, preserved)
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
			if e = c.ReadJSON(&frame); e != nil || frame["type"] != "account_deleted" || frame["payload"].(map[string]any)["accountId"] != id || frame["seq"] != float64(1) {
				t.Fatal("deletion WS did not match committed cleanup", e, frame)
			}
		})
	}
}
