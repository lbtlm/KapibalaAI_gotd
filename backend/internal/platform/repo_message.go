package platform

// Copied from go_follow_live/backend/internal/repository/repo_message.go.
// ON CONFLICT inbox deduplication and SKIP LOCKED account FIFO are adapted here.

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Message struct {
	MsgID                *string   `json:"msgId"`
	ClientMsgID          *string   `json:"clientMsgId"`
	SenderPlatformUserID string    `json:"senderPlatformUserId"`
	IsOwn                bool      `json:"isOwn"`
	Text                 string    `json:"text"`
	SentAt               time.Time `json:"sentAt"`
	DeliveryStatus       *string   `json:"deliveryStatus"`
	FailCode             *string   `json:"failCode"`
}

func enqueue(ctx context.Context, tx pgx.Tx, groupID, account, text string) (string, error) {
	var status string
	var sender *string
	var member bool
	var groupStatus string
	e := tx.QueryRow(ctx, `SELECT status FROM groups WHERE id=$1 FOR UPDATE`, groupID).Scan(&groupStatus)
	if e == pgx.ErrNoRows {
		return "", fail(404, "GROUP_NOT_FOUND")
	}
	if e != nil {
		return "", e
	}
	e = tx.QueryRow(ctx, `SELECT a.status,a.platform_user_id,EXISTS(SELECT 1 FROM members m WHERE m.group_id=$2 AND m.account_id=a.id) FROM accounts a WHERE a.id=$1 FOR UPDATE OF a`, account, groupID).Scan(&status, &sender, &member)
	if e == pgx.ErrNoRows {
		return "", fail(409, "ACCOUNT_UNAVAILABLE")
	}
	if e != nil {
		return "", e
	}
	if status != "online" && status != "rate_limited" {
		return "", fail(409, "ACCOUNT_UNAVAILABLE")
	}
	if !member {
		return "", fail(409, "ACCOUNT_NOT_IN_GROUP")
	}
	if groupStatus != "active" {
		return "", fail(409, "GROUP_UNREACHABLE")
	}
	id := ID()
	_, e = tx.Exec(ctx, `INSERT INTO messages(group_id,client_msg_id,account_id,sender_platform_user_id,is_own,text,delivery_status) VALUES($1,$2,$3,$4,true,$5,'queued')`, groupID, id, account, sender, text)
	if e != nil {
		return "", e
	}
	e = emit(ctx, tx, "message", map[string]any{"groupId": groupID, "msgId": nil, "clientMsgId": id, "isOwn": true})
	return id, e
}
func (a *App) send(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		AccountID string `json:"accountId"`
		Text      string `json:"text"`
	}
	if e := decode(r, &b); e != nil {
		return e
	}
	if b.AccountID == "" || strings.TrimSpace(b.Text) == "" {
		return fail(400, "VALIDATION_ERROR")
	}
	ctx := r.Context()
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	id, e := enqueue(ctx, tx, r.PathValue("id"), b.AccountID, b.Text)
	if e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	respond(w, 202, map[string]any{"clientMsgId": id})
	return nil
}
func (a *App) messages(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 50 {
		limit = 50
	}
	cursor := r.URL.Query().Get("before")
	var snapshot string
	offset := 0
	var items []Message
	if cursor == "" {
		rows, e := a.pool.Query(ctx, `SELECT msg_id,client_msg_id,sender_platform_user_id,is_own,text,sent_at,CASE WHEN delivery_status='dispatching' THEN 'queued' ELSE delivery_status END,fail_code FROM messages WHERE group_id=$1 ORDER BY sent_at DESC,id DESC`, r.PathValue("id"))
		if e != nil {
			return e
		}
		items = []Message{}
		for rows.Next() {
			var m Message
			if e = rows.Scan(&m.MsgID, &m.ClientMsgID, &m.SenderPlatformUserID, &m.IsOwn, &m.Text, &m.SentAt, &m.DeliveryStatus, &m.FailCode); e != nil {
				rows.Close()
				return e
			}
			items = append(items, m)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		snapshot = ID()
		if _, e = a.pool.Exec(ctx, `INSERT INTO message_snapshots(id,group_id,items) VALUES($1,$2,$3)`, snapshot, r.PathValue("id"), items); e != nil {
			return e
		}
	} else {
		parts := strings.Split(cursor, ":")
		if len(parts) != 2 {
			return fail(400, "VALIDATION_ERROR")
		}
		snapshot = parts[0]
		var e error
		offset, e = strconv.Atoi(parts[1])
		if e != nil || offset < 0 {
			return fail(400, "VALIDATION_ERROR")
		}
		var raw json.RawMessage
		e = a.pool.QueryRow(ctx, `SELECT items FROM message_snapshots WHERE id=$1 AND group_id=$2`, snapshot, r.PathValue("id")).Scan(&raw)
		if e != nil {
			return fail(400, "INVALID_CURSOR")
		}
		if json.Unmarshal(raw, &items) != nil {
			return fail(400, "INVALID_CURSOR")
		}
	}
	if offset > len(items) {
		return fail(400, "INVALID_CURSOR")
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	var next *string
	if end < len(items) {
		s := snapshot + ":" + strconv.Itoa(end)
		next = &s
	}
	respond(w, 200, map[string]any{"items": items[offset:end], "nextCursor": next})
	return nil
}
func (a *App) sendTick(ctx context.Context) {
	for _, id := range a.ids(ctx, `SELECT id FROM accounts WHERE status IN ('online','rate_limited') UNION SELECT DISTINCT account_id FROM messages WHERE delivery_status IN ('dispatching','unknown')`) {
		a.sendMu.Lock()
		if a.sending[id] {
			a.sendMu.Unlock()
			continue
		}
		a.sending[id] = true
		a.sendMu.Unlock()
		a.wg.Add(1)
		go func(id string) {
			defer a.wg.Done()
			defer func() { a.sendMu.Lock(); delete(a.sending, id); a.sendMu.Unlock() }()
			if e := a.lock(ctx, "send:"+id, func(ctx context.Context) error { return a.sendOne(ctx, id) }); e != nil && ctx.Err() == nil {
				slog.Warn("send worker retrying", "accountId", id, "error", e)
			}
		}(id)
	}
}
func (a *App) sendOne(ctx context.Context, account string) error {
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var status string
	var until *time.Time
	e = tx.QueryRow(ctx, `SELECT status,rate_limited_until FROM accounts WHERE id=$1 FOR UPDATE`, account).Scan(&status, &until)
	if e != nil {
		return e
	}
	if status == "rate_limited" {
		if until != nil && time.Now().Before(*until) {
			return nil
		}
		if e = transition(ctx, tx, account, status, "online", nil); e != nil {
			return e
		}
		status = "online"
	}
	var id, groupID, gatewayID, text, state string
	var started, reconcile *time.Time
	var retries int
	e = tx.QueryRow(ctx, `SELECT m.client_msg_id,m.group_id,g.gateway_group_id,m.text,m.delivery_status,m.dispatch_started_at,m.retry_count,m.reconcile_after FROM messages m JOIN groups g ON g.id=m.group_id WHERE m.id=(SELECT id FROM messages WHERE account_id=$1 AND delivery_status IN ('queued','dispatching','unknown') ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED)`, account).Scan(&id, &groupID, &gatewayID, &text, &state, &started, &retries, &reconcile)
	if e == pgx.ErrNoRows {
		return tx.Commit(ctx)
	}
	if e != nil {
		return e
	}
	if state == "unknown" || state == "dispatching" {
		if e = tx.Commit(ctx); e != nil {
			return e
		}
		return a.reconcileMessage(ctx, id, groupID, gatewayID, account, text, started, reconcile, retries)
	}
	if status != "online" {
		return nil
	}
	now := time.Now()
	_, e = tx.Exec(ctx, `UPDATE messages SET delivery_status='dispatching',dispatch_started_at=$2,reconcile_after=$2::timestamptz+interval '14 seconds' WHERE client_msg_id=$1`, id, now)
	if e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	return a.dispatch(ctx, id, groupID, gatewayID, account, text)
}
func (a *App) dispatch(ctx context.Context, id, groupID, gatewayID, account, text string) error {
	e := a.gateway(ctx, "POST", "/groups/"+gatewayID+"/send", map[string]any{"accountId": account, "clientMsgId": id, "text": text}, nil)
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if e == nil {
		_, err = tx.Exec(ctx, `UPDATE messages SET delivery_status='accepted' WHERE client_msg_id=$1 AND delivery_status IN ('dispatching','unknown')`, id)
	} else {
		code := externalCode(e)
		switch code {
		case "RATE_LIMITED":
			var x *externalError
			errors.As(e, &x)
			until := time.Now().Add(time.Duration(x.RetryAfterSeconds * float64(time.Second)))
			var from string
			if err = tx.QueryRow(ctx, `SELECT status FROM accounts WHERE id=$1 FOR UPDATE`, account).Scan(&from); err != nil {
				return err
			}
			if from == "online" {
				err = transition(ctx, tx, account, from, "rate_limited", &until)
			} else if from == "rate_limited" {
				_, err = tx.Exec(ctx, `UPDATE accounts SET rate_limited_until=$2 WHERE id=$1`, account, until)
			}
			if err == nil {
				_, err = tx.Exec(ctx, `UPDATE messages SET delivery_status='queued',dispatch_started_at=NULL WHERE client_msg_id=$1 AND delivery_status='dispatching'`, id)
			}
		case "ACCOUNT_SUSPENDED", "SESSION_EXPIRED":
			from := ""
			if err = tx.QueryRow(ctx, `SELECT status FROM accounts WHERE id=$1 FOR UPDATE`, account).Scan(&from); err != nil {
				return err
			}
			to := "suspended"
			if code == "SESSION_EXPIRED" {
				to = "session_expired"
			}
			err = transition(ctx, tx, account, from, to, nil)
			if err == nil {
				_, err = tx.Exec(ctx, `UPDATE messages SET delivery_status='failed',fail_code=$2 WHERE client_msg_id=$1 AND delivery_status!='sent'`, id, code)
			}
		case "GROUP_WRITE_FORBIDDEN":
			err = unreachable(ctx, tx, groupID)
			if err == nil {
				_, err = tx.Exec(ctx, `UPDATE messages SET delivery_status='failed',fail_code=$2 WHERE client_msg_id=$1 AND delivery_status!='sent'`, id, code)
			}
		case "SENDER_NOT_IN_GROUP", "ACCOUNT_OFFLINE":
			_, err = tx.Exec(ctx, `UPDATE messages SET delivery_status='failed',fail_code=$2 WHERE client_msg_id=$1 AND delivery_status!='sent'`, id, code)
		default:
			_, err = tx.Exec(ctx, `UPDATE messages SET delivery_status='unknown',reconcile_after=now()+interval '2 seconds' WHERE client_msg_id=$1 AND delivery_status IN ('dispatching','unknown')`, id)
		}
	}
	if err != nil {
		return err
	}
	if err = emit(ctx, tx, "message", map[string]any{"groupId": groupID, "clientMsgId": id, "isOwn": true}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (a *App) reconcileMessage(ctx context.Context, id, groupID, gatewayID, account, text string, started, reconcile *time.Time, retries int) error {
	var out struct {
		MsgID  string    `json:"msgId"`
		SentAt time.Time `json:"sentAt"`
	}
	e := a.gateway(ctx, "GET", "/groups/"+gatewayID+"/messages/by-client-id/"+id, nil, &out)
	if e == nil {
		tx, e := a.pool.Begin(ctx)
		if e != nil {
			return e
		}
		defer tx.Rollback(ctx)
		if e = markSent(ctx, tx, id, out.MsgID, out.SentAt); e != nil {
			return e
		}
		return tx.Commit(ctx)
	}
	var x *externalError
	if !errors.As(e, &x) || x.Status != 404 {
		return nil
	}
	if reconcile != nil && time.Now().Before(*reconcile) {
		return nil
	}
	if started != nil && time.Since(*started) < 2*time.Second {
		return nil
	}
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var current, accountStatus, groupStatus string
	e = tx.QueryRow(ctx, `SELECT m.delivery_status,a.status,g.status FROM messages m JOIN accounts a ON a.id=m.account_id JOIN groups g ON g.id=m.group_id WHERE m.client_msg_id=$1 FOR UPDATE OF m,a,g`, id).Scan(&current, &accountStatus, &groupStatus)
	if e != nil {
		return e
	}
	if current == "sent" || current == "failed" || current == "cancelled" {
		return nil
	}
	if retries >= 1 || accountStatus != "online" || groupStatus != "active" {
		code := "NETWORK_TIMEOUT"
		if accountStatus == "suspended" || accountStatus == "session_expired" {
			code = "ACCOUNT_TERMINAL"
		}
		if groupStatus != "active" {
			code = "GROUP_UNREACHABLE"
		}
		_, e = tx.Exec(ctx, `UPDATE messages SET delivery_status='failed',fail_code=$2 WHERE client_msg_id=$1`, id, code)
		if e != nil {
			return e
		}
		if e = emit(ctx, tx, "message", map[string]any{"groupId": groupID, "clientMsgId": id, "isOwn": true}); e != nil {
			return e
		}
		return tx.Commit(ctx)
	}
	_, e = tx.Exec(ctx, `UPDATE messages SET delivery_status='dispatching',retry_count=retry_count+1,dispatch_started_at=now(),reconcile_after=now()+interval '14 seconds' WHERE client_msg_id=$1`, id)
	if e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	return a.dispatch(ctx, id, groupID, gatewayID, account, text)
}
func markSent(ctx context.Context, tx pgx.Tx, client, msg string, sent time.Time) error {
	var groupID string
	e := tx.QueryRow(ctx, `SELECT group_id FROM messages WHERE client_msg_id=$1 FOR UPDATE`, client).Scan(&groupID)
	if e == pgx.ErrNoRows {
		return nil
	}
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `DELETE FROM messages WHERE group_id=$1 AND msg_id=$2 AND client_msg_id IS NULL`, groupID, msg); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE messages SET msg_id=$2,sent_at=$3,delivery_status='sent',fail_code=NULL WHERE client_msg_id=$1`, client, msg, sent); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE sequence_steps SET status='sent',sent_at=now() WHERE client_msg_id=$1 AND status IN ('pending','accepted')`, client); e != nil {
		return e
	}
	return emit(ctx, tx, "message", map[string]any{"groupId": groupID, "msgId": msg, "clientMsgId": client, "isOwn": true})
}
func unreachable(ctx context.Context, tx pgx.Tx, groupID string) error {
	if _, e := tx.Exec(ctx, `UPDATE groups SET status='unreachable' WHERE id=$1`, groupID); e != nil {
		return e
	}
	if _, e := tx.Exec(ctx, `UPDATE messages SET delivery_status='cancelled',fail_code='GROUP_UNREACHABLE' WHERE group_id=$1 AND delivery_status='queued'`, groupID); e != nil {
		return e
	}
	rows, e := tx.Query(ctx, `UPDATE sequence_runs SET status='stopped' WHERE group_id=$1 AND status='running' RETURNING id,current_step_index`, groupID)
	if e != nil {
		return e
	}
	type run struct {
		id   string
		step int
	}
	rs := []run{}
	for rows.Next() {
		var r run
		rows.Scan(&r.id, &r.step)
		rs = append(rs, r)
	}
	rows.Close()
	for _, r := range rs {
		if e = emit(ctx, tx, "sequence_run", map[string]any{"runId": r.id, "groupId": groupID, "status": "stopped", "currentStepIndex": r.step}); e != nil {
			return e
		}
	}
	return nil
}
