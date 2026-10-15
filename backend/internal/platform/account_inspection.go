package platform

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

func accountGatewayError(e error) error {
	var x *externalError
	if errors.As(e, &x) {
		out := &apiError{status: x.Status, code: safeAccountCode(e), message: safeAccountCode(e)}
		if x.Code == "RATE_LIMITED" {
			out.extra = map[string]any{"retryAfterSeconds": x.RetryAfterSeconds}
		}
		return out
	}
	return e
}
func terminalForError(e error) string {
	switch externalCode(e) {
	case "ACCOUNT_SUSPENDED":
		return "suspended"
	case "SESSION_EXPIRED":
		return "session_expired"
	}
	return ""
}

func (a *App) checkAccount(w http.ResponseWriter, r *http.Request) (err error) {
	var b struct {
		ConnectIfNeeded bool `json:"connectIfNeeded"`
	}
	if e := decode(r, &b); e != nil {
		return e
	}
	ctx := r.Context()
	id := r.PathValue("id")
	logged := false
	defer func() {
		if err != nil && !logged {
			a.recordAccountAttempt(ctx, id, "check", err)
		}
	}()
	current, e := scanAccount(a.pool.QueryRow(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id=$1`, id))
	if e == pgx.ErrNoRows {
		return fail(404, "ACCOUNT_NOT_FOUND")
	}
	if e != nil {
		return e
	}
	if e = accountTerminalError(current.Status); e != nil {
		return e
	}
	if current.Status == "idle" || current.Status == "disconnected" {
		if !b.ConnectIfNeeded {
			return fail(409, "ACCOUNT_OFFLINE")
		}
		e = a.connectAccount(ctx, id)
		if e != nil {
			a.recordAccountAttempt(ctx, id, "connect", e)
			return accountGatewayError(e)
		}
	} else if current.Status != "online" && current.Status != "rate_limited" {
		return fail(409, "ILLEGAL_TRANSITION")
	}
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	current, e = scanAccount(tx.QueryRow(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id=$1 FOR UPDATE`, id))
	if e != nil {
		return e
	}
	if e = accountTerminalError(current.Status); e != nil {
		return e
	}
	if current.Status != "online" && current.Status != "rate_limited" {
		return fail(409, "ACCOUNT_OFFLINE")
	}
	var profile struct {
		PlatformUserID string  `json:"platformUserId"`
		Phone          *string `json:"phone"`
		Username       *string `json:"username"`
		FirstName      *string `json:"firstName"`
		LastName       *string `json:"lastName"`
		Premium        *bool   `json:"premium"`
		Source         string  `json:"source"`
	}
	gatewayErr := a.gateway(ctx, "POST", "/accounts/"+id+"/check", nil, &profile)
	if gatewayErr == nil && (profile.PlatformUserID == "" || (current.PlatformUserID != nil && *current.PlatformUserID != profile.PlatformUserID) || profile.Source != "telegram") {
		gatewayErr = fail(502, "GATEWAY_PROTOCOL_ERROR")
	}
	if gatewayErr != nil {
		code := safeAccountCode(gatewayErr)
		if _, e = tx.Exec(ctx, `UPDATE accounts SET last_checked_at=now(),last_check_result='failed',last_check_error=$2 WHERE id=$1`, id, code); e != nil {
			return e
		}
		if to := terminalForError(gatewayErr); to != "" {
			if e = transition(ctx, tx, id, current.Status, to, nil); e != nil {
				return e
			}
		} else if externalCode(gatewayErr) == "ACCOUNT_OFFLINE" {
			if e = transition(ctx, tx, id, current.Status, "disconnected", nil); e != nil {
				return e
			}
		} else if externalCode(gatewayErr) == "RATE_LIMITED" {
			var limit *externalError
			errors.As(gatewayErr, &limit)
			if e = applyAccountRateLimit(ctx, tx, id, limit.RetryAfterSeconds); e != nil {
				return e
			}
		}
		if e = writeAccountLog(ctx, tx, id, actor(ctx), "check", gatewayErr); e != nil {
			return e
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
		logged = true
		return accountGatewayError(gatewayErr)
	}
	_, e = tx.Exec(ctx, `UPDATE accounts SET platform_user_id=$2,phone=$3,phone_verified=$4,username=$5,first_name=$6,last_name=$7,premium=$8,last_checked_at=now(),last_check_result='success',last_check_error=NULL,verification_source=$9 WHERE id=$1`, id, profile.PlatformUserID, profile.Phone, profile.Source == "telegram" && profile.Phone != nil && *profile.Phone != "", profile.Username, profile.FirstName, profile.LastName, profile.Premium, profile.Source)
	if e != nil {
		return e
	}
	if e = writeAccountLog(ctx, tx, id, actor(ctx), "check", nil); e != nil {
		return e
	}
	current, e = scanAccount(tx.QueryRow(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id=$1`, id))
	if e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	respond(w, 200, current)
	return nil
}

type joinedGroup struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Username *string `json:"username"`
	Type     string  `json:"type"`
	Role     string  `json:"role"`
}
type joinedSnapshot struct {
	Items        []joinedGroup `json:"items"`
	LastSyncedAt *time.Time    `json:"lastSyncedAt"`
}

func (a *App) joinedGroups(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var exists bool
	if e := a.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts WHERE id=$1)`, r.PathValue("id")).Scan(&exists); e != nil {
		return e
	}
	if !exists {
		return fail(404, "ACCOUNT_NOT_FOUND")
	}
	v := joinedSnapshot{Items: []joinedGroup{}}
	var raw json.RawMessage
	e := a.pool.QueryRow(ctx, `SELECT items,last_synced_at FROM account_group_snapshots WHERE account_id=$1`, r.PathValue("id")).Scan(&raw, &v.LastSyncedAt)
	if e != nil && e != pgx.ErrNoRows {
		return e
	}
	if len(raw) > 0 && v.LastSyncedAt != nil {
		if e = json.Unmarshal(raw, &v.Items); e != nil {
			return e
		}
	}
	respond(w, 200, v)
	return nil
}
func (a *App) syncJoinedGroups(w http.ResponseWriter, r *http.Request) (err error) {
	ctx := r.Context()
	id := r.PathValue("id")
	logged := false
	defer func() {
		if err != nil && !logged {
			a.recordAccountAttempt(ctx, id, "sync_groups", err)
		}
	}()
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var status string
	e = tx.QueryRow(ctx, `SELECT status FROM accounts WHERE id=$1 FOR UPDATE`, id).Scan(&status)
	if e == pgx.ErrNoRows {
		return fail(404, "ACCOUNT_NOT_FOUND")
	}
	if e != nil {
		return e
	}
	if e = accountTerminalError(status); e != nil {
		return e
	}
	if status != "online" {
		return fail(409, "ACCOUNT_OFFLINE")
	}
	v := joinedSnapshot{Items: []joinedGroup{}}
	if e = a.gateway(ctx, "GET", "/accounts/"+id+"/joined-groups", nil, &v); e != nil {
		terminal := terminalForError(e)
		if terminal != "" {
			if saveErr := transition(ctx, tx, id, status, terminal, nil); saveErr != nil {
				return saveErr
			}
		} else if externalCode(e) == "RATE_LIMITED" {
			var limit *externalError
			errors.As(e, &limit)
			if saveErr := applyAccountRateLimit(ctx, tx, id, limit.RetryAfterSeconds); saveErr != nil {
				return saveErr
			}
		}
		if terminal != "" || externalCode(e) == "RATE_LIMITED" {
			if saveErr := writeAccountLog(ctx, tx, id, actor(ctx), "sync_groups", e); saveErr != nil {
				return saveErr
			}
			if saveErr := tx.Commit(ctx); saveErr != nil {
				return saveErr
			}
			logged = true
		}
		return accountGatewayError(e)
	}
	if v.Items == nil {
		return fail(502, "GATEWAY_PROTOCOL_ERROR")
	}
	seen := map[string]bool{}
	for _, g := range v.Items {
		if g.ID == "" || seen[g.ID] || (g.Type != "group" && g.Type != "supergroup") || (g.Role != "creator" && g.Role != "admin" && g.Role != "member" && g.Role != "unknown") {
			return fail(502, "GATEWAY_PROTOCOL_ERROR")
		}
		seen[g.ID] = true
	}
	raw, e := json.Marshal(v.Items)
	if e != nil {
		return e
	}
	if e = tx.QueryRow(ctx, `INSERT INTO account_group_snapshots(account_id,items) VALUES($1,$2) ON CONFLICT(account_id) DO UPDATE SET items=excluded.items,last_synced_at=now() RETURNING last_synced_at`, id, raw).Scan(&v.LastSyncedAt); e != nil {
		return e
	}
	if e = writeAccountLog(ctx, tx, id, actor(ctx), "sync_groups", nil); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	respond(w, 200, v)
	return nil
}
