package platform

import (
	"net/http"

	"github.com/jackc/pgx/v5"
)

func (a *App) deleteAccount(w http.ResponseWriter, r *http.Request) (err error) {
	id, ctx := r.PathValue("id"), r.Context()
	gatewayDeleted := false
	defer func() {
		if err != nil {
			if gatewayDeleted {
				err = fail(503, "ACCOUNT_DELETE_INCOMPLETE")
			}
			a.recordAccountAttempt(ctx, id, "delete", err)
		}
	}()
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var imported string
	if e = tx.QueryRow(ctx, `SELECT account_id FROM account_imports WHERE account_id=$1 FOR UPDATE`, id).Scan(&imported); e != nil && e != pgx.ErrNoRows {
		return e
	}
	var status string
	var connectionID *string
	if e = tx.QueryRow(ctx, `SELECT status,connection_id FROM accounts WHERE id=$1 FOR UPDATE`, id).Scan(&status, &connectionID); e == pgx.ErrNoRows {
		w.WriteHeader(http.StatusNoContent)
		return nil
	} else if e != nil {
		return e
	}
	reason := ""
	if connectionID != nil || (status != "idle" && status != "disconnected" && status != "session_expired" && status != "suspended") {
		reason = "active_connection"
	} else if e = tx.QueryRow(ctx, `SELECT CASE
        WHEN EXISTS(SELECT 1 FROM groups g WHERE g.status!='left' AND (g.creator_account_id=$1 OR EXISTS(SELECT 1 FROM members m WHERE m.group_id=g.id AND m.account_id=$1))) THEN 'managed_group'
        WHEN EXISTS(SELECT 1 FROM messages WHERE account_id=$1 AND delivery_status IN ('queued','dispatching','accepted','unknown')) THEN 'pending_message'
        WHEN EXISTS(SELECT 1 FROM jobs WHERE status='running' AND (state->>'creator'=$1 OR (state->'members') ? $1))
          OR EXISTS(SELECT 1 FROM groups g WHERE (g.creator_account_id=$1 OR EXISTS(SELECT 1 FROM members m WHERE m.group_id=g.id AND m.account_id=$1)) AND (EXISTS(SELECT 1 FROM agent_runs ar WHERE ar.group_id=g.id AND ar.status='running') OR EXISTS(SELECT 1 FROM sequence_runs sr WHERE sr.group_id=g.id AND sr.status='running'))) THEN 'active_job'
        WHEN EXISTS(SELECT 1 FROM agent_effects WHERE kind='kick' AND state='inflight' AND input->>'byAccountId'=$1) THEN 'active_effect'
        ELSE '' END`, id).Scan(&reason); e != nil {
		return e
	}
	if reason != "" {
		return &apiError{status: 409, code: "ACCOUNT_IN_USE", message: "ACCOUNT_IN_USE", extra: map[string]any{"reason": reason}}
	}
	if e = a.gateway(ctx, http.MethodDelete, "/accounts/"+id, nil, nil); e != nil {
		return accountGatewayError(e)
	}
	gatewayDeleted = true
	for _, query := range []string{
		`UPDATE messages SET account_id=NULL WHERE account_id=$1`,
		`UPDATE members SET account_id=NULL WHERE account_id=$1`,
		`UPDATE groups SET creator_account_id=NULL WHERE creator_account_id=$1 AND status='left'`,
		`DELETE FROM account_group_snapshots WHERE account_id=$1`,
		`DELETE FROM gateway_actions WHERE account_id=$1`,
		`DELETE FROM account_imports WHERE account_id=$1`,
	} {
		if _, e = tx.Exec(ctx, query, id); e != nil {
			return e
		}
	}
	if e = writeAccountLog(ctx, tx, id, actor(ctx), "delete", nil); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, id); e != nil {
		return e
	}
	if e = emit(ctx, tx, "account_deleted", map[string]any{"accountId": id}); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
