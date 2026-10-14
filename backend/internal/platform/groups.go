package platform

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"time"
)

type member struct {
	AccountID      *string `json:"accountId"`
	PlatformUserID string  `json:"platformUserId"`
	Role           string  `json:"role"`
}
type group struct {
	ID                  string   `json:"id"`
	GatewayGroupID      *string  `json:"gatewayGroupId"`
	Status              string   `json:"status"`
	CreatorAccountID    string   `json:"creatorAccountId"`
	AgentEnabled        bool     `json:"agentEnabled"`
	AutoKickEnabled     bool     `json:"autoKickEnabled"`
	Members             []member `json:"members"`
	ActiveSequenceRunID *string  `json:"activeSequenceRunId"`
	ActiveAgentRunID    *string  `json:"activeAgentRunId"`
}

func (a *App) readGroup(ctx context.Context, id string) (group, error) {
	var g group
	e := a.pool.QueryRow(ctx, `SELECT id,gateway_group_id,status,COALESCE(creator_account_id,''),agent_enabled,auto_kick_enabled,(SELECT id FROM sequence_runs WHERE group_id=groups.id AND status='running'),(SELECT id FROM agent_runs WHERE group_id=groups.id AND status='running') FROM groups WHERE id=$1`, id).Scan(&g.ID, &g.GatewayGroupID, &g.Status, &g.CreatorAccountID, &g.AgentEnabled, &g.AutoKickEnabled, &g.ActiveSequenceRunID, &g.ActiveAgentRunID)
	if e == pgx.ErrNoRows {
		return g, fail(404, "GROUP_NOT_FOUND")
	}
	if e != nil {
		return g, e
	}
	g.Members = []member{}
	rows, e := a.pool.Query(ctx, `SELECT account_id,platform_user_id,role FROM members WHERE group_id=$1 ORDER BY role,platform_user_id`, id)
	if e != nil {
		return g, e
	}
	defer rows.Close()
	for rows.Next() {
		var m member
		if e = rows.Scan(&m.AccountID, &m.PlatformUserID, &m.Role); e != nil {
			return g, e
		}
		g.Members = append(g.Members, m)
	}
	return g, rows.Err()
}
func (a *App) groupGet(w http.ResponseWriter, r *http.Request) error {
	g, e := a.readGroup(r.Context(), r.PathValue("id"))
	if e != nil {
		return e
	}
	respond(w, 200, g)
	return nil
}
func (a *App) groups(w http.ResponseWriter, r *http.Request) error {
	rows, e := a.pool.Query(r.Context(), `SELECT id FROM groups ORDER BY id`)
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
	gs := []group{}
	for _, id := range ids {
		g, e := a.readGroup(r.Context(), id)
		if e != nil {
			return e
		}
		gs = append(gs, g)
	}
	respond(w, 200, gs)
	return nil
}
func (a *App) patchGroup(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		AgentEnabled    *bool `json:"agentEnabled"`
		AutoKickEnabled *bool `json:"autoKickEnabled"`
	}
	if e := decode(r, &b); e != nil {
		return e
	}
	tag, e := a.pool.Exec(r.Context(), `UPDATE groups SET agent_enabled=COALESCE($2,agent_enabled),auto_kick_enabled=COALESCE($3,auto_kick_enabled) WHERE id=$1`, r.PathValue("id"), b.AgentEnabled, b.AutoKickEnabled)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return fail(404, "GROUP_NOT_FOUND")
	}
	return a.groupGet(w, r)
}

type jobState struct {
	Creator         string     `json:"creator"`
	Members         []string   `json:"members"`
	Phase           string     `json:"phase"`
	Position        int        `json:"position"`
	Invite          string     `json:"invite"`
	ReadyAt         time.Time  `json:"readyAt"`
	JoinStarted     *time.Time `json:"joinStarted"`
	ExpiredRetries  int        `json:"expiredRetries"`
	PromoteAttempts int        `json:"promoteAttempts"`
	WaitUntil       time.Time  `json:"waitUntil"`
}
type jobError struct {
	Step string `json:"step"`
	Code string `json:"code"`
}

func (a *App) createGroup(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		CreatorAccountID string   `json:"creatorAccountId"`
		MemberAccountIDs []string `json:"memberAccountIds"`
	}
	if e := decode(r, &b); e != nil {
		return e
	}
	if b.CreatorAccountID == "" || len(b.MemberAccountIDs) == 0 {
		return fail(400, "VALIDATION_ERROR")
	}
	seen := map[string]bool{b.CreatorAccountID: true}
	ids := []string{b.CreatorAccountID}
	for _, id := range b.MemberAccountIDs {
		if id == "" || seen[id] {
			return fail(400, "VALIDATION_ERROR")
		}
		seen[id] = true
		ids = append(ids, id)
	}
	ctx := r.Context()
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	for _, id := range ids {
		var st string
		e = tx.QueryRow(ctx, `SELECT status FROM accounts WHERE id=$1 FOR UPDATE`, id).Scan(&st)
		if e != nil || st != "online" {
			return fail(422, "ACCOUNT_NOT_ONLINE")
		}
	}
	job := ID()
	_, e = tx.Exec(ctx, `INSERT INTO jobs(id,kind,state) VALUES($1,'create',$2)`, job, jobState{Creator: b.CreatorAccountID, Members: b.MemberAccountIDs, Phase: "create"})
	if e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	respond(w, 202, map[string]any{"jobId": job})
	return nil
}
func (a *App) jobGet(w http.ResponseWriter, r *http.Request) error {
	var st string
	var groupID *string
	var es json.RawMessage
	e := a.pool.QueryRow(r.Context(), `SELECT status,errors,group_id FROM jobs WHERE id=$1`, r.PathValue("id")).Scan(&st, &es, &groupID)
	if e == pgx.ErrNoRows {
		return fail(404, "JOB_NOT_FOUND")
	}
	if e != nil {
		return e
	}
	respond(w, 200, map[string]any{"status": st, "errors": es, "groupId": groupID})
	return nil
}
func (a *App) leaveAll(w http.ResponseWriter, r *http.Request) error {
	g, e := a.readGroup(r.Context(), r.PathValue("id"))
	if e != nil {
		return e
	}
	ids := []string{}
	for _, m := range g.Members {
		if m.AccountID != nil && *m.AccountID != g.CreatorAccountID {
			ids = append(ids, *m.AccountID)
		}
	}
	if g.CreatorAccountID != "" {
		ids = append(ids, g.CreatorAccountID)
	}
	job := ID()
	_, e = a.pool.Exec(r.Context(), `INSERT INTO jobs(id,kind,group_id,state) VALUES($1,'leave',$2,$3)`, job, g.ID, jobState{Creator: g.CreatorAccountID, Members: ids, Phase: "leave"})
	if e != nil {
		return e
	}
	respond(w, 202, map[string]any{"jobId": job})
	return nil
}
func (a *App) jobSave(ctx context.Context, id string, s jobState) error {
	_, e := a.pool.Exec(ctx, `UPDATE jobs SET state=$2 WHERE id=$1`, id, s)
	return e
}
func (a *App) jobFail(ctx context.Context, id, step, code string) error {
	_, e := a.pool.Exec(ctx, `UPDATE jobs SET status='failed',errors=errors||$2::jsonb WHERE id=$1`, id, []jobError{{step, code}})
	return e
}
func (a *App) jobsTick(ctx context.Context) {
	ids := a.ids(ctx, `SELECT id FROM jobs WHERE status='running' ORDER BY created_at`)
	for _, id := range ids {
		a.lock(ctx, "job:"+id, func(ctx context.Context) error { return a.runJob(ctx, id) })
	}
}
func (a *App) runJob(ctx context.Context, id string) error {
	var kind string
	var groupID *string
	var raw json.RawMessage
	var errs []jobError
	var errRaw json.RawMessage
	e := a.pool.QueryRow(ctx, `SELECT kind,group_id,state,errors FROM jobs WHERE id=$1 AND status='running'`, id).Scan(&kind, &groupID, &raw, &errRaw)
	if e != nil {
		return e
	}
	var s jobState
	if e = json.Unmarshal(raw, &s); e != nil {
		return e
	}
	json.Unmarshal(errRaw, &errs)
	if time.Now().Before(s.WaitUntil) {
		return nil
	}
	if kind == "leave" {
		return a.runLeave(ctx, id, groupID, s, errs)
	}
	switch s.Phase {
	case "create":
		var out struct {
			GroupID string `json:"groupId"`
		}
		if e = a.gateway(ctx, "POST", "/groups", map[string]any{"creatorAccountId": s.Creator, "idempotencyKey": id}, &out); e != nil {
			return a.jobFail(ctx, id, "create", externalCode(e))
		}
		if out.GroupID == "" {
			return a.jobFail(ctx, id, "create", "BAD_JSON")
		}
		gid := ID()
		tx, e := a.pool.Begin(ctx)
		if e != nil {
			return e
		}
		defer tx.Rollback(ctx)
		_, e = tx.Exec(ctx, `INSERT INTO groups(id,gateway_group_id,creator_account_id) VALUES($1,$2,$3)`, gid, out.GroupID, s.Creator)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `INSERT INTO members(group_id,platform_user_id,account_id,role) SELECT $1,platform_user_id,id,'creator' FROM accounts WHERE id=$2`, gid, s.Creator)
		if e != nil {
			return e
		}
		s.Phase = "invite"
		_, e = tx.Exec(ctx, `UPDATE jobs SET group_id=$2,state=$3 WHERE id=$1`, id, gid, s)
		if e != nil {
			return e
		}
		return tx.Commit(ctx)
	case "invite":
		g, e := a.readGroup(ctx, *groupID)
		if e != nil {
			return e
		}
		var out struct {
			Link  string  `json:"inviteLink"`
			Delay float64 `json:"readyAfterMs"`
		}
		if e = a.gateway(ctx, "POST", "/groups/"+*g.GatewayGroupID+"/invite", nil, &out); e != nil {
			return a.jobFail(ctx, id, "invite", externalCode(e))
		}
		s.Invite = out.Link
		s.ReadyAt = time.Now().Add(time.Duration(out.Delay) * time.Millisecond)
		s.Phase = "join"
		return a.jobSave(ctx, id, s)
	case "join":
		if s.Position >= len(s.Members) {
			s.Phase = "promote"
			return a.jobSave(ctx, id, s)
		}
		account := s.Members[s.Position]
		var memberCount int
		e = a.pool.QueryRow(ctx, `SELECT count(*) FROM members WHERE group_id=$1 AND account_id=$2`, *groupID, account).Scan(&memberCount)
		if e != nil {
			return e
		}
		if memberCount > 0 {
			s.Position++
			s.JoinStarted = nil
			s.ExpiredRetries = 0
			return a.jobSave(ctx, id, s)
		}
		if s.JoinStarted != nil {
			if time.Since(*s.JoinStarted) > 10*time.Second {
				return a.jobFail(ctx, id, "join:"+account, "JOIN_TIMEOUT")
			}
			return nil
		}
		if time.Now().Before(s.ReadyAt) {
			return nil
		}
		g, e := a.readGroup(ctx, *groupID)
		if e != nil {
			return e
		}
		now := time.Now()
		s.JoinStarted = &now
		if e = a.jobSave(ctx, id, s); e != nil {
			return e
		}
		e = a.gateway(ctx, "POST", "/groups/"+*g.GatewayGroupID+"/join", map[string]any{"accountId": account, "inviteLink": s.Invite}, nil)
		if e == nil {
			return nil
		}
		code := externalCode(e)
		s.JoinStarted = nil
		if code == "ALREADY_MEMBER" {
			_, e = a.pool.Exec(ctx, `INSERT INTO members(group_id,platform_user_id,account_id,role) SELECT $1,platform_user_id,id,'member' FROM accounts WHERE id=$2 ON CONFLICT DO NOTHING`, *groupID, account)
			if e != nil {
				return e
			}
			s.Position++
			return a.jobSave(ctx, id, s)
		}
		if code == "INVITE_NOT_READY" {
			s.ReadyAt = time.Now().Add(200 * time.Millisecond)
			return a.jobSave(ctx, id, s)
		}
		if code == "INVITE_EXPIRED" && s.ExpiredRetries < 1 {
			s.ExpiredRetries++
			s.Phase = "invite"
			return a.jobSave(ctx, id, s)
		}
		if code == "GATEWAY_UNAVAILABLE" {
			s.WaitUntil = time.Now().Add(time.Second)
			return a.jobSave(ctx, id, s)
		}
		return a.jobFail(ctx, id, "join:"+account, code)
	case "promote":
		if s.PromoteAttempts >= 2 {
			return a.jobFail(ctx, id, "promote", "NOT_MEMBER_YET")
		}
		s.PromoteAttempts++
		if e = a.jobSave(ctx, id, s); e != nil {
			return e
		}
		g, e := a.readGroup(ctx, *groupID)
		if e != nil {
			return e
		}
		e = a.gateway(ctx, "POST", "/groups/"+*g.GatewayGroupID+"/promote", map[string]any{"byAccountId": s.Creator, "accountId": s.Members[0]}, nil)
		if e != nil {
			if externalCode(e) == "NOT_MEMBER_YET" && s.PromoteAttempts < 2 {
				s.WaitUntil = time.Now().Add(time.Second)
				return a.jobSave(ctx, id, s)
			}
			return a.jobFail(ctx, id, "promote", externalCode(e))
		}
		tx, e := a.pool.Begin(ctx)
		if e != nil {
			return e
		}
		defer tx.Rollback(ctx)
		if _, e = tx.Exec(ctx, `UPDATE members SET role='admin' WHERE group_id=$1 AND account_id=$2`, *groupID, s.Members[0]); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE jobs SET status='finished' WHERE id=$1`, id); e != nil {
			return e
		}
		return tx.Commit(ctx)
	}
	return nil
}
func (a *App) runLeave(ctx context.Context, id string, groupID *string, s jobState, errs []jobError) error {
	if s.Position >= len(s.Members) {
		st := "finished"
		if len(errs) > 0 {
			st = "failed"
		}
		tx, e := a.pool.Begin(ctx)
		if e != nil {
			return e
		}
		defer tx.Rollback(ctx)
		if st == "finished" {
			if _, e = tx.Exec(ctx, `DELETE FROM members WHERE group_id=$1 AND account_id IS NOT NULL`, *groupID); e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `UPDATE groups SET status='left',agent_enabled=false WHERE id=$1`, *groupID); e != nil {
				return e
			}
		}
		_, e = tx.Exec(ctx, `UPDATE jobs SET status=$2 WHERE id=$1`, id, st)
		if e != nil {
			return e
		}
		return tx.Commit(ctx)
	}
	account := s.Members[s.Position]
	if account == s.Creator && len(errs) > 0 {
		s.Position++
		return a.jobSave(ctx, id, s)
	}
	g, e := a.readGroup(ctx, *groupID)
	if e != nil {
		return e
	}
	var ms []struct {
		PlatformUserID string `json:"platformUserId"`
	}
	if e = a.gateway(ctx, "GET", "/groups/"+*g.GatewayGroupID+"/members", nil, &ms); e != nil {
		return nil
	}
	var platform string
	if e = a.pool.QueryRow(ctx, `SELECT platform_user_id FROM accounts WHERE id=$1`, account).Scan(&platform); e != nil {
		return e
	}
	present := false
	for _, m := range ms {
		if m.PlatformUserID == platform {
			present = true
		}
	}
	if present {
		if e = a.gateway(ctx, "POST", "/groups/"+*g.GatewayGroupID+"/leave", map[string]any{"accountId": account}, nil); e != nil {
			errs = append(errs, jobError{"leave:" + account, externalCode(e)})
			s.Position++
			_, e = a.pool.Exec(ctx, `UPDATE jobs SET state=$2,errors=$3 WHERE id=$1`, id, s, errs)
			return e
		}
	}
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `DELETE FROM members WHERE group_id=$1 AND account_id=$2`, *groupID, account); e != nil {
		return e
	}
	s.Position++
	if _, e = tx.Exec(ctx, `UPDATE jobs SET state=$2 WHERE id=$1`, id, s); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
