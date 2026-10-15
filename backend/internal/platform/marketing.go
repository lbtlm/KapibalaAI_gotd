package platform

import (
	"github.com/jackc/pgx/v5"
	"net/http"
)

// Pause stops this campaign's automation, not membership or the persistent group agent.
func (a *App) marketing(w http.ResponseWriter, r *http.Request) error {
	action := r.PathValue("action")
	if action != "pause" && action != "resume" {
		return fail(404, "NOT_FOUND")
	}
	ctx, id := r.Context(), r.PathValue("id")
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var status string
	var paused bool
	e = tx.QueryRow(ctx, `SELECT status,marketing_paused FROM groups WHERE id=$1 FOR UPDATE`, id).Scan(&status, &paused)
	if e == pgx.ErrNoRows {
		return fail(404, "GROUP_NOT_FOUND")
	}
	if e != nil {
		return e
	}
	if status != "active" {
		return fail(409, "GROUP_UNREACHABLE")
	}
	target := action == "pause"
	if paused == target {
		return a.groupGet(w, r)
	}
	var busy bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE group_id=$1 AND status='running')`, id).Scan(&busy); e != nil {
		return e
	}
	if busy {
		return fail(409, "GROUP_IN_USE")
	}
	if !target {
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_runs WHERE group_id=$1 AND status='running') OR EXISTS(SELECT 1 FROM messages m WHERE m.group_id=$1 AND m.delivery_status IN ('dispatching','unknown','accepted') AND (EXISTS(SELECT 1 FROM sequence_steps s WHERE s.client_msg_id=m.client_msg_id) OR EXISTS(SELECT 1 FROM agent_effects e WHERE e.client_msg_id=m.client_msg_id))) OR EXISTS(SELECT 1 FROM agent_effects e JOIN agent_runs a ON a.id=e.run_id WHERE a.group_id=$1 AND e.kind='kick' AND e.state!='done')`, id).Scan(&busy); e != nil {
			return e
		}
		if busy {
			return fail(409, "MARKETING_DRAINING")
		}
	}
	if _, e = tx.Exec(ctx, `UPDATE groups SET marketing_paused=$2 WHERE id=$1`, id, target); e != nil {
		return e
	}
	if target {
		if _, e = tx.Exec(ctx, `DELETE FROM agent_pending WHERE group_id=$1`, id); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE sequence_runs SET status='cancelled' WHERE group_id=$1 AND status='running'`, id); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE messages m SET delivery_status='cancelled',fail_code='MARKETING_PAUSED' WHERE m.group_id=$1 AND m.delivery_status='queued' AND (EXISTS(SELECT 1 FROM sequence_steps s WHERE s.client_msg_id=m.client_msg_id) OR EXISTS(SELECT 1 FROM agent_effects e WHERE e.client_msg_id=m.client_msg_id))`, id); e != nil {
			return e
		}
		if e = emit(ctx, tx, "message", map[string]any{"groupId": id, "isOwn": true}); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, `INSERT INTO jobs(id,kind,group_id,status,state,logs) VALUES($1,$2,$3,'finished',jsonb_build_object('phase',$2::text),jsonb_build_array(jsonb_build_object('at',now(),'phase',$2::text,'result','success')))`, ID(), action, id); e != nil {
		return e
	}
	if e = emit(ctx, tx, "group", map[string]any{"groupId": id, "marketingPaused": target}); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	return a.groupGet(w, r)
}
