package platform

import (
	"context"
	"github.com/jackc/pgx/v5"
	"net/http"
	"time"
)

type Account struct {
	ID               string     `json:"id"`
	Status           string     `json:"status"`
	PlatformUserID   *string    `json:"platformUserId"`
	RateLimitedUntil *time.Time `json:"rateLimitedUntil"`
}

var edges = map[string]map[string]bool{
	"idle": {"online": true, "suspended": true, "session_expired": true}, "online": {"idle": true, "rate_limited": true, "disconnected": true, "suspended": true, "session_expired": true}, "rate_limited": {"online": true, "disconnected": true, "suspended": true, "session_expired": true}, "disconnected": {"idle": true, "online": true, "suspended": true, "session_expired": true},
}

func (a *App) accounts(w http.ResponseWriter, r *http.Request) error {
	rows, e := a.pool.Query(r.Context(), `SELECT id,status,platform_user_id,rate_limited_until FROM accounts ORDER BY id`)
	if e != nil {
		return e
	}
	defer rows.Close()
	items := []Account{}
	for rows.Next() {
		var v Account
		if e = rows.Scan(&v.ID, &v.Status, &v.PlatformUserID, &v.RateLimitedUntil); e != nil {
			return e
		}
		items = append(items, v)
	}
	respond(w, 200, items)
	return rows.Err()
}
func emit(ctx context.Context, tx pgx.Tx, typ string, payload any) error {
	var seq int64
	e := tx.QueryRow(ctx, `UPDATE ws_clock SET seq=seq+1 WHERE id=1 RETURNING seq`).Scan(&seq)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO ws_events(seq,type,payload) VALUES($1,$2,$3)`, seq, typ, payload)
	return e
}
func transition(ctx context.Context, tx pgx.Tx, id, from, to string, until *time.Time) error {
	if from == to && (to == "suspended" || to == "session_expired") {
		return nil
	}
	if !edges[from][to] {
		return fail(409, "ILLEGAL_TRANSITION")
	}
	if _, e := tx.Exec(ctx, `UPDATE accounts SET status=$2,rate_limited_until=$3 WHERE id=$1`, id, to, until); e != nil {
		return e
	}
	if to == "suspended" || to == "session_expired" {
		if _, e := tx.Exec(ctx, `DELETE FROM members WHERE account_id=$1`, id); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, `UPDATE messages SET delivery_status='cancelled',fail_code='ACCOUNT_TERMINAL' WHERE account_id=$1 AND delivery_status='queued'`, id); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, `UPDATE sequence_steps SET status='skipped',sent_at=now() WHERE client_msg_id IN (SELECT client_msg_id FROM messages WHERE account_id=$1 AND delivery_status='cancelled')`, id); e != nil {
			return e
		}
		if e := emit(ctx, tx, "account_terminal", map[string]any{"accountId": id, "status": to}); e != nil {
			return e
		}
	}
	return emit(ctx, tx, "account_status_changed", map[string]any{"accountId": id, "from": from, "to": to})
}
func (a *App) manualTransition(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		To           string `json:"to"`
		ExpectedFrom string `json:"expectedFrom"`
	}
	if e := decode(r, &b); e != nil {
		return e
	}
	if b.To == "" || b.ExpectedFrom == "" {
		return fail(400, "VALIDATION_ERROR")
	}
	ctx := r.Context()
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var current string
	e = tx.QueryRow(ctx, `SELECT status FROM accounts WHERE id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&current)
	if e == pgx.ErrNoRows {
		return fail(404, "ACCOUNT_NOT_FOUND")
	}
	if e != nil {
		return e
	}
	if !edges[b.ExpectedFrom][b.To] {
		return fail(409, "ILLEGAL_TRANSITION")
	}
	if current != b.ExpectedFrom {
		return fail(409, "CAS_CONFLICT")
	}
	if e = transition(ctx, tx, r.PathValue("id"), current, b.To, nil); e != nil {
		return e
	}
	if b.To == "idle" || b.To == "disconnected" {
		_, e = tx.Exec(ctx, `INSERT INTO gateway_actions(id,account_id,action) VALUES($1,$2,'disconnect')`, ID(), r.PathValue("id"))
		if e != nil {
			return e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	if b.To == "idle" || b.To == "disconnected" {
		a.disconnectActions(ctx)
	}
	respond(w, 200, map[string]any{"status": b.To})
	return nil
}
func (a *App) connect(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	id := r.PathValue("id")
	var status string
	e = tx.QueryRow(ctx, `SELECT status FROM accounts WHERE id=$1 FOR UPDATE`, id).Scan(&status)
	if e == pgx.ErrNoRows {
		return fail(404, "ACCOUNT_NOT_FOUND")
	}
	if e != nil {
		return e
	}
	if status != "idle" && status != "disconnected" {
		return fail(409, "ILLEGAL_TRANSITION")
	}
	// Complete every older offline intent while holding the same account lock.
	// A request interrupted after dispatch remains pending and must finish before
	// a new connection can be established, including after process reconstruction.
	if e = a.finishDisconnects(ctx, tx, id); e != nil {
		return e
	}
	var out struct {
		PlatformUserID string `json:"platformUserId"`
	}
	e = a.gateway(ctx, "POST", "/accounts/"+id+"/connect", nil, &out)
	if e != nil {
		code := externalCode(e)
		if code == "ACCOUNT_SUSPENDED" || code == "SESSION_EXPIRED" {
			to := "suspended"
			if code == "SESSION_EXPIRED" {
				to = "session_expired"
			}
			if err := transition(ctx, tx, id, status, to, nil); err != nil {
				return err
			}
			if err := tx.Commit(ctx); err != nil {
				return err
			}
		}
		return e
	}
	if out.PlatformUserID == "" {
		return fail(502, "GATEWAY_PROTOCOL_ERROR")
	}
	if _, e = tx.Exec(ctx, `UPDATE accounts SET platform_user_id=$2 WHERE id=$1`, id, out.PlatformUserID); e != nil {
		return e
	}
	if e = transition(ctx, tx, id, status, "online", nil); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	respond(w, 200, map[string]any{"status": "online", "platformUserId": out.PlatformUserID})
	return nil
}
func (a *App) disconnectActions(ctx context.Context) {
	for _, account := range a.ids(ctx, `SELECT DISTINCT account_id FROM gateway_actions WHERE status='pending'`) {
		tx, e := a.pool.Begin(ctx)
		if e != nil {
			return
		}
		var locked string
		if e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, account).Scan(&locked); e == nil {
			e = a.finishDisconnects(ctx, tx, account)
		}
		if e == nil {
			tx.Commit(ctx)
		} else {
			tx.Rollback(ctx)
		}
	}
}

func (a *App) finishDisconnects(ctx context.Context, tx pgx.Tx, account string) error {
	var pending bool
	if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM gateway_actions WHERE account_id=$1 AND status='pending')`, account).Scan(&pending); e != nil || !pending {
		return e
	}
	if e := a.gateway(ctx, "POST", "/accounts/"+account+"/disconnect", nil, nil); e != nil {
		return e
	}
	_, e := tx.Exec(ctx, `UPDATE gateway_actions SET status='done' WHERE account_id=$1 AND status='pending'`, account)
	return e
}
