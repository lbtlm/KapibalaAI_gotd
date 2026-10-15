package platform

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type AccountGroup struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	AccountCount int       `json:"accountCount"`
	CreatedAt    time.Time `json:"createdAt"`
}

func groupConflict(e error) error {
	var p *pgconn.PgError
	if errors.As(e, &p) && p.Code == "23505" {
		return fail(409, "ACCOUNT_GROUP_NAME_EXISTS")
	}
	return e
}
func accountGroupName(r *http.Request) (string, error) {
	var b struct {
		Name string `json:"name"`
	}
	if e := decode(r, &b); e != nil {
		return "", e
	}
	b.Name = strings.TrimSpace(b.Name)
	if len([]rune(b.Name)) < 1 || len([]rune(b.Name)) > 100 {
		return "", fail(400, "VALIDATION_ERROR")
	}
	return b.Name, nil
}
func (a *App) accountGroups(w http.ResponseWriter, r *http.Request) error {
	rows, e := a.pool.Query(r.Context(), `SELECT g.id,g.name,count(a.id),g.created_at FROM account_groups g LEFT JOIN accounts a ON a.group_id=g.id GROUP BY g.id ORDER BY g.created_at,g.id`)
	if e != nil {
		return e
	}
	defer rows.Close()
	items := []AccountGroup{}
	for rows.Next() {
		var v AccountGroup
		if e = rows.Scan(&v.ID, &v.Name, &v.AccountCount, &v.CreatedAt); e != nil {
			return e
		}
		items = append(items, v)
	}
	if e = rows.Err(); e != nil {
		return e
	}
	respond(w, 200, items)
	return nil
}
func (a *App) createAccountGroup(w http.ResponseWriter, r *http.Request) error {
	name, e := accountGroupName(r)
	if e != nil {
		return e
	}
	v := AccountGroup{ID: ID(), Name: name}
	e = a.pool.QueryRow(r.Context(), `INSERT INTO account_groups(id,name) VALUES($1,$2) RETURNING created_at`, v.ID, v.Name).Scan(&v.CreatedAt)
	if e != nil {
		return groupConflict(e)
	}
	respond(w, 201, v)
	return nil
}
func (a *App) renameAccountGroup(w http.ResponseWriter, r *http.Request) (err error) {
	defer func() {
		if err != nil {
			a.failedGroupAttempt(r.Context(), r.PathValue("id"), "group_rename", err)
		}
	}()
	name, e := accountGroupName(r)
	if e != nil {
		return e
	}
	ctx := r.Context()
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	v := AccountGroup{ID: r.PathValue("id"), Name: name}
	e = tx.QueryRow(ctx, `UPDATE account_groups SET name=$2 WHERE id=$1 RETURNING created_at`, v.ID, name).Scan(&v.CreatedAt)
	if e == pgx.ErrNoRows {
		return fail(404, "ACCOUNT_GROUP_NOT_FOUND")
	}
	if e != nil {
		return groupConflict(e)
	}
	if e = logGroupAccounts(ctx, tx, v.ID, actor(ctx), "group_rename"); e != nil {
		return e
	}
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM accounts WHERE group_id=$1`, v.ID).Scan(&v.AccountCount); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	respond(w, 200, v)
	return nil
}
func (a *App) deleteAccountGroup(w http.ResponseWriter, r *http.Request) (err error) {
	defer func() {
		if err != nil {
			a.failedGroupAttempt(r.Context(), r.PathValue("id"), "group_delete", err)
		}
	}()
	ctx := r.Context()
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var id string
	e = tx.QueryRow(ctx, `SELECT id FROM account_groups WHERE id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&id)
	if e == pgx.ErrNoRows {
		return fail(404, "ACCOUNT_GROUP_NOT_FOUND")
	}
	if e != nil {
		return e
	}
	if e = logGroupAccounts(ctx, tx, id, actor(ctx), "group_delete"); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `DELETE FROM account_groups WHERE id=$1`, id); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	w.WriteHeader(204)
	return nil
}
func logGroupAccounts(ctx context.Context, tx pgx.Tx, group, who, action string) error {
	_, e := tx.Exec(ctx, `INSERT INTO account_logs(account_id,actor,action,result) SELECT id,$2,$3,'success' FROM accounts WHERE group_id=$1`, group, who, action)
	return e
}
func (a *App) moveAccounts(w http.ResponseWriter, r *http.Request) (err error) {
	var b struct {
		AccountIDs []string `json:"accountIds"`
		GroupID    *string  `json:"groupId"`
	}
	if e := decode(r, &b); e != nil {
		return e
	}
	if len(b.AccountIDs) == 0 || len(b.AccountIDs) > 1000 {
		return fail(400, "VALIDATION_ERROR")
	}
	sort.Strings(b.AccountIDs)
	for i, id := range b.AccountIDs {
		if id == "" || (i > 0 && id == b.AccountIDs[i-1]) {
			return fail(400, "VALIDATION_ERROR")
		}
	}
	defer func() {
		if err != nil {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Second)
			defer cancel()
			a.pool.Exec(ctx, `INSERT INTO account_logs(account_id,actor,action,result,error_code) SELECT id,$2,'group_move','failed',$3 FROM accounts WHERE id=ANY($1)`, b.AccountIDs, actor(ctx), safeAccountCode(err))
		}
	}()
	ctx := r.Context()
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if b.GroupID != nil {
		var id string
		e = tx.QueryRow(ctx, `SELECT id FROM account_groups WHERE id=$1 FOR KEY SHARE`, *b.GroupID).Scan(&id)
		if e == pgx.ErrNoRows {
			return fail(404, "ACCOUNT_GROUP_NOT_FOUND")
		}
		if e != nil {
			return e
		}
	}
	for _, id := range b.AccountIDs {
		var v string
		e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, id).Scan(&v)
		if e == pgx.ErrNoRows {
			return fail(404, "ACCOUNT_NOT_FOUND")
		}
		if e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, `UPDATE accounts SET group_id=$2 WHERE id=ANY($1)`, b.AccountIDs, b.GroupID); e != nil {
		return e
	}
	for _, id := range b.AccountIDs {
		if e = writeAccountLog(ctx, tx, id, actor(ctx), "group_move", nil); e != nil {
			return e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	respond(w, 200, map[string]any{"updated": len(b.AccountIDs)})
	return nil
}

func (a *App) failedGroupAttempt(ctx context.Context, group, action string, err error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	a.pool.Exec(ctx, `INSERT INTO account_logs(account_id,actor,action,result,error_code) SELECT id,$2,$3,'failed',$4 FROM accounts WHERE group_id=$1`, group, actor(ctx), action, safeAccountCode(err))
}
