package platform_test

import (
	"context"
	"encoding/json"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
	"net/http"
	"testing"
)

func TestImportExternalOwnedGroupVerifiesMembersAndLeavesOnlyServiceAccounts(t *testing.T) {
	g := gatewayFixture(t)
	g.mu.Lock()
	g.members["p-account-2"] = true
	g.members["external-owner"] = true
	g.mu.Unlock()
	base := g.server.Config.Handler
	g.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/groups/gateway-group/members" {
			g.mu.Lock()
			defer g.mu.Unlock()
			members := []any{}
			for p := range g.members {
				role := "member"
				if p == "external-owner" {
					role = "creator"
				}
				members = append(members, map[string]any{"platformUserId": p, "role": role})
			}
			json.NewEncoder(w).Encode(members)
			return
		}
		base.ServeHTTP(w, r)
	})
	a, s := fixture(t, platform.Config{GatewayURL: g.server.URL})
	a.Start(context.Background())
	admin, _ := login(t, s, "admin")
	viewer, _ := login(t, s, "viewer")
	call(t, s, "POST", "/api/accounts/account-2/connect", admin, "{}")
	st, _, _ := call(t, s, "POST", "/api/groups/import", viewer, `{"gatewayGroupId":"gateway-group","accountIds":["account-2"]}`)
	if st != 403 {
		t.Fatalf("viewer imported %d", st)
	}
	st, b, _ := call(t, s, "POST", "/api/groups/import", admin, `{"gatewayGroupId":"gateway-group","accountIds":["account-2"]}`)
	if st != 201 {
		t.Fatalf("import %d %v", st, b)
	}
	id := b["id"].(string)
	_, b, _ = call(t, s, "GET", "/api/groups/"+id, admin, "")
	if b["creatorAccountId"] != "" {
		t.Fatalf("external owner invented %v", b)
	}
	members := b["members"].([]any)
	if len(members) != 2 {
		t.Fatalf("import hid external membership %v", b)
	}
	owner := false
	for _, m := range members {
		v := m.(map[string]any)
		if v["platformUserId"] == "external-owner" && v["accountId"] == nil && v["role"] == "creator" {
			owner = true
		}
	}
	if !owner {
		t.Fatalf("external owner's identity/role lost %v", b)
	}
	st, b, _ = call(t, s, "POST", "/api/groups/"+id+"/leave-all", admin, "{}")
	if st != 202 {
		t.Fatal(st)
	}
	job := b["jobId"].(string)
	eventually(t, 4e9, func() bool {
		_, b, _ := call(t, s, "GET", "/api/jobs/"+job, admin, "")
		return b["status"] == "finished"
	})
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.members["external-owner"] || g.members["p-account-2"] {
		t.Fatalf("wrong accounts left: %v", g.members)
	}
}
