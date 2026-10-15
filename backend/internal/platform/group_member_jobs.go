package platform

import (
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"net/http"
	"sort"

	"github.com/jackc/pgx/v5"
)

// Admission reuses the existing durable join/promote worker and receipts.
func (a *App) groupMemberJob(w http.ResponseWriter, r *http.Request) error {
	if r.PathValue("action") != "administrator" && r.PathValue("action") != "members" {
		return fail(404, "NOT_FOUND")
	}
	var b struct {
		AccountIDs []string `json:"accountIds"`
		AccountID  string   `json:"accountId"`
	}
	if e := decode(r, &b); e != nil {
		return e
	}
	ctx, id := r.Context(), r.PathValue("id")
	promote := r.PathValue("action") == "administrator"
	if promote {
		b.AccountIDs = []string{b.AccountID}
	}
	if len(b.AccountIDs) == 0 {
		return fail(400, "VALIDATION_ERROR")
	}
	ids := append([]string{}, b.AccountIDs...)
	sort.Strings(ids)
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	seen := map[string]bool{}
	for _, account := range ids {
		if account == "" || seen[account] {
			return fail(400, "VALIDATION_ERROR")
		}
		seen[account] = true
		var status string
		if e = tx.QueryRow(ctx, `SELECT status FROM accounts WHERE id=$1 FOR UPDATE`, account).Scan(&status); e != nil {
			var pg *pgconn.PgError
			if errors.As(e, &pg) && pg.Code == "55P03" {
				return fail(409, "GROUP_IN_USE")
			}
			if e == pgx.ErrNoRows {
				return fail(404, "ACCOUNT_NOT_FOUND")
			}
			return e
		}
		if status != "online" {
			return fail(422, "ACCOUNT_NOT_ONLINE")
		}
	}
	var status, creator string
	if e = tx.QueryRow(ctx, `SELECT status,COALESCE(creator_account_id,'') FROM groups WHERE id=$1 FOR UPDATE NOWAIT`, id).Scan(&status, &creator); e != nil {
		var pg *pgconn.PgError
		if errors.As(e, &pg) && pg.Code == "55P03" {
			return fail(409, "GROUP_IN_USE")
		}
		if e == pgx.ErrNoRows {
			return fail(404, "GROUP_NOT_FOUND")
		}
		return e
	}
	if status != "active" {
		return fail(409, "GROUP_UNREACHABLE")
	}
	var busy bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE group_id=$1 AND status='running') OR ($2 AND (EXISTS(SELECT 1 FROM agent_runs WHERE group_id=$1 AND status='running') OR EXISTS(SELECT 1 FROM sequence_runs WHERE group_id=$1 AND status='running') OR EXISTS(SELECT 1 FROM agent_effects e JOIN agent_runs r ON r.id=e.run_id WHERE r.group_id=$1 AND e.kind='kick' AND e.state!='done')))`, id, promote).Scan(&busy); e != nil {
		return e
	}
	if busy {
		return fail(409, "GROUP_IN_USE")
	}
	state := jobState{Creator: creator, Members: b.AccountIDs, Phase: "invite"}
	kind := "join"
	if promote {
		if creator == "" || creator == b.AccountID {
			return fail(422, "GROUP_OWNER_REQUIRED")
		}
		var member bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM members WHERE group_id=$1 AND account_id=$2)`, id, b.AccountID).Scan(&member); e != nil {
			return e
		}
		if !member {
			return fail(422, "MEMBER_NOT_IN_GROUP")
		}
		state.Phase = "promote"
		state.Admin = b.AccountID
		kind = "promote"
	}
	job := ID()
	if _, e = tx.Exec(ctx, `INSERT INTO jobs(id,kind,group_id,state) VALUES($1,$2,$3,$4)`, job, kind, id, state); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	respond(w, 202, map[string]string{"jobId": job})
	return nil
}

func (a *App) groupJobs(w http.ResponseWriter, r *http.Request) error {
	rows, e := a.pool.Query(r.Context(), `SELECT id,kind,status,errors,logs,state->>'phase',created_at FROM jobs WHERE group_id=$1 ORDER BY created_at DESC,id DESC LIMIT 50`, r.PathValue("id"))
	if e != nil {
		return e
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, kind, status string
		var phase *string
		var errors, logs json.RawMessage
		var at any
		if e = rows.Scan(&id, &kind, &status, &errors, &logs, &phase, &at); e != nil {
			return e
		}
		items = append(items, map[string]any{"id": id, "kind": kind, "status": status, "errors": errors, "logs": logs, "phase": phase, "createdAt": at})
	}
	respond(w, 200, items)
	return rows.Err()
}
