package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/domain"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

type agentStep struct {
	Kind          string          `json:"kind"`
	ToolUseID     *string         `json:"toolUseId"`
	Name          *string         `json:"name"`
	Input         json.RawMessage `json:"input"`
	ResultSummary string          `json:"resultSummary"`
	IsError       bool            `json:"isError"`
	ErrorCode     *string         `json:"errorCode"`
	AuditVerdict  *string         `json:"auditVerdict"`
	RawResponse   string          `json:"rawResponse"`
}
type pendingTool struct {
	Block         domain.Block `json:"block"`
	AuditAttempts int          `json:"auditAttempts"`
	AuditVerdict  string       `json:"auditVerdict"`
	Stage         string       `json:"stage"`
}
type agentRun struct {
	ID             string               `json:"id"`
	GroupID        string               `json:"groupId"`
	Status         string               `json:"status"`
	EndReason      *string              `json:"endReason"`
	Summary        *string              `json:"summary"`
	Steps          []agentStep          `json:"steps"`
	History        []domain.ChatMessage `json:"-"`
	Pending        *pendingTool         `json:"-"`
	ProtocolErrors int                  `json:"-"`
	ElapsedMS      int64                `json:"-"`
	Epoch          int64                `json:"-"`
}

func (a *App) loadAgent(ctx context.Context, id string) (agentRun, error) {
	var r agentRun
	var history, steps, pending json.RawMessage
	e := a.pool.QueryRow(ctx, `SELECT id,group_id,status,end_reason,summary,history,steps,pending,protocol_errors,elapsed_ms+CASE WHEN status='running' THEN GREATEST(0,(extract(epoch FROM (now()-budget_checkpoint))*1000)::bigint) ELSE 0 END,execution_epoch FROM agent_runs WHERE id=$1`, id).Scan(&r.ID, &r.GroupID, &r.Status, &r.EndReason, &r.Summary, &history, &steps, &pending, &r.ProtocolErrors, &r.ElapsedMS, &r.Epoch)
	if e == pgx.ErrNoRows {
		return r, fail(404, "AGENT_RUN_NOT_FOUND")
	}
	if e != nil {
		return r, e
	}
	if e = json.Unmarshal(history, &r.History); e != nil {
		return r, e
	}
	if e = json.Unmarshal(steps, &r.Steps); e != nil {
		return r, e
	}
	if len(pending) > 0 {
		e = json.Unmarshal(pending, &r.Pending)
	}
	return r, e
}
func (a *App) agentGet(w http.ResponseWriter, r *http.Request) error {
	run, e := a.loadAgent(r.Context(), r.PathValue("id"))
	if e != nil {
		return e
	}
	respond(w, 200, run)
	return nil
}
func (a *App) agentList(w http.ResponseWriter, r *http.Request) error {
	rows, e := a.pool.Query(r.Context(), `SELECT id FROM agent_runs WHERE group_id=$1 ORDER BY created_at DESC,id DESC LIMIT 50`, r.PathValue("id"))
	if e != nil {
		return e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	runs := []agentRun{}
	for _, id := range ids {
		run, e := a.loadAgent(r.Context(), id)
		if e != nil {
			return e
		}
		runs = append(runs, run)
	}
	respond(w, 200, runs)
	return nil
}
func (a *App) saveAgent(ctx context.Context, r *agentRun) error {
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, `UPDATE agent_runs SET status=$2,end_reason=$3,summary=$4,history=$5,steps=$6,pending=$7,protocol_errors=$8 WHERE id=$1 AND status='running' AND execution_epoch=$9`, r.ID, r.Status, r.EndReason, r.Summary, r.History, r.Steps, r.Pending, r.ProtocolErrors, r.Epoch)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("agent execution fence lost")
	}
	if e = emit(ctx, tx, "agent_run", map[string]any{"runId": r.ID, "groupId": r.GroupID, "status": r.Status, "endReason": r.EndReason}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
func truncateBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
func endAgent(r *agentRun, reason, summary string) {
	r.EndReason = &reason
	if summary != "" {
		r.Summary = &summary
	}
	switch reason {
	case "final":
		r.Status = "finished"
	case "audit_blocked":
		r.Status = "blocked"
	case "cancelled":
		r.Status = "cancelled"
	default:
		r.Status = "failed"
	}
}
func (a *App) createAgentRuns(ctx context.Context) {
	for _, groupID := range a.ids(ctx, `SELECT DISTINCT group_id FROM agent_pending`) {
		tx, e := a.pool.Begin(ctx)
		if e != nil {
			return
		}
		var enabled bool
		var status string
		e = tx.QueryRow(ctx, `SELECT agent_enabled,status FROM groups WHERE id=$1 FOR UPDATE`, groupID).Scan(&enabled, &status)
		if e != nil {
			tx.Rollback(ctx)
			continue
		}
		if !enabled || status != "active" {
			tx.Exec(ctx, `DELETE FROM agent_pending WHERE group_id=$1`, groupID)
			tx.Commit(ctx)
			continue
		}
		var active bool
		tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_runs WHERE group_id=$1 AND status='running')`, groupID).Scan(&active)
		if active {
			tx.Rollback(ctx)
			continue
		}
		rows, e := tx.Query(ctx, `SELECT m.msg_id,m.sender_platform_user_id,m.text,m.sent_at FROM messages m JOIN agent_pending p ON p.group_id=m.group_id AND p.msg_id=m.msg_id WHERE p.group_id=$1 ORDER BY m.sent_at,m.id`, groupID)
		if e != nil {
			tx.Rollback(ctx)
			continue
		}
		trigger := []map[string]any{}
		for rows.Next() {
			var msg, sender, text string
			var sent time.Time
			rows.Scan(&msg, &sender, &text, &sent)
			trigger = append(trigger, map[string]any{"msgId": msg, "senderPlatformUserId": sender, "text": text, "sentAt": sent})
		}
		rows.Close()
		var autoKick bool
		tx.QueryRow(ctx, `SELECT auto_kick_enabled FROM groups WHERE id=$1`, groupID).Scan(&autoKick)
		own := []string{}
		rows, e = tx.Query(ctx, `SELECT platform_user_id FROM accounts WHERE platform_user_id IS NOT NULL ORDER BY id`)
		if e != nil {
			tx.Rollback(ctx)
			continue
		}
		for rows.Next() {
			var p string
			rows.Scan(&p)
			own = append(own, p)
		}
		rows.Close()
		payload, _ := json.Marshal(map[string]any{"groupId": groupID, "triggerMessages": trigger, "policy": map[string]any{"autoKickEnabled": autoKick}, "ownPlatformUserIds": own})
		id := ID()
		history := []domain.ChatMessage{{Role: "user", Content: []domain.Block{{Type: "text", Text: string(payload)}}}}
		if _, e = tx.Exec(ctx, `INSERT INTO agent_runs(id,group_id,history) VALUES($1,$2,$3)`, id, groupID, history); e == nil {
			_, e = tx.Exec(ctx, `DELETE FROM agent_pending WHERE group_id=$1`, groupID)
		}
		if e == nil {
			e = emit(ctx, tx, "agent_run", map[string]any{"runId": id, "groupId": groupID, "status": "running", "endReason": nil})
		}
		if e == nil {
			tx.Commit(ctx)
		} else {
			tx.Rollback(ctx)
		}
	}
}
func (a *App) agentTick(ctx context.Context) {
	a.createAgentRuns(ctx)
	for _, id := range a.ids(ctx, `SELECT id FROM agent_runs WHERE status='running'`) {
		a.wg.Add(1)
		go func(id string) {
			defer a.wg.Done()
			if e := a.lock(ctx, "agent:"+id, func(ctx context.Context) error { return a.executeAgent(ctx, id) }); e != nil && ctx.Err() == nil {
				slog.Warn("agent checkpoint retrying", "runId", id, "error", e)
			}
		}(id)
	}
}
func (a *App) turn(ctx context.Context, r *agentRun) ([]byte, string) {
	ctx, cancel := context.WithTimeout(ctx, a.cfg.TurnTimeout)
	defer cancel()
	payload, _ := json.Marshal(domain.TurnRequest{RunID: r.ID, Tools: domain.Tools(), Messages: r.History})
	req, e := http.NewRequestWithContext(ctx, "POST", a.cfg.AgentURL+"/agent/turn", bytes.NewReader(payload))
	if e != nil {
		return nil, "BAD_JSON"
	}
	req.Header.Set("Content-Type", "application/json")
	res, e := a.client.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return nil, "TURN_TIMEOUT"
		}
		return nil, "BAD_JSON"
	}
	defer res.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if e != nil || res.StatusCode < 200 || res.StatusCode >= 300 {
		return raw, "BAD_JSON"
	}
	return raw, ""
}
func (a *App) executeAgent(ctx context.Context, id string) error {
	var claimedEpoch int64
	if e := a.pool.QueryRow(ctx, `UPDATE agent_runs SET execution_epoch=execution_epoch+1 WHERE id=$1 AND status='running' RETURNING execution_epoch`, id).Scan(&claimedEpoch); e != nil {
		return e
	}
	r, e := a.loadAgent(ctx, id)
	if e != nil || r.Status != "running" {
		return e
	}
	if r.Epoch != claimedEpoch {
		return fmt.Errorf("agent execution fence lost before recovery")
	}
	activeAt := time.Now()
	baseElapsed := r.ElapsedMS
	parent := ctx
	remaining := 60000 - r.ElapsedMS
	if remaining <= 0 {
		endAgent(&r, "wall_clock", "")
		return a.saveAgent(ctx, &r)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(remaining)*time.Millisecond)
	defer cancel()
	defer func() {
		if ctx.Err() == context.DeadlineExceeded && parent.Err() == nil && r.Status == "running" {
			r.ElapsedMS = 60000
			endAgent(&r, "wall_clock", "")
			a.saveAgent(parent, &r)
		}
	}()
	checkpoint := func() error {
		r.ElapsedMS = baseElapsed + time.Since(activeAt).Milliseconds()
		return a.saveAgent(ctx, &r)
	}
	for ctx.Err() == nil && r.Status == "running" {
		var enabled bool
		var groupStatus string
		if e = a.pool.QueryRow(ctx, `SELECT agent_enabled,status FROM groups WHERE id=$1`, r.GroupID).Scan(&enabled, &groupStatus); e != nil {
			return e
		}
		if r.Pending == nil {
			if !enabled || groupStatus != "active" {
				endAgent(&r, "cancelled", "")
				return checkpoint()
			}
			if len(r.Steps) >= 12 {
				endAgent(&r, "budget_exhausted", "")
				return checkpoint()
			}
			if baseElapsed+time.Since(activeAt).Milliseconds() >= 60000 {
				endAgent(&r, "wall_clock", "")
				return checkpoint()
			}
			if r.ProtocolErrors >= 3 {
				endAgent(&r, "protocol_errors", "")
				return checkpoint()
			}
			raw, code := a.turn(ctx, &r)
			if ctx.Err() != nil {
				if ctx.Err() == context.DeadlineExceeded && parent.Err() == nil {
					timeout := "TURN_TIMEOUT"
					r.Steps = append(r.Steps, agentStep{Kind: "protocol_error", ResultSummary: timeout, IsError: true, ErrorCode: &timeout, RawResponse: truncateBytes(string(raw), 2048)})
					endAgent(&r, "wall_clock", "")
					return a.saveAgent(parent, &r)
				}
				return ctx.Err()
			}
			out, err := domain.ParseTurn(raw)
			if code == "" && err != nil {
				code = "BAD_JSON"
			}
			if code == "" && out.StopReason == "tool_use" {
				for _, s := range r.Steps {
					if s.ToolUseID != nil && *s.ToolUseID == out.Content[0].ID {
						code = "DUPLICATE_TOOL_USE_ID"
						break
					}
				}
			}
			if code != "" {
				r.Steps = append(r.Steps, agentStep{Kind: "protocol_error", ResultSummary: code, IsError: true, ErrorCode: &code, RawResponse: truncateBytes(string(raw), 2048)})
				r.History = append(r.History, domain.ChatMessage{Role: "user", Content: []domain.Block{{Type: "text", Text: "PROTOCOL_ERROR " + code + ": invalid agent response"}}})
				r.ProtocolErrors++
				if e = checkpoint(); e != nil {
					return e
				}
				continue
			}
			b := out.Content[0]
			if out.StopReason == "end_turn" {
				r.ProtocolErrors = 0
				r.Steps = append(r.Steps, agentStep{Kind: "final", ResultSummary: truncateRunes(b.Text, 200), RawResponse: truncateBytes(string(raw), 2048)})
				endAgent(&r, "final", b.Text)
				return checkpoint()
			}
			r.History = append(r.History, domain.ChatMessage{Role: "assistant", Content: []domain.Block{b}})
			toolID, name := b.ID, b.Name
			r.Steps = append(r.Steps, agentStep{Kind: "tool_use", ToolUseID: &toolID, Name: &name, Input: b.Input, RawResponse: truncateBytes(string(raw), 2048)})
			r.Pending = &pendingTool{Block: b, Stage: "validate"}
			if e = checkpoint(); e != nil {
				return e
			}
		}
		p := r.Pending
		b := p.Block
		code := domain.ValidateInput(b.Name, b.Input)
		var result map[string]any
		if code != "" {
			r.ProtocolErrors++
			result = toolError(code)
		} else if b.Name == "finish" {
			r.ProtocolErrors = 0
			var in struct {
				Summary string `json:"summary"`
			}
			json.Unmarshal(b.Input, &in)
			result = map[string]any{"ok": true}
			endAgent(&r, "final", in.Summary)
		} else {
			r.ProtocolErrors = 0
			// A previously accepted idempotency key bypasses audit and returns the
			// original durable effect. Rejected audit calls never consume the key.
			var input map[string]any
			json.Unmarshal(b.Input, &input)
			cached := false
			if b.Name == "send_message" {
				var client string
				e = a.pool.QueryRow(ctx, `SELECT client_msg_id FROM agent_effects WHERE run_id=$1 AND key=$2`, r.ID, "send:"+input["idempotency_key"].(string)).Scan(&client)
				if e == nil {
					result, e = a.currentAgentSend(ctx, client)
					if e != nil {
						return e
					}
					cached = true
				} else if e != pgx.ErrNoRows {
					return e
				}
			}
			if !cached && (b.Name == "send_message" || b.Name == "kick_user") {
				if b.Name == "kick_user" {
					var allowed bool
					e = a.pool.QueryRow(ctx, `SELECT auto_kick_enabled FROM groups WHERE id=$1`, r.GroupID).Scan(&allowed)
					if e != nil {
						return e
					}
					if !allowed {
						code = "POLICY_DENIED"
						result = toolError(code)
						cached = true
					}
				}
				if !cached {
					auditText, _ := input["text"].(string)
					if b.Name == "kick_user" {
						data, _ := json.Marshal(map[string]any{"action": "kick", "platform_user_id": input["platform_user_id"], "reason": input["reason"]})
						auditText = string(data)
					}
					for p.AuditVerdict == "" && p.AuditAttempts < 3 {
						p.AuditAttempts++
						if e = checkpoint(); e != nil {
							return e
						}
						verdict := a.audit(ctx, auditText, r.GroupID)
						if ctx.Err() != nil {
							return ctx.Err()
						}
						if verdict != "" {
							p.AuditVerdict = verdict
							last := &r.Steps[len(r.Steps)-1]
							last.AuditVerdict = &p.AuditVerdict
						}
						if e = checkpoint(); e != nil {
							return e
						}
						if baseElapsed+time.Since(activeAt).Milliseconds() >= 60000 {
							endAgent(&r, "wall_clock", "")
							return checkpoint()
						}
					}
					if p.AuditVerdict == "" {
						last := &r.Steps[len(r.Steps)-1]
						last.ResultSummary = "Audit did not produce a valid verdict after three attempts"
						last.IsError = true
						blocked := "AUDIT_REJECTED"
						last.ErrorCode = &blocked
						endAgent(&r, "audit_blocked", "")
						return checkpoint()
					}
					if p.AuditVerdict == "fail" {
						code = "AUDIT_REJECTED"
						result = toolError(code)
						cached = true
					}
				}
			}
			if !cached {
				switch b.Name {
				case "get_recent_messages":
					result, e = a.recentMessages(ctx, r.GroupID, int(input["limit"].(float64)))
					if e != nil {
						return e
					}
				case "send_message":
					result, code, e = a.agentSend(ctx, &r, input)
					if e != nil {
						return e
					}
				case "kick_user":
					result, code, e = a.agentKick(ctx, &r, b, input)
					if e != nil {
						return e
					}
				}
			}
		}
		if code != "" && result == nil {
			result = toolError(code)
		}
		content, _ := json.Marshal(result)
		if len(content) > 8192 {
			content = []byte(`{"truncated":true}`)
		}
		step := &r.Steps[len(r.Steps)-1]
		step.ResultSummary = truncateRunes(string(content), 200)
		step.IsError = code != ""
		if code != "" {
			step.ErrorCode = &code
		}
		r.History = append(r.History, domain.ChatMessage{Role: "user", Content: []domain.Block{{Type: "tool_result", ToolUseID: b.ID, Content: string(content), IsError: code != ""}}})
		r.Pending = nil
		if e = checkpoint(); e != nil {
			return e
		}
	}
	return ctx.Err()
}
func toolError(code string) map[string]any {
	return map[string]any{"code": code, "message": strings.ReplaceAll(code, "_", " ")}
}
func (a *App) audit(ctx context.Context, text, groupID string) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var out struct {
		Verdict string `json:"verdict"`
	}
	e := a.request(ctx, a.cfg.AgentURL, "POST", "/agent/audit", map[string]any{"text": text, "groupId": groupID}, &out)
	if e == nil && (out.Verdict == "pass" || out.Verdict == "fail") {
		return out.Verdict
	}
	return ""
}
func (a *App) recentMessages(ctx context.Context, groupID string, limit int) (map[string]any, error) {
	if limit > 50 {
		limit = 50
	}
	if limit < 1 {
		limit = 1
	}
	rows, e := a.pool.Query(ctx, `SELECT msg_id,sender_platform_user_id,is_own,text,sent_at FROM messages WHERE group_id=$1 AND msg_id IS NOT NULL ORDER BY sent_at DESC,id DESC LIMIT $2`, groupID, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	items := []map[string]any{}
	truncated := false
	for rows.Next() {
		var msg, sender, text string
		var own bool
		var sent time.Time
		if e = rows.Scan(&msg, &sender, &own, &text, &sent); e != nil {
			return nil, e
		}
		if utf8.RuneCountInString(text) > 500 {
			text = truncateRunes(text, 500)
			truncated = true
		}
		items = append(items, map[string]any{"msgId": msg, "senderPlatformUserId": sender, "isOwn": own, "text": text, "sentAt": sent})
	}
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	for {
		payload := map[string]any{"messages": items, "truncated": truncated}
		raw, _ := json.Marshal(payload)
		if len(raw) <= 8192 {
			return payload, rows.Err()
		}
		items = items[1:]
		truncated = true
	}
}
func (a *App) waitAgentSend(ctx context.Context, client string) (map[string]any, string) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		var status string
		var code *string
		e := a.pool.QueryRow(ctx, `SELECT delivery_status,fail_code FROM messages WHERE client_msg_id=$1`, client).Scan(&status, &code)
		if e != nil {
			return toolError("SEND_TIMEOUT"), "SEND_TIMEOUT"
		}
		if status == "accepted" || status == "sent" {
			return map[string]any{"clientMsgId": client, "deliveryStatus": status}, ""
		}
		if status == "failed" || status == "cancelled" {
			c := "SEND_FAILED"
			if code != nil && (*code == "GROUP_WRITE_FORBIDDEN" || *code == "GROUP_UNREACHABLE") {
				c = "GROUP_UNREACHABLE"
			}
			return toolError(c), c
		}
		if time.Now().After(deadline) {
			return toolError("SEND_TIMEOUT"), "SEND_TIMEOUT"
		}
		select {
		case <-ctx.Done():
			return toolError("SEND_TIMEOUT"), "SEND_TIMEOUT"
		case <-time.After(40 * time.Millisecond):
		}
	}
}
func (a *App) agentSend(ctx context.Context, r *agentRun, input map[string]any) (map[string]any, string, error) {
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return nil, "", e
	}
	defer tx.Rollback(ctx)
	if e = agentFence(ctx, tx, r); e != nil {
		return nil, "", e
	}
	key := "send:" + input["idempotency_key"].(string)
	var existing string
	e = tx.QueryRow(ctx, `SELECT client_msg_id FROM agent_effects WHERE run_id=$1 AND key=$2`, r.ID, key).Scan(&existing)
	if e == nil {
		result, e := a.currentAgentSend(ctx, existing)
		return result, "", e
	}
	if e != pgx.ErrNoRows {
		return nil, "", e
	}
	var account string
	e = tx.QueryRow(ctx, `SELECT a.id FROM accounts a JOIN members m ON m.account_id=a.id WHERE m.group_id=$1 AND a.status='online' ORDER BY a.id LIMIT 1`, r.GroupID).Scan(&account)
	if e == pgx.ErrNoRows {
		return toolError("NO_AVAILABLE_ACCOUNT"), "NO_AVAILABLE_ACCOUNT", nil
	}
	if e != nil {
		return nil, "", e
	}
	client, e := enqueue(ctx, tx, r.GroupID, account, input["text"].(string))
	if e != nil {
		return toolError("SEND_FAILED"), "SEND_FAILED", nil
	}
	_, e = tx.Exec(ctx, `INSERT INTO agent_effects(run_id,key,kind,state,client_msg_id,input) VALUES($1,$2,'send','enqueued',$3,$4)`, r.ID, key, client, input)
	if e != nil {
		return nil, "", e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, "", e
	}
	result, code := a.waitAgentSend(ctx, client)
	return result, code, nil
}
func (a *App) agentKick(ctx context.Context, r *agentRun, b domain.Block, input map[string]any) (map[string]any, string, error) {
	key := "kick:" + b.ID
	var state string
	var result json.RawMessage
	var started time.Time
	e := a.pool.QueryRow(ctx, `SELECT state,result,started_at FROM agent_effects WHERE run_id=$1 AND key=$2`, r.ID, key).Scan(&state, &result, &started)
	if e == nil && state == "done" {
		var out map[string]any
		json.Unmarshal(result, &out)
		return out, "", nil
	}
	if e != nil && e != pgx.ErrNoRows {
		return nil, "", e
	}
	g, e := a.readGroup(ctx, r.GroupID)
	if e != nil {
		return nil, "", e
	}
	target := input["platform_user_id"].(string)
	if state != "" {
		var members []struct {
			PlatformUserID string `json:"platformUserId"`
		}
		for {
			e = a.gateway(ctx, "GET", "/groups/"+*g.GatewayGroupID+"/members", nil, &members)
			if e != nil {
				select {
				case <-ctx.Done():
					return nil, "", ctx.Err()
				case <-time.After(100 * time.Millisecond):
				}
				continue
			}
			present := false
			for _, m := range members {
				if m.PlatformUserID == target {
					present = true
				}
			}
			if !present {
				out := map[string]any{"kicked": true}
				_, e = a.pool.Exec(ctx, `UPDATE agent_effects SET state='done',result=$3 WHERE run_id=$1 AND key=$2`, r.ID, key, out)
				return out, "", e
			}
			if time.Since(started) >= 14*time.Second {
				break
			}
			select {
			case <-ctx.Done():
				return nil, "", ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	var account string
	e = a.pool.QueryRow(ctx, `SELECT a.id FROM accounts a JOIN members m ON m.account_id=a.id WHERE m.group_id=$1 AND a.status='online' AND m.role IN ('creator','admin') ORDER BY CASE WHEN m.role='admin' THEN 0 ELSE 1 END,a.id LIMIT 1`, r.GroupID).Scan(&account)
	if e == pgx.ErrNoRows {
		return toolError("NO_AVAILABLE_ACCOUNT"), "NO_AVAILABLE_ACCOUNT", nil
	}
	if e != nil {
		return nil, "", e
	}
	if state == "" {
		tx, e := a.pool.Begin(ctx)
		if e != nil {
			return nil, "", e
		}
		if e = agentFence(ctx, tx, r); e != nil {
			tx.Rollback(ctx)
			return nil, "", e
		}
		tag, e := tx.Exec(ctx, `INSERT INTO agent_effects(run_id,key,kind,state,input) VALUES($1,$2,'kick','inflight',$3) ON CONFLICT DO NOTHING`, r.ID, key, input)
		if e != nil {
			tx.Rollback(ctx)
			return nil, "", e
		}
		if e = tx.Commit(ctx); e != nil {
			return nil, "", e
		}
		if tag.RowsAffected() == 0 {
			return a.agentKick(ctx, r, b, input)
		}
	}
	e = a.gateway(ctx, "POST", "/groups/"+*g.GatewayGroupID+"/kick", map[string]any{"byAccountId": account, "targetPlatformUserId": target}, nil)
	if e != nil {
		code := externalCode(e)
		if code == "NETWORK_TIMEOUT" || code == "GATEWAY_UNAVAILABLE" {
			_, e = a.pool.Exec(ctx, `UPDATE agent_effects SET started_at=now()-interval '12 seconds' WHERE run_id=$1 AND key=$2`, r.ID, key)
			if e != nil {
				return nil, "", e
			}
			return a.agentKick(ctx, r, b, input)
		}
		if code != "OWNER_LEFT" && code != "NO_PERMISSION" {
			code = "SEND_FAILED"
		}
		return toolError(code), code, nil
	}
	out := map[string]any{"kicked": true}
	_, e = a.pool.Exec(ctx, `UPDATE agent_effects SET state='done',result=$3 WHERE run_id=$1 AND key=$2`, r.ID, key, out)
	return out, "", e
}
