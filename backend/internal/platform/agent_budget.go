package platform

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"hash/fnv"
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
func (a *App) recoverAgentBudgets(ctx context.Context) error {
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var last time.Time
	e = tx.QueryRow(ctx, `SELECT last_active FROM runtime_clock WHERE id=1 FOR UPDATE`).Scan(&last)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `UPDATE agent_runs SET elapsed_ms=elapsed_ms+GREATEST(0,(extract(epoch FROM ($1::timestamptz-budget_checkpoint))*1000)::bigint),budget_checkpoint=now() WHERE status='running'`, last)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE runtime_clock SET last_active=now() WHERE id=1`); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (a *App) runtimeKey() int64 {
	h := fnv.New64a()
	h.Write([]byte(a.scope + ":runtime"))
	return int64(h.Sum64())
}

// A shared session lock represents a live worker instance. Only a process that
// can acquire the exclusive lock may remove time spent with no instances alive.
func (a *App) startRuntime(ctx context.Context) error {
	c, e := pgx.Connect(ctx, a.cfg.DatabaseURL)
	if e != nil {
		return e
	}
	var first bool
	if e = c.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, a.runtimeKey()).Scan(&first); e == nil && first {
		e = a.recoverAgentBudgets(ctx)
		if e == nil {
			e = a.recoverSequences(ctx)
		}
	}
	if e == nil {
		_, e = c.Exec(ctx, `SELECT pg_advisory_lock_shared($1)`, a.runtimeKey())
	}
	if e == nil && first {
		_, e = c.Exec(ctx, `SELECT pg_advisory_unlock($1)`, a.runtimeKey())
	}
	if e != nil {
		c.Close(context.Background())
		return e
	}
	a.runtime = c
	return nil
}

func (a *App) watchRuntime(ctx context.Context) {
	defer a.wg.Done()
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if a.runtime != nil {
			a.runtime.Close(cleanup)
		}
	}()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		if a.runtime != nil {
			probe, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			_, e := a.runtime.Exec(probe, `SELECT 1`)
			cancel()
			if e == nil {
				continue
			}
			a.runtime.Close(context.Background())
			a.runtime = nil
		}
		// This process remained alive during a DB interruption. Rejoin presence
		// without resetting any online budget or pending sequence schedule.
		connect, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		c, e := pgx.Connect(connect, a.cfg.DatabaseURL)
		if e == nil {
			_, e = c.Exec(connect, `SELECT pg_advisory_lock_shared($1)`, a.runtimeKey())
		}
		cancel()
		if e != nil {
			if c != nil {
				c.Close(context.Background())
			}
			continue
		}
		a.runtime = c
	}
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
