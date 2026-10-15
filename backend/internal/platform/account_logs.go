package platform

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strconv"
	"time"
)

func actor(ctx context.Context) string {
	u, _ := ctx.Value(authKey{}).(identity)
	if u.username != "" {
		return u.username
	}
	return "system"
}
func safeAccountCode(e error) string {
	var p *apiError
	if errors.As(e, &p) {
		return p.code
	}
	var x *externalError
	if errors.As(e, &x) {
		switch x.Code {
		case "ACCOUNT_SUSPENDED", "SESSION_EXPIRED", "ACCOUNT_OFFLINE", "CONNECTION_ENDED", "CONNECTION_CONFLICT", "RATE_LIMITED", "NETWORK_TIMEOUT", "GATEWAY_UNAVAILABLE", "ACCOUNT_IMPORT_CONFLICT", "INVALID_SESSION", "INVALID_CREDENTIALS", "INVALID_INPUT":
			return x.Code
		}
		return "GATEWAY_ERROR"
	}
	return "INTERNAL_ERROR"
}
func writeAccountLog(ctx context.Context, tx pgx.Tx, id, who, action string, err error) error {
	result := "success"
	var code *string
	if err != nil {
		result = "failed"
		v := safeAccountCode(err)
		code = &v
	}
	_, e := tx.Exec(ctx, `INSERT INTO account_logs(account_id,actor,action,result,error_code) VALUES($1,$2,$3,$4,$5)`, id, who, action, result, code)
	return e
}
func (a *App) recordAccountAttempt(ctx context.Context, id, action string, err error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return
	}
	defer tx.Rollback(ctx)
	var exists bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts WHERE id=$1)`, id).Scan(&exists); e == nil && exists {
		if action == "check" && err != nil {
			if _, e = tx.Exec(ctx, `UPDATE accounts SET last_checked_at=now(),last_check_result='failed',last_check_error=$2 WHERE id=$1 AND (status NOT IN ('suspended','session_expired') OR (status='suspended' AND $2='ACCOUNT_SUSPENDED') OR (status='session_expired' AND $2='SESSION_EXPIRED'))`, id, safeAccountCode(err)); e != nil {
				return
			}
		}
		if writeAccountLog(ctx, tx, id, actor(ctx), action, err) == nil {
			tx.Commit(ctx)
		}
	}
}

type accountLog struct {
	ID        string    `json:"id"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Result    string    `json:"result"`
	ErrorCode *string   `json:"errorCode"`
	CreatedAt time.Time `json:"createdAt"`
}

func (a *App) accountLogs(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var exists bool
	if e := a.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts WHERE id=$1)`, r.PathValue("id")).Scan(&exists); e != nil {
		return e
	}
	if !exists {
		return fail(404, "ACCOUNT_NOT_FOUND")
	}
	cursor := int64(0)
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		var e error
		cursor, e = strconv.ParseInt(raw, 10, 64)
		if e != nil || cursor < 1 {
			return fail(400, "VALIDATION_ERROR")
		}
	}
	action, result := r.URL.Query().Get("action"), r.URL.Query().Get("result")
	if result != "" && result != "success" && result != "failed" {
		return fail(400, "VALIDATION_ERROR")
	}
	rows, e := a.pool.Query(ctx, `SELECT id,actor,action,result,error_code,created_at FROM account_logs WHERE account_id=$1 AND ($2::bigint=0 OR id<$2) AND ($3='' OR action=$3) AND ($4='' OR result=$4) ORDER BY id DESC LIMIT 21`, r.PathValue("id"), cursor, action, result)
	if e != nil {
		return e
	}
	defer rows.Close()
	items := []accountLog{}
	for rows.Next() {
		var v accountLog
		var id int64
		if e = rows.Scan(&id, &v.Actor, &v.Action, &v.Result, &v.ErrorCode, &v.CreatedAt); e != nil {
			return e
		}
		v.ID = strconv.FormatInt(id, 10)
		items = append(items, v)
	}
	if e = rows.Err(); e != nil {
		return e
	}
	var next *string
	if len(items) > 20 {
		items = items[:20]
		next = &items[19].ID
	}
	respond(w, 200, map[string]any{"items": items, "nextCursor": next})
	return nil
}
