package platform_test

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
	"time"
)

// Exercise the real send scheduler and database; cancelled work must never reach a gateway.
func TestIdleAccountsCannotStarveQueuedCampaignWork(t *testing.T) {
	app, _, cfg := fixtureConfig(t)
	insertBusinessGroup(t, cfg)
	businessSQL(t, cfg, `INSERT INTO accounts(id,status,platform_user_id) SELECT 'idle-'||n,'online','idle-user-'||n FROM generate_series(1,30) n`)
	pool, e := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	rows, e := pool.Query(context.Background(), `SELECT id FROM accounts WHERE status IN ('online','rate_limited') UNION SELECT DISTINCT account_id FROM messages WHERE delivery_status IN ('dispatching','unknown')`)
	if e != nil {
		t.Fatal(e)
	}
	var last string
	for rows.Next() {
		if e = rows.Scan(&last); e != nil {
			t.Fatal(e)
		}
	}
	rows.Close()
	businessSQL(t, cfg, `INSERT INTO sequences(id,name,steps) VALUES('cancelled-script','Script','[]'); INSERT INTO sequence_runs(id,group_id,sequence_id,status) VALUES('cancelled-run','database-group','cancelled-script','cancelled')`)
	businessSQL(t, cfg, `INSERT INTO messages(group_id,client_msg_id,account_id,sender_platform_user_id,is_own,text,delivery_status) SELECT 'database-group','waiting',id,platform_user_id,true,'cancelled input','queued' FROM accounts WHERE id=$1`, last)
	businessSQL(t, cfg, `INSERT INTO sequence_steps(run_id,index,account_role,text,delay_seconds,client_msg_id,resolved_vars,var_sources) VALUES('cancelled-run',1,'member','cancelled input',0,'waiting','{}','{}')`)
	if e = app.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	until := time.Now().Add(time.Second)
	var state string
	for time.Now().Before(until) {
		pool.QueryRow(context.Background(), `SELECT delivery_status FROM messages WHERE client_msg_id='waiting'`).Scan(&state)
		if state == "cancelled" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(fmt.Sprintf("queued account %s starved by idle online accounts: state=%s", last, state))
}
