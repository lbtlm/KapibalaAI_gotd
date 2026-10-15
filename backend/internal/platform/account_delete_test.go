package platform_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/migration"
)

func TestLeaveAdmissionRevalidatesReferencesAfterAccountLock(t *testing.T) {
	for _, scenario := range []string{"changed_creator", "changed_member", "status_only"} {
		t.Run(scenario, func(t *testing.T) {
			_, server, cfg := fixtureConfig(t)
			insertAccountRows(t, cfg, "locked-input", "new-reference-input")
			businessSQL(t, cfg, `INSERT INTO groups(id,status,creator_account_id) VALUES('admission-group','active','locked-input')`)
			token, _ := login(t, server, "admin")
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
			if _, e = tx.Exec(context.Background(), `SELECT id FROM accounts WHERE id='locked-input' FOR UPDATE`); e != nil {
				t.Fatal(e)
			}
			type outcome struct {
				status int
				body   map[string]any
			}
			result := make(chan outcome, 1)
			go func() {
				status, body, _ := call(t, server, "POST", "/api/groups/admission-group/leave-all", token, "")
				result <- outcome{status, body}
			}()
			eventually(t, 3*time.Second, func() bool {
				var blocked bool
				e := pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, tx.Conn().PgConn().PID()).Scan(&blocked)
				return e == nil && blocked
			})
			// Updating the group while admission waits also catches a reversed
			// group-before-account lock order. No Gateway listener is present.
			if _, e = tx.Exec(context.Background(), `UPDATE groups SET status='left' WHERE id='admission-group'`); e != nil {
				t.Fatal(e)
			}
			if scenario == "changed_creator" {
				_, e = tx.Exec(context.Background(), `UPDATE groups SET creator_account_id='new-reference-input' WHERE id='admission-group'`)
			} else if scenario == "changed_member" {
				_, e = tx.Exec(context.Background(), `INSERT INTO members(group_id,platform_user_id,account_id,role) VALUES('admission-group','new-member','new-reference-input','member')`)
			}
			if e != nil {
				t.Fatal(e)
			}
			if e = tx.Commit(context.Background()); e != nil {
				t.Fatal(e)
			}
			select {
			case got := <-result:
				if scenario == "status_only" {
					if got.status != 202 {
						t.Fatalf("group status update broke admission: %d %v", got.status, got.body)
					}
				} else {
					if got.status != 409 || got.body["error"].(map[string]any)["code"] != "CAS_CONFLICT" {
						t.Fatalf("admitted a reference without its account lock: %d %v", got.status, got.body)
					}
					var jobs int
					if e = pool.QueryRow(context.Background(), `SELECT count(*) FROM jobs`).Scan(&jobs); e != nil || jobs != 0 {
						t.Fatal("rejected admission left a task", jobs, e)
					}
				}
			case <-time.After(3 * time.Second):
				t.Fatal("group/account admission did not finish after the lock released")
			}
		})
	}
}

func TestAccountDeleteRejectsOccupiedAccountsAndViewerWrites(t *testing.T) {
	_, s, cfg := fixtureConfig(t)
	ids := []string{"online-input", "limited-input", "connecting-input", "creator-input", "member-input", "message-input", "job-creator-input", "job-member-input", "effect-input", "run-input", "sequence-input"}
	insertAccountRows(t, cfg, ids...)
	businessSQL(t, cfg, `UPDATE accounts SET status='online' WHERE id='online-input'`)
	businessSQL(t, cfg, `UPDATE accounts SET status='rate_limited' WHERE id='limited-input'`)
	businessSQL(t, cfg, `UPDATE accounts SET connection_id='pending-business-intent' WHERE id='connecting-input'`)
	businessSQL(t, cfg, `INSERT INTO groups(id,creator_account_id,status) VALUES('creator-group','creator-input','active'),('member-group',NULL,'unreachable'),('history-group',NULL,'left'),('effect-group',NULL,'left'),('run-group','run-input','left')`)
	businessSQL(t, cfg, `INSERT INTO members(group_id,platform_user_id,account_id,role) VALUES('member-group','persisted-member','member-input','member')`)
	businessSQL(t, cfg, `INSERT INTO messages(group_id,client_msg_id,account_id,sender_platform_user_id,is_own,text,delivery_status) VALUES('history-group','pending-business-send','message-input','persisted-sender',true,'business input','accepted')`)
	businessSQL(t, cfg, `INSERT INTO jobs(id,kind,state) VALUES('creator-job','create','{"creator":"job-creator-input","members":[]}'),('member-job','leave','{"creator":"other-input","members":["job-member-input"]}')`)
	businessSQL(t, cfg, `INSERT INTO agent_runs(id,group_id,status,history) VALUES('effect-run','effect-group','cancelled','[]'),('active-run','run-group','running','[]')`)
	businessSQL(t, cfg, `INSERT INTO agent_effects(run_id,key,kind,state,input) VALUES('effect-run','effect-input','kick','inflight','{"byAccountId":"effect-input"}')`)
	businessSQL(t, cfg, `INSERT INTO groups(id,creator_account_id,status) VALUES('sequence-group','sequence-input','left');INSERT INTO sequences(id,name,steps) VALUES('sequence-input','business input','[]');INSERT INTO sequence_runs(id,group_id,sequence_id,status) VALUES('sequence-run','sequence-group','sequence-input','running')`)
	token, _ := login(t, s, "admin")
	viewer, _ := login(t, s, "viewer")
	st, b, _ := call(t, s, "DELETE", "/api/accounts/online-input", viewer, "")
	if st != 403 {
		t.Errorf("viewer deletion admitted: %d %v", st, b)
	}
	for _, tc := range []struct{ id, reason string }{
		{"online-input", "active_connection"}, {"limited-input", "active_connection"}, {"connecting-input", "active_connection"},
		{"creator-input", "managed_group"}, {"member-input", "managed_group"}, {"message-input", "pending_message"},
		{"job-creator-input", "active_job"}, {"job-member-input", "active_job"}, {"effect-input", "active_effect"}, {"run-input", "active_job"}, {"sequence-input", "active_job"},
	} {
		st, b, _ := call(t, s, "DELETE", "/api/accounts/"+tc.id, token, "")
		if st != 409 || b["error"].(map[string]any)["code"] != "ACCOUNT_IN_USE" || b["error"].(map[string]any)["reason"] != tc.reason {
			t.Errorf("occupied deletion did not reject %s with %s: %d %v", tc.id, tc.reason, st, b)
		}
		if st, b, _ = call(t, s, "GET", "/api/accounts/"+tc.id, token, ""); st != 200 {
			t.Errorf("rejected deletion removed the account: %d %v", st, b)
		}
	}
	for i := 0; i < 2; i++ {
		if st, b, _ = call(t, s, "DELETE", "/api/accounts/missing-input", token, ""); st != 204 {
			t.Errorf("absent deletion is not idempotent: %d %v", st, b)
		}
	}
}

func TestDeletionAuditMigrationRoundTripRetainsOrphanHistory(t *testing.T) {
	a, _, cfg := fixtureConfig(t)
	a.Close()
	businessSQL(t, cfg, `INSERT INTO account_logs(account_id,actor,action,result,created_at) VALUES('removed-history-input','historical actor','delete','success','2026-01-01T00:00:00Z')`)
	pool, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	rewindBusinessSchema(t, cfg, 8)
	if e = migration.Apply(cfg.DatabaseURL); e != nil {
		t.Fatal(e)
	}
	var retained bool
	if e = pool.QueryRow(context.Background(), `SELECT actor='historical actor' AND action='delete' AND result='success' AND created_at='2026-01-01T00:00:00Z' AND NOT EXISTS(SELECT 1 FROM accounts WHERE id='removed-history-input') FROM account_logs WHERE account_id='removed-history-input'`).Scan(&retained); e != nil || !retained {
		t.Fatal("migration changed deletion audit or recreated the account", e, retained)
	}
}

func TestAccountDeleteGatewayOutagePreservesEligibleAccountData(t *testing.T) {
	_, s, cfg := fixtureConfig(t)
	token, _ := login(t, s, "admin")
	for _, status := range []string{"idle", "disconnected", "session_expired", "suspended"} {
		id := "outage-" + status
		businessSQL(t, cfg, `INSERT INTO accounts(id,status,phone) VALUES($1,$2,'retained profile')`, id, status)
		businessSQL(t, cfg, `INSERT INTO account_group_snapshots(account_id,items) VALUES($1,'[]')`, id)
		businessSQL(t, cfg, `INSERT INTO account_logs(account_id,actor,action,result) VALUES($1,'business input','historical_input','success')`, id)
		st, b, _ := call(t, s, "DELETE", "/api/accounts/"+id, token, "")
		if st != 504 || b["error"].(map[string]any)["code"] != "NETWORK_TIMEOUT" {
			t.Errorf("gateway outage concealed for %s: %d %v", status, st, b)
		}
		st, b, _ = call(t, s, "GET", "/api/accounts/"+id, token, "")
		if st != 200 || b["status"] != status || b["phone"] != "retained profile" {
			t.Errorf("gateway failure changed local account: %d %v", st, b)
		}
		st, b, _ = call(t, s, "GET", "/api/accounts/"+id+"/joined-groups", token, "")
		if st != 200 || b["lastSyncedAt"] == nil {
			t.Errorf("gateway failure deleted cached history: %d %v", st, b)
		}
		st, b, _ = call(t, s, "GET", "/api/accounts/"+id+"/logs?action=historical_input", token, "")
		if st != 200 || len(b["items"].([]any)) != 1 {
			t.Errorf("gateway failure deleted audit: %d %v", st, b)
		}
	}
}

func TestLateEventsAfterAccountRemovalDoNotBlockTheNewIdentity(t *testing.T) {
	a, s, cfg := fixtureConfig(t)
	insertAccountRows(t, cfg, "new-identity-input")
	// Model retained post-deletion business data, without a Gateway or Session.
	businessSQL(t, cfg, `INSERT INTO groups(id,status) VALUES('retained-group','left')`)
	businessSQL(t, cfg, `INSERT INTO messages(group_id,client_msg_id,account_id,sender_platform_user_id,is_own,text,delivery_status) VALUES('retained-group','detached-message-input',NULL,'retained-platform-input',true,'retained business input','accepted')`)
	businessSQL(t, cfg, `INSERT INTO gateway_inbox(event_id,type,data,received_at) VALUES(1,'account_status','{"accountId":"removed-identity-input","status":"session_expired"}',now()-interval '2 seconds'),(2,'account_status','{"accountId":"removed-identity-input","status":"offline","connectionId":"removed-connection-input"}',now()-interval '2 seconds'),(3,'message_failed','{"clientMsgId":"detached-message-input","code":"ACCOUNT_SUSPENDED"}',now()-interval '2 seconds'),(4,'account_status','{"accountId":"new-identity-input","status":"session_expired"}',now()-interval '2 seconds')`)
	token, _ := login(t, s, "admin")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if e := a.Start(ctx); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(3 * time.Second)
	for accountStatus(t, s, token, "new-identity-input") != "session_expired" && time.Now().Before(deadline) {
		time.Sleep(30 * time.Millisecond)
	}
	if got := accountStatus(t, s, token, "new-identity-input"); got != "session_expired" {
		t.Fatalf("removed-account history blocked the next identity: %s", got)
	}
	st, b, _ := call(t, s, "GET", "/api/accounts/removed-identity-input", token, "")
	if st != 404 {
		t.Fatalf("late signal recreated deleted identity: %d %v", st, b)
	}
	st, b, _ = call(t, s, "GET", "/api/groups/retained-group/messages", token, "")
	if st != 200 || len(b["items"].([]any)) != 1 || b["items"].([]any)[0].(map[string]any)["deliveryStatus"] != "failed" || b["items"].([]any)[0].(map[string]any)["failCode"] != "ACCOUNT_SUSPENDED" {
		t.Fatalf("detached historical message result lost: %d %v", st, b)
	}
}
