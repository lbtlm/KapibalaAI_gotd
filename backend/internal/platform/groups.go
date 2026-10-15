package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

type member struct {
	AccountID      *string `json:"accountId"`
	PlatformUserID string  `json:"platformUserId"`
	Role           string  `json:"role"`
}
type group struct {
	ID                  string          `json:"id"`
	Title               string          `json:"title"`
	Username            string          `json:"username"`
	InviteLink          string          `json:"inviteLink"`
	AdminAccountID      *string         `json:"adminAccountId"`
	SequenceConfig      json.RawMessage `json:"sequenceConfig"`
	GatewayGroupID      *string         `json:"gatewayGroupId"`
	Status              string          `json:"status"`
	CreatorAccountID    string          `json:"creatorAccountId"`
	AgentEnabled        bool            `json:"agentEnabled"`
	AutoKickEnabled     bool            `json:"autoKickEnabled"`
	Members             []member        `json:"members"`
	ActiveSequenceRunID *string         `json:"activeSequenceRunId"`
	ActiveAgentRunID    *string         `json:"activeAgentRunId"`
	ActiveJobID         *string         `json:"activeJobId"`
	MarketingPaused     bool            `json:"marketingPaused"`
	AgentSessionID      string          `json:"agentSessionId"`
}

func (a *App) readGroup(ctx context.Context, id string) (group, error) {
	var g group
	e := a.pool.QueryRow(ctx, `SELECT id,gateway_group_id,status,COALESCE(creator_account_id,''),agent_enabled,auto_kick_enabled,(SELECT id FROM sequence_runs WHERE group_id=groups.id AND status='running'),(SELECT id FROM agent_runs WHERE group_id=groups.id AND status='running'),title,admin_account_id,sequence_config,username,invite_link,marketing_paused FROM groups WHERE id=$1`, id).Scan(&g.ID, &g.GatewayGroupID, &g.Status, &g.CreatorAccountID, &g.AgentEnabled, &g.AutoKickEnabled, &g.ActiveSequenceRunID, &g.ActiveAgentRunID, &g.Title, &g.AdminAccountID, &g.SequenceConfig, &g.Username, &g.InviteLink, &g.MarketingPaused)
	if e == pgx.ErrNoRows {
		return g, fail(404, "GROUP_NOT_FOUND")
	}
	if e != nil {
		return g, e
	}
	if e = a.pool.QueryRow(ctx, `SELECT (SELECT id FROM jobs WHERE group_id=$1 AND status='running' ORDER BY created_at LIMIT 1)`, id).Scan(&g.ActiveJobID); e != nil {
		return g, e
	}
	g.Members = []member{}
	g.AgentSessionID = "group-agent:" + g.ID
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
		AgentEnabled    *bool   `json:"agentEnabled"`
		AutoKickEnabled *bool   `json:"autoKickEnabled"`
		AdminAccountID  *string `json:"adminAccountId"`
	}
	if e := decode(r, &b); e != nil {
		return e
	}
	ctx, id := r.Context(), r.PathValue("id")
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var status string
	var admin *string
	e = tx.QueryRow(ctx, `SELECT status,admin_account_id FROM groups WHERE id=$1 FOR UPDATE`, id).Scan(&status, &admin)
	if e == pgx.ErrNoRows {
		return fail(404, "GROUP_NOT_FOUND")
	}
	if e != nil {
		return e
	}
	if status != "active" {
		return fail(409, "GROUP_UNREACHABLE")
	}
	if b.AdminAccountID != nil && (admin == nil || *admin != *b.AdminAccountID) {
		var busy bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_runs WHERE group_id=$1 AND status='running') OR EXISTS(SELECT 1 FROM sequence_runs WHERE group_id=$1 AND status='running') OR EXISTS(SELECT 1 FROM jobs WHERE group_id=$1 AND status='running') OR EXISTS(SELECT 1 FROM agent_effects e JOIN agent_runs r ON r.id=e.run_id WHERE r.group_id=$1 AND e.kind='kick' AND e.state!='done')`, id).Scan(&busy); e != nil {
			return e
		}
		if busy {
			return fail(409, "GROUP_IN_USE")
		}
		admin = b.AdminAccountID
	}
	if b.AdminAccountID != nil || b.AgentEnabled != nil && *b.AgentEnabled {
		var valid bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM members WHERE group_id=$1 AND account_id=$2 AND role IN ('admin','creator'))`, id, admin).Scan(&valid); e != nil {
			return e
		}
		if !valid {
			return fail(422, "ADMIN_REQUIRED")
		}
	}
	tag, e := tx.Exec(ctx, `UPDATE groups SET agent_enabled=COALESCE($2,agent_enabled),auto_kick_enabled=COALESCE($3,auto_kick_enabled),admin_account_id=$4 WHERE id=$1`, id, b.AgentEnabled, b.AutoKickEnabled, admin)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return fail(404, "GROUP_NOT_FOUND")
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	return a.groupGet(w, r)
}

type jobState struct {
	Creator           string     `json:"creator"`
	Admin             string     `json:"admin,omitempty"`
	Title             string     `json:"title,omitempty"`
	Username          string     `json:"username,omitempty"`
	PrivateOnConflict bool       `json:"privateOnConflict,omitempty"`
	Members           []string   `json:"members"`
	Phase             string     `json:"phase"`
	Position          int        `json:"position"`
	Invite            string     `json:"invite"`
	ReadyAt           time.Time  `json:"readyAt"`
	JoinStarted       *time.Time `json:"joinStarted"`
	ExpiredRetries    int        `json:"expiredRetries"`
	PromoteAttempts   int        `json:"promoteAttempts"`
	PromoteStage      string     `json:"promoteStage,omitempty"`
	WaitUntil         time.Time  `json:"waitUntil"`
}
type jobError struct {
	Step string `json:"step"`
	Code string `json:"code"`
}

func (a *App) createGroup(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		CreatorAccountID  string   `json:"creatorAccountId"`
		MemberAccountIDs  []string `json:"memberAccountIds"`
		AdminAccountID    string   `json:"adminAccountId"`
		Title             string   `json:"title"`
		Username          string   `json:"username"`
		PrivateOnConflict bool     `json:"privateOnConflict"`
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
	if b.AdminAccountID == "" {
		b.AdminAccountID = b.MemberAccountIDs[0]
	}
	if !seen[b.AdminAccountID] || b.AdminAccountID == b.CreatorAccountID {
		return fail(422, "ADMIN_NOT_SELECTED")
	}
	b.Title = strings.TrimSpace(b.Title)
	b.Username = strings.TrimPrefix(strings.TrimSpace(b.Username), "@")
	if b.Username != "" && !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{4,31}$`).MatchString(b.Username) {
		return fail(422, "USERNAME_INVALID")
	}
	if len([]rune(b.Title)) > 128 {
		return fail(400, "VALIDATION_ERROR")
	}
	sort.Strings(ids)
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
	_, e = tx.Exec(ctx, `INSERT INTO jobs(id,kind,state) VALUES($1,'create',$2)`, job, jobState{Creator: b.CreatorAccountID, Admin: b.AdminAccountID, Title: b.Title, Username: b.Username, PrivateOnConflict: b.PrivateOnConflict, Members: b.MemberAccountIDs, Phase: "create"})
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
	var es, logs, state json.RawMessage
	e := a.pool.QueryRow(r.Context(), `SELECT status,errors,group_id,logs,state FROM jobs WHERE id=$1`, r.PathValue("id")).Scan(&st, &es, &groupID, &logs, &state)
	if e == pgx.ErrNoRows {
		return fail(404, "JOB_NOT_FOUND")
	}
	if e != nil {
		return e
	}
	var progress jobState
	json.Unmarshal(state, &progress)
	respond(w, 200, map[string]any{"status": st, "errors": es, "groupId": groupID, "logs": logs, "phase": progress.Phase, "position": progress.Position, "total": len(progress.Members)})
	return nil
}
func (a *App) leaveAll(w http.ResponseWriter, r *http.Request) error {
	ctx, groupID := r.Context(), r.PathValue("id")
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	// Share deletion's account lock before reading the final job references.
	// A stable account order avoids inversions between concurrent admissions.
	rows, e := tx.Query(ctx, `SELECT id FROM accounts WHERE id IN (SELECT creator_account_id FROM groups WHERE id=$1 UNION SELECT account_id FROM members WHERE group_id=$1) ORDER BY id FOR UPDATE`, groupID)
	if e != nil {
		return e
	}
	locked := map[string]bool{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		locked[id] = true
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return e
	}
	var creator string
	if e = tx.QueryRow(ctx, `SELECT COALESCE(creator_account_id,'') FROM groups WHERE id=$1 FOR UPDATE NOWAIT`, groupID).Scan(&creator); e == pgx.ErrNoRows {
		return fail(404, "GROUP_NOT_FOUND")
	} else if e != nil {
		var pg *pgconn.PgError
		if errors.As(e, &pg) && pg.Code == "55P03" {
			return fail(409, "GROUP_IN_USE")
		}
		return e
	}
	rows, e = tx.Query(ctx, `SELECT DISTINCT account_id FROM members WHERE group_id=$1 AND account_id IS NOT NULL AND account_id!=$2 ORDER BY account_id`, groupID, creator)
	if e != nil {
		return e
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return e
		}
		if !locked[id] {
			return fail(409, "CAS_CONFLICT")
		}
		ids = append(ids, id)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return e
	}
	if creator != "" {
		if !locked[creator] {
			return fail(409, "CAS_CONFLICT")
		}
		ids = append(ids, creator)
	}
	job := ID()
	var existing string
	if e = tx.QueryRow(ctx, `SELECT id FROM jobs WHERE group_id=$1 AND kind='leave' AND status='running' ORDER BY created_at LIMIT 1`, groupID).Scan(&existing); e == nil {
		respond(w, 202, map[string]any{"jobId": existing})
		return nil
	} else if e != pgx.ErrNoRows {
		return e
	}
	var changingMembers bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE group_id=$1 AND status='running')`, groupID).Scan(&changingMembers); e != nil {
		return e
	}
	if changingMembers {
		return fail(409, "GROUP_IN_USE")
	}
	if _, e = tx.Exec(ctx, `UPDATE groups SET status='ending',agent_enabled=false WHERE id=$1`, groupID); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `DELETE FROM agent_pending WHERE group_id=$1`, groupID); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE sequence_runs SET status='cancelled' WHERE group_id=$1 AND status='running'`, groupID); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE messages SET delivery_status='cancelled',fail_code='MARKETING_ENDED' WHERE group_id=$1 AND delivery_status='queued'`, groupID); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO jobs(id,kind,group_id,state) VALUES($1,'leave',$2,$3)`, job, groupID, jobState{Creator: creator, Members: ids, Phase: "drain"})
	if e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	respond(w, 202, map[string]any{"jobId": job})
	return nil
}
func (a *App) jobSave(ctx context.Context, id string, s jobState) error {
	_, e := a.pool.Exec(ctx, `UPDATE jobs SET logs=CASE WHEN state->>'phase' IS DISTINCT FROM $2::jsonb->>'phase' OR state->>'position' IS DISTINCT FROM $2::jsonb->>'position' THEN logs||jsonb_build_array(jsonb_build_object('at',now(),'phase',state->>'phase','position',state->>'position','result','success')) ELSE logs END,state=$2 WHERE id=$1`, id, s)
	return e
}
func (a *App) jobFail(ctx context.Context, id, step, code string) error {
	_, e := a.pool.Exec(ctx, `UPDATE jobs SET status='failed',errors=errors||$2::jsonb,logs=logs||jsonb_build_array(jsonb_build_object('at',now(),'phase',$3::text,'result','failed','code',$4::text)) WHERE id=$1`, id, []jobError{{step, code}}, step, code)
	return e
}
func (a *App) jobsTick(ctx context.Context) {
	ids := a.ids(ctx, `SELECT id FROM jobs WHERE status='running' ORDER BY created_at`)
	for _, id := range ids {
		a.launchWorker(ctx, "job:"+id, func(ctx context.Context) error { return a.runJob(ctx, id) })
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
	if s.Admin == "" && len(s.Members) > 0 {
		s.Admin = s.Members[0]
	}
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
		if e = a.gateway(ctx, "POST", "/groups", map[string]any{"creatorAccountId": s.Creator, "idempotencyKey": id, "title": s.Title}, &out); e != nil {
			if code := externalCode(e); code == "NETWORK_TIMEOUT" || code == "GATEWAY_UNAVAILABLE" {
				s.WaitUntil = time.Now().Add(time.Second)
				return a.jobSave(ctx, id, s)
			}
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
		_, e = tx.Exec(ctx, `INSERT INTO groups(id,gateway_group_id,creator_account_id,title,admin_account_id) VALUES($1,$2,$3,$4,$5)`, gid, out.GroupID, s.Creator, s.Title, s.Admin)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `INSERT INTO members(group_id,platform_user_id,account_id,role) SELECT $1,platform_user_id,id,'creator' FROM accounts WHERE id=$2`, gid, s.Creator)
		if e != nil {
			return e
		}
		s.Phase = "configure"
		_, e = tx.Exec(ctx, `UPDATE jobs SET group_id=$2,state=$3,logs=logs||jsonb_build_array(jsonb_build_object('at',now(),'phase','create','result','success')) WHERE id=$1`, id, gid, s)
		if e != nil {
			return e
		}
		return tx.Commit(ctx)
	case "configure":
		g, e := a.readGroup(ctx, *groupID)
		if e != nil {
			return e
		}
		if s.Title != "" || s.Username != "" {
			e = a.gateway(ctx, "POST", "/groups/"+*g.GatewayGroupID+"/configure", map[string]any{"accountId": s.Creator, "title": s.Title, "username": s.Username}, nil)
			if e != nil {
				code := externalCode(e)
				if code == "NETWORK_TIMEOUT" || code == "GATEWAY_UNAVAILABLE" {
					return e
				}
				if s.PrivateOnConflict && (code == "USERNAME_OCCUPIED" || code == "USERNAME_PURCHASE_AVAILABLE" || code == "CHANNELS_ADMIN_PUBLIC_TOO_MUCH") {
					if _, e = a.pool.Exec(ctx, `UPDATE jobs SET errors=errors||$2::jsonb WHERE id=$1`, id, []jobError{{"private_fallback", code}}); e != nil {
						return e
					}
					s.Username = ""
					return a.jobSave(ctx, id, s)
				}
				return a.jobFail(ctx, id, "configure", code)
			}
		}
		if _, e = a.pool.Exec(ctx, `UPDATE groups SET username=$2 WHERE id=$1`, *groupID, s.Username); e != nil {
			return e
		}
		s.Phase = "invite"
		return a.jobSave(ctx, id, s)
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
		if _, e = a.pool.Exec(ctx, `UPDATE groups SET invite_link=$2 WHERE id=$1`, *groupID, out.Link); e != nil {
			return e
		}
		s.ReadyAt = time.Now().Add(time.Duration(out.Delay) * time.Millisecond)
		s.Phase = "join"
		return a.jobSave(ctx, id, s)
	case "join":
		if s.Position >= len(s.Members) {
			if kind == "join" {
				_, e = a.pool.Exec(ctx, `UPDATE jobs SET status='finished' WHERE id=$1`, id)
				return e
			}
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
		legacyUnknown := s.PromoteStage == "" && s.PromoteAttempts > 0
		if !legacyUnknown && s.PromoteStage != "prepared" {
			if s.PromoteAttempts >= 2 {
				return a.jobFail(ctx, id, "promote", "NOT_MEMBER_YET")
			}
			s.PromoteAttempts++
			s.PromoteStage = "prepared"
			if e = a.jobSave(ctx, id, s); e != nil {
				return e
			}
		}
		g, e := a.readGroup(ctx, *groupID)
		if e != nil {
			return e
		}
		var members []struct {
			PlatformUserID string `json:"platformUserId"`
			Role           string `json:"role"`
		}
		if e = a.gateway(ctx, "GET", "/groups/"+*g.GatewayGroupID+"/members", nil, &members); e != nil {
			return e
		}
		var target string
		if e = a.pool.QueryRow(ctx, `SELECT platform_user_id FROM accounts WHERE id=$1`, s.Admin).Scan(&target); e != nil {
			return e
		}
		for _, m := range members {
			if m.PlatformUserID == target && m.Role == "admin" {
				return a.finishPromote(ctx, id, *groupID, s.Admin)
			}
		}
		if legacyUnknown {
			// Old checkpoints have no gateway receipt key. Lack of an admin role
			// cannot prove whether their unkeyed request reached the platform.
			return nil
		}
		key := fmt.Sprintf("%s:promote:%d", id, s.PromoteAttempts)
		e = a.gateway(ctx, "POST", "/groups/"+*g.GatewayGroupID+"/promote", map[string]any{"byAccountId": s.Creator, "accountId": s.Admin, "idempotencyKey": key}, nil)
		if e != nil {
			code := externalCode(e)
			if code == "NETWORK_TIMEOUT" || code == "GATEWAY_UNAVAILABLE" {
				s.WaitUntil = time.Now().Add(200 * time.Millisecond)
				return a.jobSave(ctx, id, s)
			}
			if code == "NOT_MEMBER_YET" && s.PromoteAttempts < 2 {
				s.PromoteStage = "retry"
				s.WaitUntil = time.Now().Add(time.Second)
				return a.jobSave(ctx, id, s)
			}
			return a.jobFail(ctx, id, "promote", code)
		}
		return a.finishPromote(ctx, id, *groupID, s.Admin)
	}
	return nil
}

func (a *App) finishPromote(ctx context.Context, id, groupID, account string) error {
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `UPDATE members SET role='admin' WHERE group_id=$1 AND account_id=$2`, groupID, account); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE groups SET admin_account_id=$2 WHERE id=$1`, groupID, account); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE jobs SET status='finished',logs=logs||jsonb_build_array(jsonb_build_object('at',now(),'phase','promote','result','success','accountId',$2::text)) WHERE id=$1`, id, account); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (a *App) runLeave(ctx context.Context, id string, groupID *string, s jobState, errs []jobError) error {
	var pending bool
	if e := a.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM messages WHERE group_id=$1 AND delivery_status IN ('dispatching','unknown','accepted')) OR EXISTS(SELECT 1 FROM agent_runs WHERE group_id=$1 AND status='running') OR EXISTS(SELECT 1 FROM agent_effects e JOIN agent_runs r ON r.id=e.run_id WHERE r.group_id=$1 AND e.kind='kick' AND e.state!='done') OR EXISTS(SELECT 1 FROM jobs WHERE group_id=$1 AND id!=$2 AND status='running')`, *groupID, id).Scan(&pending); e != nil {
		return e
	}
	if pending {
		return nil
	}
	if s.Phase != "leave" {
		s.Phase = "leave"
		return a.jobSave(ctx, id, s)
	}
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
			_, e = a.pool.Exec(ctx, `UPDATE jobs SET state=$2,errors=$3,logs=logs||jsonb_build_array(jsonb_build_object('at',now(),'phase','leave','result','failed','accountId',$4::text,'code',$5::text)) WHERE id=$1`, id, s, errs, account, externalCode(e))
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
	if _, e = tx.Exec(ctx, `UPDATE jobs SET state=$2,logs=logs||jsonb_build_array(jsonb_build_object('at',now(),'phase','leave','result','success','accountId',$3::text)) WHERE id=$1`, id, s, account); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
