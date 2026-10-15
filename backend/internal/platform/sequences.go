package platform

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/domain"
	"net/http"
	"time"
)

type sequence struct {
	ID    string                `json:"id"`
	Name  string                `json:"name"`
	Steps []domain.SequenceStep `json:"steps"`
}

func (a *App) createSequence(w http.ResponseWriter, r *http.Request) error {
	var b sequence
	if e := decode(r, &b); e != nil {
		return e
	}
	if !(domain.SequenceDefinition{Name: b.Name, Steps: b.Steps}).Valid() {
		return fail(400, "VALIDATION_ERROR")
	}
	id := ID()
	status := 201
	var e error
	if r.Method == "PUT" {
		id = r.PathValue("id")
		status = 200
		tag, err := a.pool.Exec(r.Context(), `UPDATE sequences SET name=$2,steps=$3 WHERE id=$1`, id, b.Name, b.Steps)
		e = err
		if err == nil && tag.RowsAffected() == 0 {
			return fail(404, "SEQUENCE_NOT_FOUND")
		}
	} else {
		_, e = a.pool.Exec(r.Context(), `INSERT INTO sequences(id,name,steps) VALUES($1,$2,$3)`, id, b.Name, b.Steps)
	}
	if e != nil {
		return e
	}
	respond(w, status, map[string]any{"id": id})
	return nil
}
func (a *App) sequences(w http.ResponseWriter, r *http.Request) error {
	rows, e := a.pool.Query(r.Context(), `SELECT id,name,steps FROM sequences ORDER BY name,id`)
	if e != nil {
		return e
	}
	defer rows.Close()
	items := []sequence{}
	for rows.Next() {
		var s sequence
		var raw json.RawMessage
		if e = rows.Scan(&s.ID, &s.Name, &raw); e != nil {
			return e
		}
		if e = json.Unmarshal(raw, &s.Steps); e != nil {
			return e
		}
		items = append(items, s)
	}
	respond(w, 200, items)
	return rows.Err()
}

type sequenceInput struct {
	SequenceID       string                       `json:"sequenceId"`
	Vars             map[string]string            `json:"vars"`
	StepVars         map[string]map[string]string `json:"stepVars"`
	MemberAccountIDs []string                     `json:"memberAccountIds,omitempty"`
}

type boundStep struct {
	domain.ResolvedStep
	AccountID string `json:"accountId"`
}

func bindSequence(ctx context.Context, tx pgx.Tx, groupID string, input sequenceInput, steps []domain.ResolvedStep) ([]boundStep, error) {
	var admin *string
	if e := tx.QueryRow(ctx, `SELECT admin_account_id FROM groups WHERE id=$1 FOR SHARE`, groupID).Scan(&admin); e != nil {
		return nil, e
	}
	rows, e := tx.Query(ctx, `SELECT account_id,role FROM members WHERE group_id=$1 AND account_id IS NOT NULL ORDER BY account_id`, groupID)
	if e != nil {
		return nil, e
	}
	roles := map[string]string{}
	members := []string{}
	for rows.Next() {
		var id, role string
		if e = rows.Scan(&id, &role); e != nil {
			rows.Close()
			return nil, e
		}
		roles[id] = role
		if role == "member" {
			members = append(members, id)
		}
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if input.MemberAccountIDs != nil {
		members = []string{}
		seen := map[string]bool{}
		for _, id := range input.MemberAccountIDs {
			if roles[id] != "member" || seen[id] {
				return nil, fail(422, "MEMBER_NOT_IN_GROUP")
			}
			seen[id] = true
			members = append(members, id)
		}
	}
	bound := make([]boundStep, 0, len(steps))
	memberIndex := 0
	for _, step := range steps {
		account := ""
		if step.AccountRole == "admin" {
			if admin == nil || (roles[*admin] != "admin" && roles[*admin] != "creator") {
				return nil, fail(422, "ADMIN_REQUIRED")
			}
			account = *admin
		} else {
			if len(members) == 0 {
				return nil, fail(422, "MEMBER_REQUIRED")
			}
			account = members[memberIndex%len(members)]
			memberIndex++
		}
		bound = append(bound, boundStep{step, account})
	}
	return bound, nil
}

func (a *App) resolveSequence(ctx context.Context, b sequenceInput) ([]domain.ResolvedStep, error) {
	var raw json.RawMessage
	e := a.pool.QueryRow(ctx, `SELECT steps FROM sequences WHERE id=$1`, b.SequenceID).Scan(&raw)
	if e == pgx.ErrNoRows {
		return nil, fail(404, "SEQUENCE_NOT_FOUND")
	}
	if e != nil {
		return nil, e
	}
	var ss []domain.SequenceStep
	if e = json.Unmarshal(raw, &ss); e != nil {
		return nil, e
	}
	resolved, e := domain.Resolve(ss, b.Vars, b.StepVars)
	var missing *domain.PlaceholderError
	if errors.As(e, &missing) {
		return nil, &apiError{status: 422, code: "UNRESOLVED_PLACEHOLDER", message: missing.Error(), extra: map[string]any{"stepIndex": missing.StepIndex, "key": missing.Key}}
	}
	return resolved, e
}
func (a *App) previewSequence(w http.ResponseWriter, r *http.Request) error {
	if _, e := a.readGroup(r.Context(), r.PathValue("id")); e != nil {
		return e
	}
	var b sequenceInput
	if e := decode(r, &b); e != nil {
		return e
	}
	steps, e := a.resolveSequence(r.Context(), b)
	if e != nil {
		return e
	}
	tx, e := a.pool.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	bound, e := bindSequence(r.Context(), tx, r.PathValue("id"), b, steps)
	if e != nil {
		return e
	}
	respond(w, 200, map[string]any{"steps": bound})
	return nil
}
func (a *App) startSequence(w http.ResponseWriter, r *http.Request) error {
	var b sequenceInput
	if e := decode(r, &b); e != nil {
		return e
	}
	ctx := r.Context()
	steps, e := a.resolveSequence(ctx, b)
	if e != nil {
		return e
	}
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var groupStatus string
	e = tx.QueryRow(ctx, `SELECT status FROM groups WHERE id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&groupStatus)
	if e == pgx.ErrNoRows {
		return fail(404, "GROUP_NOT_FOUND")
	}
	if e != nil {
		return e
	}
	if groupStatus != "active" {
		return fail(409, "GROUP_UNREACHABLE")
	}
	var busy bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE group_id=$1 AND status='running')`, r.PathValue("id")).Scan(&busy); e != nil {
		return e
	}
	if busy {
		return fail(409, "GROUP_IN_USE")
	}
	bound, e := bindSequence(ctx, tx, r.PathValue("id"), b, steps)
	if e != nil {
		return e
	}
	id := ID()
	_, e = tx.Exec(ctx, `INSERT INTO sequence_runs(id,group_id,sequence_id) VALUES($1,$2,$3)`, id, r.PathValue("id"), b.SequenceID)
	var pg *pgconn.PgError
	if errors.As(e, &pg) && pg.Code == "23505" {
		return fail(409, "SEQUENCE_ALREADY_RUNNING")
	}
	if e != nil {
		return e
	}
	for i, s := range bound {
		var scheduled *time.Time
		if i == 0 {
			v := time.Now().Add(time.Duration(s.DelaySeconds * float64(time.Second)))
			scheduled = &v
		}
		_, e = tx.Exec(ctx, `INSERT INTO sequence_steps(run_id,index,account_role,text,delay_seconds,scheduled_at,resolved_vars,var_sources,account_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id, s.Index, s.AccountRole, s.Text, s.DelaySeconds, scheduled, s.ResolvedVars, s.VarSources, s.AccountID)
		if e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, `UPDATE groups SET sequence_config=$2 WHERE id=$1`, r.PathValue("id"), b); e != nil {
		return e
	}
	if e = emit(ctx, tx, "sequence_run", map[string]any{"runId": id, "groupId": r.PathValue("id"), "status": "running", "currentStepIndex": 1}); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	respond(w, 201, map[string]any{"runId": id})
	return nil
}

type sequenceRunStep struct {
	Index        int               `json:"index"`
	Text         string            `json:"text"`
	AccountID    *string           `json:"accountId"`
	AccountRole  string            `json:"accountRole"`
	Status       string            `json:"status"`
	ScheduledAt  *time.Time        `json:"scheduledAt"`
	SentAt       *time.Time        `json:"sentAt"`
	ClientMsgID  *string           `json:"clientMsgId"`
	ResolvedVars map[string]string `json:"resolvedVars"`
	VarSources   map[string]string `json:"varSources"`
}

func (a *App) sequenceRun(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var status string
	var index int
	e := a.pool.QueryRow(ctx, `SELECT status,current_step_index FROM sequence_runs WHERE id=$1`, r.PathValue("id")).Scan(&status, &index)
	if e == pgx.ErrNoRows {
		return fail(404, "SEQUENCE_RUN_NOT_FOUND")
	}
	if e != nil {
		return e
	}
	rows, e := a.pool.Query(ctx, `SELECT index,status,scheduled_at,sent_at,client_msg_id,resolved_vars,var_sources,text,account_id,account_role FROM sequence_steps WHERE run_id=$1 ORDER BY index`, r.PathValue("id"))
	if e != nil {
		return e
	}
	defer rows.Close()
	steps := []sequenceRunStep{}
	for rows.Next() {
		var s sequenceRunStep
		var v, source json.RawMessage
		if e = rows.Scan(&s.Index, &s.Status, &s.ScheduledAt, &s.SentAt, &s.ClientMsgID, &v, &source, &s.Text, &s.AccountID, &s.AccountRole); e != nil {
			return e
		}
		json.Unmarshal(v, &s.ResolvedVars)
		json.Unmarshal(source, &s.VarSources)
		steps = append(steps, s)
	}
	respond(w, 200, map[string]any{"status": status, "currentStepIndex": index, "steps": steps})
	return rows.Err()
}
func (a *App) sequenceTick(ctx context.Context) {
	for _, id := range a.ids(ctx, `SELECT id FROM sequence_runs WHERE status='running'`) {
		a.launchWorker(ctx, "sequence:"+id, func(ctx context.Context) error { return a.sequenceOne(ctx, id) })
	}
}
func (a *App) sequenceOne(ctx context.Context, id string) error {
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var groupID, status string
	var index int
	e = tx.QueryRow(ctx, `SELECT group_id,current_step_index,status FROM sequence_runs WHERE id=$1`, id).Scan(&groupID, &index, &status)
	if e != nil || status != "running" {
		return e
	}
	var groupStatus string
	e = tx.QueryRow(ctx, `SELECT status FROM groups WHERE id=$1 FOR UPDATE`, groupID).Scan(&groupStatus)
	if e != nil {
		return e
	}
	if groupStatus != "active" {
		return nil
	}
	if e = tx.QueryRow(ctx, `SELECT current_step_index,status FROM sequence_runs WHERE id=$1 FOR UPDATE`, id).Scan(&index, &status); e != nil {
		return e
	}
	if status != "running" {
		return nil
	}
	var stepStatus, role, text string
	var scheduled, sent *time.Time
	var client *string
	var delay float64
	e = tx.QueryRow(ctx, `SELECT status,account_role,text,scheduled_at,sent_at,client_msg_id,delay_seconds FROM sequence_steps WHERE run_id=$1 AND index=$2`, id, index).Scan(&stepStatus, &role, &text, &scheduled, &sent, &client, &delay)
	if e != nil {
		return e
	}
	if client == nil && stepStatus == "pending" && (scheduled == nil || time.Now().Before(*scheduled)) {
		return nil
	}
	if client != nil && stepStatus != "sent" && stepStatus != "skipped" {
		var delivery string
		e = tx.QueryRow(ctx, `SELECT delivery_status FROM messages WHERE client_msg_id=$1`, client).Scan(&delivery)
		if e != nil {
			return e
		}
		if delivery == "accepted" {
			_, e = tx.Exec(ctx, `UPDATE sequence_steps SET status='accepted' WHERE run_id=$1 AND index=$2 AND status='pending'`, id, index)
			if e != nil {
				return e
			}
			return tx.Commit(ctx)
		}
		if delivery == "failed" || delivery == "cancelled" {
			stepStatus = "failed"
			if _, e = tx.Exec(ctx, `UPDATE sequence_steps SET status='failed',sent_at=now() WHERE run_id=$1 AND index=$2`, id, index); e != nil {
				return e
			}
		} else {
			return nil
		}
	}
	if stepStatus == "sent" || stepStatus == "skipped" {
		var nextDelay float64
		e = tx.QueryRow(ctx, `SELECT delay_seconds FROM sequence_steps WHERE run_id=$1 AND index=$2`, id, index+1).Scan(&nextDelay)
		if e == pgx.ErrNoRows {
			status = "finished"
		} else if e != nil {
			return e
		} else {
			at := time.Now()
			if sent != nil {
				at = *sent
			}
			at = at.Add(time.Duration(nextDelay * float64(time.Second)))
			if _, e = tx.Exec(ctx, `UPDATE sequence_steps SET scheduled_at=$3 WHERE run_id=$1 AND index=$2 AND scheduled_at IS NULL`, id, index+1, at); e != nil {
				return e
			}
			index++
		}
	} else if stepStatus == "failed" {
		status = "failed"
	} else if client == nil && scheduled != nil && !time.Now().Before(*scheduled) {
		var account string
		e = tx.QueryRow(ctx, `SELECT a.id FROM accounts a JOIN members m ON m.account_id=a.id JOIN sequence_steps s ON s.account_id=a.id WHERE m.group_id=$1 AND s.run_id=$3 AND s.index=$4 AND a.status IN ('online','rate_limited') AND (($2='admin' AND m.role IN ('creator','admin')) OR ($2='member' AND m.role='member'))`, groupID, role, id, index).Scan(&account)
		if e == pgx.ErrNoRows {
			if _, e = tx.Exec(ctx, `UPDATE sequence_steps SET status='skipped',sent_at=now() WHERE run_id=$1 AND index=$2`, id, index); e != nil {
				return e
			}
		} else if e != nil {
			return e
		} else {
			msg, e := enqueue(ctx, tx, groupID, account, text)
			if e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `UPDATE sequence_steps SET client_msg_id=$3 WHERE run_id=$1 AND index=$2`, id, index, msg); e != nil {
				return e
			}
		}
	}
	if _, e = tx.Exec(ctx, `UPDATE sequence_runs SET status=$2,current_step_index=$3 WHERE id=$1`, id, status, index); e != nil {
		return e
	}
	if e = emit(ctx, tx, "sequence_run", map[string]any{"runId": id, "groupId": groupID, "status": status, "currentStepIndex": index}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (a *App) recoverSequences(ctx context.Context) error {
	_, e := a.pool.Exec(ctx, `UPDATE sequence_steps s SET scheduled_at=now()+make_interval(secs=>s.delay_seconds) FROM sequence_runs r JOIN sequence_steps cur ON cur.run_id=r.id AND cur.index=r.current_step_index WHERE s.run_id=r.id AND r.status='running' AND s.status='pending' AND s.client_msg_id IS NULL AND ((s.index=r.current_step_index AND s.scheduled_at<now()) OR (s.index=r.current_step_index+1 AND cur.status IN ('sent','skipped') AND COALESCE(s.scheduled_at,cur.sent_at+make_interval(secs=>s.delay_seconds))<now()))`)
	return e
}
