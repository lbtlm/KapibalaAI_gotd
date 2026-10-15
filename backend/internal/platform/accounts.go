package platform

import (
	"context"
	"github.com/jackc/pgx/v5"
	"net/http"
	"time"
)

type Account struct {
	ID                 string     `json:"id"`
	Status             string     `json:"status"`
	PlatformUserID     *string    `json:"platformUserId"`
	RateLimitedUntil   *time.Time `json:"rateLimitedUntil"`
	Phone              *string    `json:"phone"`
	PhoneVerified      bool       `json:"phoneVerified"`
	VerificationSource *string    `json:"verificationSource"`
	Username           *string    `json:"username"`
	FirstName          *string    `json:"firstName"`
	LastName           *string    `json:"lastName"`
	Premium            *bool      `json:"premium"`
	GroupID            *string    `json:"groupId"`
	LastCheckedAt      *time.Time `json:"lastCheckedAt"`
	LastCheckResult    *string    `json:"lastCheckResult"`
	LastCheckError     *string    `json:"lastCheckError"`
	CreatedAt          time.Time  `json:"createdAt"`
}

const accountColumns = `id,status,platform_user_id,rate_limited_until,phone,phone_verified,username,first_name,last_name,premium,group_id,last_checked_at,last_check_result,last_check_error,created_at,verification_source`

func scanAccount(row pgx.Row) (Account, error) {
	var v Account
	e := row.Scan(&v.ID, &v.Status, &v.PlatformUserID, &v.RateLimitedUntil, &v.Phone, &v.PhoneVerified, &v.Username, &v.FirstName, &v.LastName, &v.Premium, &v.GroupID, &v.LastCheckedAt, &v.LastCheckResult, &v.LastCheckError, &v.CreatedAt, &v.VerificationSource)
	return v, e
}

func (a *App) accountGet(w http.ResponseWriter, r *http.Request) error {
	v, e := scanAccount(a.pool.QueryRow(r.Context(), `SELECT `+accountColumns+` FROM accounts WHERE id=$1`, r.PathValue("id")))
	if e == pgx.ErrNoRows {
		return fail(404, "ACCOUNT_NOT_FOUND")
	}
	if e != nil {
		return e
	}
	respond(w, 200, v)
	return nil
}

var edges = map[string]map[string]bool{
	"idle": {"online": true, "suspended": true, "session_expired": true}, "online": {"idle": true, "rate_limited": true, "disconnected": true, "suspended": true, "session_expired": true}, "rate_limited": {"online": true, "disconnected": true, "suspended": true, "session_expired": true}, "disconnected": {"idle": true, "online": true, "suspended": true, "session_expired": true},
}

func accountTerminalError(status string) error {
	switch status {
	case "session_expired":
		return fail(401, "SESSION_EXPIRED")
	case "suspended":
		return fail(403, "ACCOUNT_SUSPENDED")
	}
	return nil
}

func (a *App) accounts(w http.ResponseWriter, r *http.Request) error {
	rows, e := a.pool.Query(r.Context(), `SELECT `+accountColumns+` FROM accounts ORDER BY id`)
	if e != nil {
		return e
	}
	defer rows.Close()
	items := []Account{}
	for rows.Next() {
		v, err := scanAccount(rows)
		if err != nil {
			return err
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
	if _, e := tx.Exec(ctx, `UPDATE accounts SET status=$2,rate_limited_until=$3,connection_id=CASE WHEN $2 IN ('idle','disconnected','suspended','session_expired') THEN NULL ELSE connection_id END WHERE id=$1`, id, to, until); e != nil {
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

func applyAccountRateLimit(ctx context.Context, tx pgx.Tx, id string, seconds float64) error {
	var status string
	var existing *time.Time
	if e := tx.QueryRow(ctx, `SELECT status,rate_limited_until FROM accounts WHERE id=$1 FOR UPDATE`, id).Scan(&status, &existing); e != nil {
		return e
	}
	until := time.Now().UTC().Add(time.Duration(seconds * float64(time.Second)))
	if existing != nil && existing.After(until) {
		until = *existing
	}
	if status == "online" {
		return transition(ctx, tx, id, status, "rate_limited", &until)
	}
	if status == "rate_limited" {
		_, e := tx.Exec(ctx, `UPDATE accounts SET rate_limited_until=$2 WHERE id=$1`, id, until)
		return e
	}
	return nil
}
func (a *App) manualTransition(w http.ResponseWriter, r *http.Request) (err error) {
	action := "transition"
	defer func() {
		if err != nil {
			a.recordAccountAttempt(r.Context(), r.PathValue("id"), action, err)
		}
	}()
	var b struct {
		To           string `json:"to"`
		ExpectedFrom string `json:"expectedFrom"`
	}
	if e := decode(r, &b); e != nil {
		return e
	}
	if b.To == "idle" || b.To == "disconnected" {
		action = "disconnect"
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
	if e = writeAccountLog(ctx, tx, r.PathValue("id"), actor(ctx), action, nil); e != nil {
		return e
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
func (a *App) connect(w http.ResponseWriter, r *http.Request) (err error) {
	defer func() {
		if err != nil {
			a.recordAccountAttempt(r.Context(), r.PathValue("id"), "connect", err)
		}
	}()
	if err = a.connectAccount(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	v, e := scanAccount(a.pool.QueryRow(r.Context(), `SELECT `+accountColumns+` FROM accounts WHERE id=$1`, r.PathValue("id")))
	if e != nil {
		return e
	}
	respond(w, 200, map[string]any{"status": v.Status, "platformUserId": v.PlatformUserID})
	return nil
}
func (a *App) connectAccount(ctx context.Context, id string) error {
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var status string
	var incarnation *string
	e = tx.QueryRow(ctx, `SELECT status,connection_id FROM accounts WHERE id=$1 FOR UPDATE`, id).Scan(&status, &incarnation)
	if e == pgx.ErrNoRows {
		return fail(404, "ACCOUNT_NOT_FOUND")
	}
	if e != nil {
		return e
	}
	if e = accountTerminalError(status); e != nil {
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
	if incarnation == nil {
		v := ID()
		incarnation = &v
		if _, e = tx.Exec(ctx, `UPDATE accounts SET connection_id=$2 WHERE id=$1`, id, v); e != nil {
			return e
		}
	}
	connectionID := *incarnation
	// A committed intent survives a lost response or a killed backend. Replays use
	// this exact gateway incarnation rather than opening an untracked connection.
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	tx, e = a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = tx.QueryRow(ctx, `SELECT status,connection_id FROM accounts WHERE id=$1 FOR UPDATE`, id).Scan(&status, &incarnation); e != nil {
		return e
	}
	if e = accountTerminalError(status); e != nil {
		return e
	}
	if incarnation == nil || *incarnation != connectionID {
		return fail(409, "CAS_CONFLICT")
	}
	if status != "idle" && status != "disconnected" {
		return fail(409, "ILLEGAL_TRANSITION")
	}
	var out struct {
		PlatformUserID string `json:"platformUserId"`
		ConnectionID   string `json:"connectionId"`
	}
	e = a.gateway(ctx, "POST", "/accounts/"+id+"/connect", map[string]any{"connectionId": connectionID}, &out)
	if e != nil {
		code := externalCode(e)
		if to := terminalForError(e); to != "" {
			if err := transition(ctx, tx, id, status, to, nil); err != nil {
				return err
			}
			if err := tx.Commit(ctx); err != nil {
				return err
			}
		} else if code == "CONNECTION_ENDED" {
			if _, err := tx.Exec(ctx, `UPDATE accounts SET connection_id=NULL WHERE id=$1 AND connection_id=$2`, id, connectionID); err != nil {
				return err
			}
			if err := tx.Commit(ctx); err != nil {
				return err
			}
		}
		return accountGatewayError(e)
	}
	if out.PlatformUserID == "" || (out.ConnectionID != "" && out.ConnectionID != connectionID) {
		return fail(502, "GATEWAY_PROTOCOL_ERROR")
	}
	if _, e = tx.Exec(ctx, `UPDATE accounts SET platform_user_id=$2 WHERE id=$1`, id, out.PlatformUserID); e != nil {
		return e
	}
	if e = transition(ctx, tx, id, status, "online", nil); e != nil {
		return e
	}
	if e = writeAccountLog(ctx, tx, id, actor(ctx), "connect", nil); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
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
