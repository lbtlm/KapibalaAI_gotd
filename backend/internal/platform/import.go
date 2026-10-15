package platform

import (
	"net/http"
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
	var external []struct {
		PlatformUserID string `json:"platformUserId"`
		Role           string `json:"role"`
	}
	if e := a.gateway(r.Context(), "GET", "/groups/"+b.GatewayGroupID+"/members", nil, &external); e != nil {
		return e
	}
	roles := map[string]string{}
	for _, m := range external {
		roles[m.PlatformUserID] = m.Role
	}
	ctx := r.Context()
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	selectedMembers := map[string]string{}
	var creator *string
	var admin *string
	seen := map[string]bool{}
	for _, id := range b.AccountIDs {
		if seen[id] || id == "" {
			return fail(400, "VALIDATION_ERROR")
		}
		seen[id] = true
		var status string
		var platform *string
		e = tx.QueryRow(ctx, `SELECT status,platform_user_id FROM accounts WHERE id=$1 FOR UPDATE`, id).Scan(&status, &platform)
		if e != nil || status != "online" || platform == nil {
			return fail(422, "ACCOUNT_NOT_ONLINE")
		}
		role, ok := roles[*platform]
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
		selectedMembers[*platform] = id
	}
	groupID := ID()
	if admin == nil {
		admin = creator
	}
	_, e = tx.Exec(ctx, `INSERT INTO groups(id,gateway_group_id,creator_account_id,admin_account_id) VALUES($1,$2,$3,$4)`, groupID, b.GatewayGroupID, creator, admin)
	if e != nil {
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
