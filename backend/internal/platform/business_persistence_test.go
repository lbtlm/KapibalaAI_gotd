package platform_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/migration"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
)

func TestTerminalBusinessTransactionRollsBackThenCancelsQueuedRows(t *testing.T) {
	_, s, cfg := fixtureConfig(t)
	group := insertBusinessGroup(t, cfg)
	token, _ := login(t, s, "admin")
	businessSQL(t, cfg, `UPDATE accounts SET status='rate_limited',rate_limited_until=now()+interval '1 hour' WHERE id='account-2'`)
	businessSQL(t, cfg, `INSERT INTO messages(group_id,client_msg_id,account_id,sender_platform_user_id,is_own,text,delivery_status) VALUES($1,'database-queued','account-2','database-input-b',true,'queued business input','queued')`, group)
	businessSQL(t, cfg, `CREATE FUNCTION reject_member_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'database transaction input'; END $$;CREATE TRIGGER reject_delete BEFORE DELETE ON members FOR EACH ROW EXECUTE FUNCTION reject_member_delete()`)
	st, b, _ := call(t, s, "POST", "/api/accounts/account-2/transition", token, `{"expectedFrom":"rate_limited","to":"suspended"}`)
	if st != 500 || accountStatus(t, s, token, "account-2") != "rate_limited" {
		t.Fatalf("partial terminal state: %d %v", st, b)
	}
	st, b, _ = call(t, s, "GET", "/api/groups/"+group, token, "")
	if st != 200 || len(b["members"].([]any)) != 3 {
		t.Fatalf("partial membership: %d %v", st, b)
	}
	businessSQL(t, cfg, `DROP TRIGGER reject_delete ON members;DROP FUNCTION reject_member_delete()`)
	st, b, _ = call(t, s, "POST", "/api/accounts/account-2/transition", token, `{"expectedFrom":"rate_limited","to":"suspended"}`)
	if st != 200 {
		t.Fatal(st, b)
	}
	st, b, _ = call(t, s, "GET", "/api/groups/"+group+"/messages", token, "")
	message := b["items"].([]any)[0].(map[string]any)
	if st != 200 || message["deliveryStatus"] != "cancelled" || message["failCode"] != "ACCOUNT_TERMINAL" {
		t.Fatalf("queued consequence lost: %d %v", st, b)
	}
}

func TestPersistedMessagePaginationIsStableAcrossLaterDatabaseInputs(t *testing.T) {
	_, s, cfg := fixtureConfig(t)
	group := insertBusinessGroup(t, cfg)
	token, _ := login(t, s, "admin")
	for _, id := range []string{"one", "two", "three"} {
		businessSQL(t, cfg, `INSERT INTO messages(group_id,msg_id,sender_platform_user_id,is_own,text,sent_at) VALUES($1,$2,'database-author',false,'persisted input','2026-01-01T00:00:00Z')`, group, id)
	}
	st, b, _ := call(t, s, "GET", "/api/groups/"+group+"/messages?limit=2", token, "")
	if st != 200 || len(b["items"].([]any)) != 2 || b["nextCursor"] == nil {
		t.Fatal(st, b)
	}
	cursor := b["nextCursor"].(string)
	businessSQL(t, cfg, `INSERT INTO messages(group_id,msg_id,sender_platform_user_id,is_own,text,sent_at) VALUES($1,'later-input','database-author',false,'later persisted input','2025-01-01T00:00:00Z')`, group)
	st, b, _ = call(t, s, "GET", "/api/groups/"+group+"/messages?limit=2&before="+cursor, token, "")
	items := b["items"].([]any)
	if st != 200 || len(items) != 1 || items[0].(map[string]any)["msgId"] != "one" {
		t.Fatalf("snapshot changed: %d %v", st, b)
	}
}

func TestHistoricalVerificationNormalizationPreservesRealProfilesAndHistory(t *testing.T) {
	a, _, cfg := fixtureConfig(t)
	a.Close()
	pool, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	rewindBusinessSchema(t, cfg, 6)
	businessSQL(t, cfg, `INSERT INTO accounts(id,status,connection_id,rate_limited_until,phone,phone_verified,verification_source,first_name,last_checked_at,last_check_result) VALUES('history-real','online','real-incarnation',NULL,'preserved real phone',true,'telegram','Real profile',now(),'success'),('history-unverified','rate_limited','historical-incarnation',now()+interval '1 hour','preserved input phone',true,'simulated','Input profile',now(),'success')`)
	businessSQL(t, cfg, `INSERT INTO account_logs(account_id,actor,action,result) VALUES('history-real','database input','historical_input','success'),('history-unverified','database input','historical_input','success')`)
	businessSQL(t, cfg, `INSERT INTO account_group_snapshots(account_id,items) VALUES('history-real','[{"id":"database-group-input","title":"Historical row","username":null,"type":"group","role":"unknown"}]'),('history-unverified','[{"id":"database-group-input","title":"Historical row","username":null,"type":"group","role":"unknown"}]')`)
	if e = migration.Apply(cfg.DatabaseURL); e != nil {
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
	for _, id := range []string{"history-real", "history-unverified"} {
		st, b, _ := call(t, s, "GET", "/api/accounts/"+id, token, "")
		if st != 200 {
			t.Fatal(st, b)
		}
		if id == "history-real" {
			if b["phoneVerified"] != true || b["verificationSource"] != "telegram" || b["lastCheckResult"] != "success" || b["firstName"] != "Real profile" || b["status"] != "online" {
				t.Fatalf("real proof changed: %v", b)
			}
		} else if b["phoneVerified"] != false || b["verificationSource"] != nil || b["lastCheckResult"] != nil || b["lastCheckedAt"] != nil || b["phone"] != "preserved input phone" || b["status"] != "disconnected" || b["rateLimitedUntil"] != nil {
			t.Fatalf("historical simulated check still trusted: %v", b)
		}
		st, b, _ = call(t, s, "GET", "/api/accounts/"+id+"/logs?action=historical_input", token, "")
		if st != 200 || len(b["items"].([]any)) != 1 {
			t.Fatal("history deleted", st, b)
		}
		st, b, _ = call(t, s, "GET", "/api/accounts/"+id+"/joined-groups", token, "")
		if st != 200 || (id == "history-real" && (b["lastSyncedAt"] == nil || len(b["items"].([]any)) != 1)) || (id == "history-unverified" && (b["lastSyncedAt"] != nil || len(b["items"].([]any)) != 0)) {
			t.Fatal("snapshot deleted", st, b)
		}
	}
	var retained int
	if e = pool.QueryRow(context.Background(), `SELECT jsonb_array_length(items) FROM account_group_snapshots WHERE account_id='history-unverified'`).Scan(&retained); e != nil || retained != 1 {
		t.Fatal("historical cache data deleted", e, retained)
	}
}

func TestHistoricalCacheDowngradePreservesRetiredVerification(t *testing.T) {
	a, _, cfg := fixtureConfig(t)
	a.Close()
	pool, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	rewindBusinessSchema(t, cfg, 6)
	businessSQL(t, cfg, `INSERT INTO accounts(id,status,phone_verified,verification_source,last_checked_at,last_check_result) VALUES('historical-cache','idle',true,'simulated',now(),'success')`)
	businessSQL(t, cfg, `INSERT INTO account_group_snapshots(account_id,items) VALUES('historical-cache','[{"id":"historical-group","title":"Historical input","username":null,"type":"group","role":"unknown"}]')`)
	if e = migration.Apply(cfg.DatabaseURL); e != nil {
		t.Fatal(e)
	}
	rewindBusinessSchema(t, cfg, 6)
	var preserved bool
	if e = pool.QueryRow(context.Background(), `SELECT jsonb_array_length(s.items)=1 AND s.last_synced_at IS NULL AND a.verification_source IS NULL AND NOT a.phone_verified AND a.last_check_result IS NULL AND a.last_checked_at IS NULL FROM account_group_snapshots s JOIN accounts a ON a.id=s.account_id WHERE a.id='historical-cache'`).Scan(&preserved); e != nil || !preserved {
		t.Fatal("downgrade changed retained cache or manufactured verification", e, preserved)
	}
	if e = migration.Apply(cfg.DatabaseURL); e != nil {
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
	st, b, _ := call(t, s, "GET", "/api/accounts/historical-cache", token, "")
	if st != 200 || b["phoneVerified"] != false || b["verificationSource"] != nil || b["lastCheckResult"] != nil || b["lastCheckedAt"] != nil {
		t.Fatalf("migration cycle manufactured account verification: %d %v", st, b)
	}
	st, b, _ = call(t, s, "GET", "/api/accounts/historical-cache/joined-groups", token, "")
	if st != 200 || b["lastSyncedAt"] != nil || len(b["items"].([]any)) != 0 {
		t.Fatalf("migration cycle manufactured group synchronization: %d %v", st, b)
	}
}
