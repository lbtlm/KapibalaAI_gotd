package platform

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (a *App) eventsStream(ctx context.Context) {
	defer a.wg.Done()
	for ctx.Err() == nil {
		a.streamOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(150 * time.Millisecond):
		}
	}
}
func (a *App) streamOnce(ctx context.Context) {
	var cursor int64
	if a.pool.QueryRow(ctx, `SELECT event_id FROM gateway_cursor WHERE id=1`).Scan(&cursor) != nil {
		return
	}
	req, e := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/events?since=%d", a.cfg.GatewayURL, cursor), nil)
	if e != nil {
		return
	}
	client := &http.Client{}
	res, e := client.Do(req)
	if e != nil {
		return
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return
	}
	scan := bufio.NewScanner(res.Body)
	scan.Buffer(make([]byte, 4096), 1<<20)
	var id int64
	var typ, data string
	for scan.Scan() {
		line := scan.Text()
		if line == "" {
			if id > 0 && data != "" {
				var raw json.RawMessage = []byte(data)
				if !json.Valid(raw) {
					continue
				}
				_, e = a.pool.Exec(ctx, `INSERT INTO gateway_inbox(event_id,type,data) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, id, typ, raw)
				if e != nil {
					a.noteInconsistency(ctx, "gateway_event", strconv.FormatInt(id, 10), "database write failed; replaying event from gateway history")
					return
				}
			}
			id = 0
			typ = ""
			data = ""
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch key {
		case "id":
			id, _ = strconv.ParseInt(value, 10, 64)
		case "event":
			typ = value
		case "data":
			if data != "" {
				data += "\n"
			}
			data += value
		}
	}
}
func (a *App) noteInconsistency(ctx context.Context, kind, ref, message string) {
	a.queueAlert(kind, ref, message)
	a.flushAlerts(ctx)
}
func (a *App) inboxTick(ctx context.Context) {
	a.lock(ctx, "inbox", func(ctx context.Context) error {
		// Only an eligible ordered prefix can execute: a younger known lower ID
		// must hold later IDs until their original order can be preserved.
		rows, e := a.pool.Query(ctx, `SELECT event_id FROM gateway_inbox WHERE NOT processed AND received_at<now()-interval '1100 milliseconds' AND event_id<COALESCE((SELECT min(event_id) FROM gateway_inbox WHERE NOT processed AND received_at>=now()-interval '1100 milliseconds'),9223372036854775807) ORDER BY event_id`)
		if e != nil {
			return e
		}
		ids := []int64{}
		for rows.Next() {
			var id int64
			rows.Scan(&id)
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			if e = a.processEvent(ctx, id); e != nil {
				a.noteInconsistency(ctx, "gateway_event", strconv.FormatInt(id, 10), "event transaction failed; retained inbox will retry")
				return e
			}
		}
		return nil
	})
}
func (a *App) processEvent(ctx context.Context, id int64) error {
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var typ string
	var data map[string]any
	var raw json.RawMessage
	e = tx.QueryRow(ctx, `SELECT type,data FROM gateway_inbox WHERE event_id=$1 AND NOT processed FOR UPDATE`, id).Scan(&typ, &raw)
	if e == pgx.ErrNoRows {
		return nil
	}
	if e != nil {
		return e
	}
	if e = json.Unmarshal(raw, &data); e != nil {
		return e
	}
	str := func(k string) string { v, _ := data[k].(string); return v }
	switch typ {
	case "message_sent":
		sent, err := time.Parse(time.RFC3339Nano, str("sentAt"))
		if err != nil {
			return err
		}
		e = markSent(ctx, tx, str("clientMsgId"), str("msgId"), sent)
	case "message":
		sent, err := time.Parse(time.RFC3339Nano, str("sentAt"))
		if err != nil {
			return err
		}
		var groupID string
		e = tx.QueryRow(ctx, `SELECT id FROM groups WHERE gateway_group_id=$1`, str("groupId")).Scan(&groupID)
		if e == pgx.ErrNoRows {
			e = nil
			break
		}
		if e != nil {
			return e
		}
		var own bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts WHERE platform_user_id=$1)`, str("senderPlatformUserId")).Scan(&own); e != nil {
			return e
		}
		tag, err := tx.Exec(ctx, `INSERT INTO messages(group_id,msg_id,sender_platform_user_id,is_own,text,sent_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(group_id,msg_id) DO NOTHING`, groupID, str("msgId"), str("senderPlatformUserId"), own, str("text"), sent)
		if err != nil {
			return err
		}
		if str("mediaUrl") != "" {
			_, e = tx.Exec(ctx, `UPDATE messages SET media_url=$3,media_kind=$4,media_mime=$5,media_name=$6,media_status='pending' WHERE group_id=$1 AND msg_id=$2 AND media_url IS NULL`, groupID, str("msgId"), str("mediaUrl"), str("mediaKind"), str("mediaMime"), str("mediaName"))
			if e != nil {
				return e
			}
		}
		if tag.RowsAffected() > 0 {
			if e = emit(ctx, tx, "message", map[string]any{"groupId": groupID, "msgId": str("msgId"), "isOwn": own}); e != nil {
				return e
			}
			if !own {
				_, e = tx.Exec(ctx, `INSERT INTO agent_pending(group_id,msg_id) SELECT id,$2 FROM groups WHERE id=$1 AND status='active' AND agent_enabled AND NOT marketing_paused ON CONFLICT DO NOTHING`, groupID, str("msgId"))
			}
		}
	case "message_failed":
		var groupID string
		var account *string
		e = tx.QueryRow(ctx, `SELECT group_id,account_id FROM messages WHERE client_msg_id=$1 FOR UPDATE`, str("clientMsgId")).Scan(&groupID, &account)
		if e == pgx.ErrNoRows {
			e = nil
			break
		}
		if e != nil {
			return e
		}
		code := str("code")
		if code == "GROUP_WRITE_FORBIDDEN" {
			e = unreachable(ctx, tx, groupID)
		} else if code == "ACCOUNT_SUSPENDED" && account != nil {
			var from string
			if e = tx.QueryRow(ctx, `SELECT status FROM accounts WHERE id=$1 FOR UPDATE`, *account).Scan(&from); e == pgx.ErrNoRows {
				e = nil
			} else if e == nil && accountTerminalError(from) == nil {
				e = transition(ctx, tx, *account, from, "suspended", nil)
			}
		}
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `UPDATE messages SET delivery_status='failed',fail_code=$2 WHERE client_msg_id=$1 AND delivery_status!='sent'`, str("clientMsgId"), code)
	case "member_joined":
		_, e = tx.Exec(ctx, `INSERT INTO members(group_id,platform_user_id,account_id,role) SELECT g.id,$2,a.id,'member' FROM groups g LEFT JOIN accounts a ON a.platform_user_id=$2 WHERE g.gateway_group_id=$1 AND (a.id IS NULL OR a.status NOT IN ('suspended','session_expired')) ON CONFLICT DO NOTHING`, str("groupId"), str("platformUserId"))
	case "member_left":
		_, e = tx.Exec(ctx, `DELETE FROM members WHERE group_id=(SELECT id FROM groups WHERE gateway_group_id=$1) AND platform_user_id=$2`, str("groupId"), str("platformUserId"))
	case "account_status":
		var from string
		var incarnation *string
		e = tx.QueryRow(ctx, `SELECT status,connection_id FROM accounts WHERE id=$1 FOR UPDATE`, str("accountId")).Scan(&from, &incarnation)
		if e == pgx.ErrNoRows {
			e = nil
			break
		}
		if e == nil {
			if str("status") == "offline" {
				if incarnation == nil || str("connectionId") == "" || *incarnation != str("connectionId") {
					break
				}
				if from == "online" || from == "rate_limited" {
					e = transition(ctx, tx, str("accountId"), from, "disconnected", nil)
					if e == nil {
						e = writeAccountLog(ctx, tx, str("accountId"), "gateway", "connection_ended", nil)
					}
				} else if from == "idle" || from == "disconnected" {
					_, e = tx.Exec(ctx, `UPDATE accounts SET connection_id=NULL WHERE id=$1 AND connection_id=$2`, str("accountId"), str("connectionId"))
				}
			} else {
				if accountTerminalError(from) != nil && accountTerminalError(str("status")) != nil {
					break
				}
				e = transition(ctx, tx, str("accountId"), from, str("status"), nil)
			}
		}
	}
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE gateway_inbox SET processed=true WHERE event_id=$1`, id); e != nil {
		return e
	}
	// The cursor stays behind every pending event. A 1.1s intake window covers the
	// gateway's <=1s reorder bound, including events committed after later IDs.
	_, e = tx.Exec(ctx, `UPDATE gateway_cursor SET event_id=GREATEST(event_id,COALESCE((SELECT max(event_id) FROM gateway_inbox WHERE processed AND received_at<now()-interval '1100 milliseconds' AND event_id<COALESCE((SELECT min(event_id) FROM gateway_inbox WHERE NOT processed),9223372036854775807)),0)) WHERE id=1`)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
