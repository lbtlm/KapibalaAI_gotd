package platform

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"time"
)

func agentFence(ctx context.Context, tx pgx.Tx, r *agentRun) error {
	var epoch int64
	e := tx.QueryRow(ctx, `SELECT execution_epoch FROM agent_runs WHERE id=$1 AND status='running' FOR UPDATE`, r.ID).Scan(&epoch)
	if e != nil {
		return e
	}
	if epoch != r.Epoch {
		return fmt.Errorf("agent execution fence lost")
	}
	return nil
}
func (a *App) currentAgentSend(ctx context.Context, client string) (map[string]any, error) {
	var status string
	var failCode *string
	e := a.pool.QueryRow(ctx, `SELECT CASE WHEN delivery_status='dispatching' THEN 'queued' ELSE delivery_status END,fail_code FROM messages WHERE client_msg_id=$1`, client).Scan(&status, &failCode)
	if e != nil {
		return nil, e
	}
	return map[string]any{"clientMsgId": client, "deliveryStatus": status, "failCode": failCode}, nil
}
func (a *App) recoverAgentBudgets(ctx context.Context) {
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return
	}
	defer tx.Rollback(ctx)
	var last time.Time
	e = tx.QueryRow(ctx, `SELECT last_active FROM runtime_clock WHERE id=1 FOR UPDATE`).Scan(&last)
	if e != nil {
		return
	}
	if time.Since(last) > 2*time.Second {
		_, e = tx.Exec(ctx, `UPDATE agent_runs SET budget_checkpoint=now() WHERE status='running'`)
	}
	if e != nil {
		return
	}
	if _, e = tx.Exec(ctx, `UPDATE runtime_clock SET last_active=now() WHERE id=1`); e != nil {
		return
	}
	tx.Commit(ctx)
}
func (a *App) agentBudgetTick(ctx context.Context) {
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return
	}
	defer tx.Rollback(ctx)
	var last time.Time
	if e = tx.QueryRow(ctx, `SELECT last_active FROM runtime_clock WHERE id=1 FOR UPDATE`).Scan(&last); e != nil {
		return
	}
	if _, e = tx.Exec(ctx, `UPDATE agent_runs SET elapsed_ms=elapsed_ms+GREATEST(0,(extract(epoch FROM (now()-budget_checkpoint))*1000)::bigint),budget_checkpoint=now() WHERE status='running'`); e != nil {
		return
	}
	if _, e = tx.Exec(ctx, `UPDATE runtime_clock SET last_active=now() WHERE id=1`); e != nil {
		return
	}
	tx.Commit(ctx)
}
