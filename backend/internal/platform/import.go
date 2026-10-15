package platform

import (
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"net/http"
	"sort"
)

func (a *App) importGroup(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		GatewayGroupID string   `json:"gatewayGroupId"`
		AccountIDs     []string `json:"accountIds"`
	}
	if e := decode(r, &b); e != nil {
		return e
	}
	if b.GatewayGroupID == "" || len(b.AccountIDs) == 0 {
		return fail(400, "VALIDATION_ERROR")
	}
	ctx := r.Context()
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	seen := map[string]bool{}
	for _, id := range b.AccountIDs {
		if id == "" || seen[id] {
			return fail(400, "VALIDATION_ERROR")
		}
		seen[id] = true
	}
	ids := append([]string(nil), b.AccountIDs...)
	sort.Strings(ids)
	platforms := map[string]string{}
	for _, id := range ids {
		var status string
		var pid *string
		if e = tx.QueryRow(ctx, `SELECT status,platform_user_id FROM accounts WHERE id=$1 FOR UPDATE NOWAIT`, id).Scan(&status, &pid); e != nil {
			var busy *pgconn.PgError
			if errors.As(e, &busy) && busy.Code == "55P03" {
				return fail(409, "ACCOUNT_IN_USE")
			}
			if e == pgx.ErrNoRows {
				return fail(422, "ACCOUNT_NOT_ONLINE")
			}
			return e
		}
		if status != "online" || pid == nil {
			return fail(422, "ACCOUNT_NOT_ONLINE")
		}
		platforms[id] = *pid
	}
	var raw json.RawMessage
	e = tx.QueryRow(ctx, `SELECT items FROM account_group_snapshots WHERE account_id=$1 AND last_synced_at IS NOT NULL`, b.AccountIDs[0]).Scan(&raw)
	if e == pgx.ErrNoRows {
		return fail(409, "GROUP_SNAPSHOT_REQUIRED")
	}
	if e != nil {
		return e
	}
	var snapshot []joinedGroup
	if e = json.Unmarshal(raw, &snapshot); e != nil {
		return e
	}
	found := false
	for _, g := range snapshot {
		if g.ID == b.GatewayGroupID {
			if g.Type != "supergroup" {
				return fail(422, "GROUP_TYPE_UNSUPPORTED")
			}
			found = true
			break
		}
	}
	if !found {
		return fail(409, "GROUP_SNAPSHOT_REQUIRED")
	}
	var exists bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM groups WHERE gateway_group_id=$1)`, b.GatewayGroupID).Scan(&exists); e != nil {
		return e
	}
	if exists {
		return fail(409, "GROUP_ALREADY_IMPORTED")
	}
	var imported struct {
		GroupID  string `json:"groupId"`
		Title    string `json:"title"`
		Username string `json:"username"`
	}
	if e = a.gateway(ctx, "POST", "/groups/import", map[string]any{"groupId": b.GatewayGroupID, "accountIds": b.AccountIDs}, &imported); e != nil {
		return accountGatewayError(e)
	}
	if imported.GroupID != b.GatewayGroupID {
		return fail(502, "GATEWAY_PROTOCOL_ERROR")
	}
	var external []struct {
		PlatformUserID string `json:"platformUserId"`
		Role           string `json:"role"`
	}
	if e = a.gateway(ctx, "GET", "/groups/"+b.GatewayGroupID+"/members", nil, &external); e != nil {
		return accountGatewayError(e)
	}
	roles := map[string]string{}
	for _, m := range external {
		roles[m.PlatformUserID] = m.Role
	}
	selectedMembers := map[string]string{}
	var creator *string
	var admin *string
	for _, id := range b.AccountIDs {
		platform := platforms[id]
		role, ok := roles[platform]
		if !ok {
			return fail(409, "ACCOUNT_NOT_IN_GROUP")
		}
		if role != "creator" && role != "admin" {
			role = "member"
		}
		if role == "creator" {
			c := id
			creator = &c
		}
		if role == "admin" && admin == nil {
			value := id
			admin = &value
		}
		selectedMembers[platform] = id
	}
	groupID := ID()
	if admin == nil {
		admin = creator
	}
	_, e = tx.Exec(ctx, `INSERT INTO groups(id,gateway_group_id,creator_account_id,admin_account_id,title,username) VALUES($1,$2,$3,$4,$5,$6)`, groupID, b.GatewayGroupID, creator, admin, imported.Title, imported.Username)
	if e != nil {
		var conflict *pgconn.PgError
		if errors.As(e, &conflict) && conflict.Code == "23505" {
			return fail(409, "GROUP_ALREADY_IMPORTED")
		}
		return e
	}
	for _, m := range external {
		var account *string
		if selected, ok := selectedMembers[m.PlatformUserID]; ok {
			account = &selected
		}
		role := m.Role
		if role != "creator" && role != "admin" {
			role = "member"
		}
		_, e = tx.Exec(ctx, `INSERT INTO members(group_id,platform_user_id,account_id,role) VALUES($1,$2,$3,$4)`, groupID, m.PlatformUserID, account, role)
		if e != nil {
			return e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	respond(w, 201, map[string]any{"id": groupID})
	return nil
}
